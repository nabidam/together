package live

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"together/internal/auth"
)

// Hub owns the in-memory room world. Nothing here is persisted: rooms, their
// playback state, chat, and presence live only in memory and die with the room
// or the process (V2 — SPEC §9.2/§9.11). The db handle is retained only for
// reading durable media rows at room creation, never for room state.
type Hub struct {
	db     *sql.DB
	mu     sync.Mutex // guards rooms map and guests map
	rooms  map[string]*Room
	guests map[string]*GuestSession // keyed by the together_guest cookie token
}

// Room is ephemeral hub state. All mutable fields are guarded by Room.mu.
type Room struct {
	mu         sync.Mutex
	id         string      // opaque, crypto-random 16 hex chars
	name       string      // ≤64 chars
	ownerID    int64       // account user id; isHost := conn.userID == ownerID, live
	mediaID    int64       // fixed at creation
	mediaTitle string      // copied from the media row at creation
	kind       string      // 'video' | 'audio', copied from the media row
	joinToken  string      // ≥128-bit crypto-random hex; regenerate replaces it
	watch      *WatchState // nil = no active activity; started at creation
	clients    map[*client]bool
}

type client struct {
	user   auth.User
	send   chan []byte
	cancel func() // stops this connection; set in Handle, invoked by teardown
}

func NewHub(d *sql.DB) *Hub {
	return &Hub{db: d, rooms: map[string]*Room{}, guests: map[string]*GuestSession{}}
}

// randHex returns n crypto-random bytes as hex — the same generator behind
// internal/auth session tokens (crypto/rand). Room ids use 8 bytes (16 hex);
// join tokens 16 bytes (128-bit).
func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (h *Hub) getRoom(id string) (*Room, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, ok := h.rooms[id]
	return r, ok
}

func nowMs() int64 { return time.Now().UnixMilli() }

func marshal(v any) []byte { b, _ := json.Marshal(v); return b }

func (r *Room) broadcast(b []byte) { // callers hold r.mu
	for c := range r.clients {
		select {
		case c.send <- b:
		default: // ponytail: slow consumer drops frame; next state broadcast is absolute so nothing is lost
		}
	}
}

func (r *Room) activityJSON() any {
	if r.watch == nil {
		return nil
	}
	return map[string]any{"id": r.id, "type": "watch", "state": *r.watch}
}

func (r *Room) presence() []auth.User {
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

// Handle upgrades a WS connection scoped to an existing room. The room must
// already exist (created via POST /api/rooms) — there is no lazy creation.
func (h *Hub) Handle(w http.ResponseWriter, req *http.Request) {
	r, ok := h.getRoom(req.PathValue("id"))
	if !ok {
		http.Error(w, `{"error":"room not found"}`, 404)
		return
	}
	conn, err := websocket.Accept(w, req, nil)
	if err != nil {
		return
	}
	user := auth.From(req)
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	c := &client{user: user, send: make(chan []byte, 32), cancel: cancel}
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

func (h *Hub) dispatch(r *Room, c *client, m inMsg) {
	switch m.Type {
	case "ping":
		select {
		case c.send <- marshal(map[string]any{"type": "pong", "t": m.T, "serverTime": nowMs()}):
		default: // ponytail: dropped frame beats leaked goroutine; client re-pings in 10s
		}

	case "chat":
		// In-memory broadcast only; the ring buffer + hello history land in task 5.
		if len(m.Body) == 0 || len(m.Body) > 2000 {
			return
		}
		now := time.Now().Unix()
		r.mu.Lock()
		r.broadcast(marshal(map[string]any{"type": "chat", "userId": c.user.ID, "username": c.user.Username, "body": m.Body, "createdAt": now}))
		r.mu.Unlock()

	case "start":
		if !h.mediaReady(m.MediaID) {
			select {
			case c.send <- marshal(map[string]any{"type": "error", "body": "media not ready"}):
			default:
			}
			return
		}
		st := NewWatch(m.MediaID, nowMs())
		r.mu.Lock()
		r.watch = &st
		r.broadcast(marshal(map[string]any{"type": "activity", "activity": r.activityJSON()}))
		r.mu.Unlock()

	case "end":
		r.mu.Lock()
		if r.watch != nil {
			r.watch = nil
			r.broadcast(marshal(map[string]any{"type": "activity", "activity": nil}))
		}
		r.mu.Unlock()

	case "intent":
		r.mu.Lock()
		if r.watch != nil {
			if next, err := r.watch.Apply(m.Action, m.Position, nowMs()); err == nil {
				r.watch = &next
				r.broadcast(marshal(map[string]any{"type": "activity", "activity": r.activityJSON()}))
			}
		}
		r.mu.Unlock()
	}
}

// mediaReady reports whether a media row exists and is ready to play.
func (h *Hub) mediaReady(id int64) bool {
	var status string
	return h.db.QueryRow(`SELECT status FROM media WHERE id=?`, id).Scan(&status) == nil && status == "ready"
}
