package live

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"

	"together/internal/auth"
)

type Hub struct {
	db    *sql.DB
	mu    sync.Mutex
	rooms map[int64]*room
}

type room struct {
	id       int64
	mu       sync.Mutex
	clients  map[*client]bool
	actID    int64
	actState *WatchState // nil = no activity. ponytail: watch-only; make an interface when music activity lands
}

type client struct {
	user auth.User
	send chan []byte
}

func NewHub(d *sql.DB) *Hub { return &Hub{db: d, rooms: map[int64]*room{}} }

// Restore reloads active activities after a server restart.
func (h *Hub) Restore() error {
	rows, err := h.db.Query(`SELECT id, room_id, state_json FROM activities WHERE status='active'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, roomID int64
		var js string
		rows.Scan(&id, &roomID, &js)
		var st WatchState
		if json.Unmarshal([]byte(js), &st) == nil {
			r := h.room(roomID)
			r.actID, r.actState = id, &st
		}
	}
	return nil
}

func (h *Hub) room(id int64) *room {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r, ok := h.rooms[id]; ok {
		return r
	}
	r := &room{id: id, clients: map[*client]bool{}}
	h.rooms[id] = r
	return r
}

func nowMs() int64 { return time.Now().UnixMilli() }

func marshal(v any) []byte { b, _ := json.Marshal(v); return b }

func (r *room) broadcast(b []byte) { // callers hold r.mu
	for c := range r.clients {
		select {
		case c.send <- b:
		default: // ponytail: slow consumer drops frame; next state broadcast is absolute so nothing is lost
		}
	}
}

func (r *room) activityJSON() any {
	if r.actState == nil {
		return nil
	}
	return map[string]any{"id": r.actID, "type": "watch", "state": *r.actState}
}

func (r *room) presence() []auth.User {
	seen := map[int64]bool{}
	out := []auth.User{}
	for c := range r.clients {
		if !seen[c.user.ID] {
			seen[c.user.ID] = true
			out = append(out, c.user)
		}
	}
	return out
}

type inMsg struct {
	Type     string  `json:"type"`
	Body     string  `json:"body"`
	MediaID  int64   `json:"mediaId"`
	Action   string  `json:"action"`
	Position float64 `json:"position"`
	T        int64   `json:"t"`
}

func (h *Hub) Handle(w http.ResponseWriter, req *http.Request) {
	roomID, err := strconv.ParseInt(req.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad room id", 400)
		return
	}
	conn, err := websocket.Accept(w, req, nil)
	if err != nil {
		return
	}
	user := auth.From(req)
	r := h.room(roomID)
	c := &client{user: user, send: make(chan []byte, 32)}

	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	go func() { // writer
		for b := range c.send {
			if conn.Write(ctx, websocket.MessageText, b) != nil {
				cancel()
				return
			}
		}
	}()

	r.mu.Lock()
	r.clients[c] = true
	c.send <- marshal(map[string]any{"type": "hello", "you": user, "users": r.presence(), "activity": r.activityJSON()})
	r.broadcast(marshal(map[string]any{"type": "presence", "users": r.presence()}))
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		delete(r.clients, c)
		close(c.send)
		r.broadcast(marshal(map[string]any{"type": "presence", "users": r.presence()}))
		r.mu.Unlock()
		conn.CloseNow()
	}()

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var m inMsg
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		h.dispatch(r, c, m)
	}
}

func (h *Hub) dispatch(r *room, c *client, m inMsg) {
	switch m.Type {
	case "ping":
		c.send <- marshal(map[string]any{"type": "pong", "t": m.T, "serverTime": nowMs()})

	case "chat":
		if len(m.Body) == 0 || len(m.Body) > 2000 {
			return
		}
		now := time.Now().Unix()
		h.db.Exec(`INSERT INTO messages (room_id, user_id, body, created_at) VALUES (?,?,?,?)`, r.id, c.user.ID, m.Body, now)
		r.mu.Lock()
		r.broadcast(marshal(map[string]any{"type": "chat", "userId": c.user.ID, "username": c.user.Username, "body": m.Body, "createdAt": now}))
		r.mu.Unlock()

	case "start":
		var status string
		if h.db.QueryRow(`SELECT status FROM media WHERE id=? AND kind='movie'`, m.MediaID).Scan(&status) != nil || status != "ready" {
			c.send <- marshal(map[string]any{"type": "error", "body": "media not ready"})
			return
		}
		st := NewWatch(m.MediaID, nowMs())
		res, err := h.db.Exec(`INSERT INTO activities (room_id, type, state_json) VALUES (?,?,?)`, r.id, "watch", string(marshal(st)))
		if err != nil {
			log.Println("start activity:", err)
			return
		}
		id, _ := res.LastInsertId()
		r.mu.Lock()
		r.actID, r.actState = id, &st
		r.broadcast(marshal(map[string]any{"type": "activity", "activity": r.activityJSON()}))
		r.mu.Unlock()

	case "end":
		r.mu.Lock()
		if r.actState != nil {
			h.db.Exec(`UPDATE activities SET status='ended', ended_at=unixepoch() WHERE id=?`, r.actID)
			r.actState = nil
			r.broadcast(marshal(map[string]any{"type": "activity", "activity": nil}))
		}
		r.mu.Unlock()

	case "intent":
		r.mu.Lock()
		if r.actState != nil {
			if next, err := r.actState.Apply(m.Action, m.Position, nowMs()); err == nil {
				r.actState = &next
				h.db.Exec(`UPDATE activities SET state_json=? WHERE id=?`, string(marshal(next)), r.actID)
				r.broadcast(marshal(map[string]any{"type": "activity", "activity": r.activityJSON()}))
			}
		}
		r.mu.Unlock()
	}
}
