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
	// chat is an in-memory drop-oldest ring of the latest 200 messages
	// (FR-25), oldest first. A plain slice capped by re-slicing on append is
	// enough at this size — no circular-buffer indexing needed. Never
	// persisted: it dies with the room like everything else in §3.2.
	chat []ChatMsg
}

// ChatMsg is both the wire shape of an outbound `chat` frame's payload (sans
// the `type` envelope) and a room's ring buffer entry — the same struct
// serves hello.chat[] and a fresh broadcast so the two never drift apart.
type ChatMsg struct {
	Name      string `json:"name"`
	IsGuest   bool   `json:"isGuest"`
	Body      string `json:"body"`
	CreatedAt int64  `json:"createdAt"` // unix seconds
}

// Presence is one live connection's row in a `presence`/`hello.users` frame.
// One entry per connection, not per identity — two tabs (or two hosts, task
// 20) each get their own row with their own status.
type Presence struct {
	Name    string `json:"name"`
	IsHost  bool   `json:"isHost"`
	IsGuest bool   `json:"isGuest"`
	Status  string `json:"status"`
}

const chatRingCap = 200

// appendChat drops the oldest entry once the ring exceeds chatRingCap.
// Caller holds r.mu.
func (r *Room) appendChat(m ChatMsg) {
	r.chat = append(r.chat, m)
	if len(r.chat) > chatRingCap {
		r.chat = r.chat[len(r.chat)-chatRingCap:]
	}
}

type client struct {
	user    auth.User
	name    string // display name: account username, or guest's post-suffix name
	isGuest bool
	guestID string // set for guest connections only
	status  string // downloading|file_ready|in_sync, client-reported, per-connection
	send    chan []byte
	cancel  func() // stops this connection; set in Handle, invoked by teardown
}

// isHostOf reports live host status: the room's ownerID is fixed at creation
// (never mutated), so reading it here needs no lock — matching isHost in
// rooms.go, which does the same.
func (c *client) isHostOf(r *Room) bool { return !c.isGuest && c.user.ID == r.ownerID }

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

// presence returns one Presence per live connection — deliberately not
// deduped by identity (§3.2: two tabs/hosts coexist, each with its own
// per-connection status). Caller holds r.mu.
func (r *Room) presence() []Presence {
	out := []Presence{}
	for c := range r.clients {
		out = append(out, Presence{Name: c.name, IsHost: c.isHostOf(r), IsGuest: c.isGuest, Status: c.status})
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
	State    string  `json:"state"` // status frame: downloading|file_ready|in_sync
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
	name, isGuest, guestID := user.Username, false, ""
	if gs, ok := GuestFrom(req); ok {
		name, isGuest, guestID = gs.name, true, gs.guestID
	}
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	// status starts "downloading" (the ◌ rung — no local file yet; §3.2).
	c := &client{user: user, name: name, isGuest: isGuest, guestID: guestID, status: "downloading", send: make(chan []byte, 32), cancel: cancel}
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
	chatHistory := make([]ChatMsg, len(r.chat))
	copy(chatHistory, r.chat) // oldest→newest, same order as the ring
	c.send <- marshal(map[string]any{
		"type":     "hello",
		"you":      map[string]any{"name": c.name, "isHost": c.isHostOf(r), "isGuest": c.isGuest},
		"users":    r.presence(),
		"activity": r.activityJSON(),
		"chat":     chatHistory,
		"room":     map[string]any{"name": r.name, "kind": r.kind, "mediaId": r.mediaID},
	})
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
		// In-memory only — no messages table exists (V1 chat persistence is
		// deleted); this case performs zero SQL.
		if len(m.Body) == 0 || len(m.Body) > 2000 {
			select {
			case c.send <- marshal(map[string]any{"type": "error", "body": "chat message must be 1-2000 characters"}):
			default: // ponytail: dropped frame beats leaked goroutine; the connection stays open regardless
			}
			return
		}
		msg := ChatMsg{Name: c.name, IsGuest: c.isGuest, Body: m.Body, CreatedAt: time.Now().Unix()}
		r.mu.Lock()
		r.appendChat(msg)
		r.broadcast(marshal(map[string]any{"type": "chat", "name": msg.Name, "isGuest": msg.IsGuest, "body": msg.Body, "createdAt": msg.CreatedAt}))
		r.mu.Unlock()

	case "status":
		if m.State != "downloading" && m.State != "file_ready" && m.State != "in_sync" {
			return // presence-only, malformed states are silently ignored — never reaches watch.Apply
		}
		r.mu.Lock()
		c.status = m.State
		r.broadcast(marshal(map[string]any{"type": "presence", "users": r.presence()}))
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
