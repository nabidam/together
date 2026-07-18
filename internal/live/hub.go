package live

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log"
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
	mu     sync.Mutex // guards rooms, tokens, and guests maps
	rooms  map[string]*Room
	tokens map[string]*Room         // current join token -> room; stale tokens are removed
	guests map[string]*GuestSession // keyed by the together_guest cookie token
	idle   time.Duration            // TOGETHER_ROOM_IDLE — empty-room close delay (ARCHITECTURE §9)
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

	// closed guards teardown idempotency (ARCHITECTURE §5): host DELETE,
	// empty-timer fire, and a panicking room's recover() all funnel through
	// teardown(), and any of the three can race the others. Once true, a
	// second teardown call is a no-op.
	closed bool
	// emptyTimer fires teardown after TOGETHER_ROOM_IDLE with zero live WS
	// connections. Created lazily on the first last-client-disconnect;
	// Reset() on subsequent empties, Stop() on any join (AC-5.3: a rejoin
	// inside the idle window keeps the room alive).
	emptyTimer *time.Timer
	// panicTrigger is a test-only seam (task 6): if set, dispatch calls it
	// on every inbound frame, letting a test inject a panic into a specific
	// room's handling without a real bug, to exercise the per-room recover.
	panicTrigger func()
}

// withLock runs fn with rm.mu held and released via defer — critical for
// NFR-7: if fn panics, the deferred Unlock still runs during stack
// unwinding, so a per-room recover() further up the goroutine can safely
// re-acquire rm.mu in teardown() instead of deadlocking on a lock a plain
// Lock()/Unlock() pair would have left held forever.
func (r *Room) withLock(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn()
}

// testPanicHook returns the room's panicTrigger under lock (set by tests via
// setPanicTrigger, from a different goroutine than the one that reads it —
// the lock is what makes that read/write race-detector-clean).
func (r *Room) testPanicHook() func() {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.panicTrigger
}

func (r *Room) setPanicTrigger(fn func()) {
	r.mu.Lock()
	r.panicTrigger = fn
	r.mu.Unlock()
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
	cancel  func() // stops this connection's context; set in Handle
	// closeOnce guards c.send: both the Handle defer (normal disconnect) and
	// teardown() (host end / timer / panic) want to close it, and closing a
	// channel twice panics. Whichever runs first wins; the other is a no-op.
	closeOnce sync.Once
}

// isHostOf reports live host status: the room's ownerID is fixed at creation
// (never mutated), so reading it here needs no lock — matching isHost in
// rooms.go, which does the same.
func (c *client) isHostOf(r *Room) bool { return !c.isGuest && c.user.ID == r.ownerID }

// NewHub builds a hub with idle as its TOGETHER_ROOM_IDLE (ARCHITECTURE §9):
// how long an empty room (zero live WS connections) survives before its
// emptyTimer fires teardown. Callers pass a short duration in tests (AC-5.3)
// and the real config value (default 30m) in main.
func NewHub(d *sql.DB, idle time.Duration) *Hub {
	return &Hub{db: d, rooms: map[string]*Room{}, tokens: map[string]*Room{}, guests: map[string]*GuestSession{}, idle: idle}
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
	// Per-room panic guard (NFR-7): this goroutine is the one that reads and
	// dispatches inbound frames for this connection, so it's the one that
	// can panic on room logic. Deferred first == runs last, after the
	// cleanup/cancel defers below have already unwound this connection.
	defer h.recoverRoom(r)

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
				break
			}
		}
		// c.send is only ever closed once (closeOnce), by whichever of the
		// Handle defer below or teardown() gets there first, and only after
		// every buffered frame — including a teardown's room_closed — has
		// been enqueued. Draining the range loop to completion before
		// closing the connection is what guarantees that frame is actually
		// delivered instead of dropped by a hard ctx cancel (see teardown).
		conn.CloseNow()
	}()

	r.mu.Lock()
	if r.closed { // room torn down between getRoom and here — bail without joining
		r.mu.Unlock()
		conn.CloseNow()
		return
	}
	// Admission and insertion are one critical section: two handshakes
	// racing for the final slot must not both observe len(clients) == 11.
	// Reject before hello/presence so an over-cap tab never becomes visible.
	if len(r.clients) >= participantCap {
		r.mu.Unlock()
		conn.Close(websocket.StatusPolicyViolation, "room capacity reached")
		return
	}
	if r.emptyTimer != nil {
		r.emptyTimer.Stop() // any join keeps the room alive (AC-5.3)
	}
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
		c.closeOnce.Do(func() { close(c.send) })
		if !r.closed {
			r.broadcast(marshal(map[string]any{"type": "presence", "users": r.presence()}))
			if len(r.clients) == 0 {
				if r.emptyTimer == nil {
					r.emptyTimer = time.AfterFunc(h.idle, func() { h.fireEmpty(r) })
				} else {
					r.emptyTimer.Reset(h.idle)
				}
			}
		}
		r.mu.Unlock()
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
	// Test-only seam (task 6): lets a test inject a panic into this room's
	// handling to exercise the per-room recover, without a real crash bug.
	if fn := r.testPanicHook(); fn != nil {
		fn()
	}

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
		r.withLock(func() {
			r.appendChat(msg)
			r.broadcast(marshal(map[string]any{"type": "chat", "name": msg.Name, "isGuest": msg.IsGuest, "body": msg.Body, "createdAt": msg.CreatedAt}))
		})

	case "status":
		if m.State != "downloading" && m.State != "file_ready" && m.State != "in_sync" {
			return // presence-only, malformed states are silently ignored — never reaches watch.Apply
		}
		r.withLock(func() {
			c.status = m.State
			r.broadcast(marshal(map[string]any{"type": "presence", "users": r.presence()}))
		})

	case "start":
		// A room is permanently scoped to the media selected at creation.
		// Read the fixed id while holding the room lock so a mismatched start
		// cannot replace its activity with another ready library item.
		r.mu.Lock()
		matchesRoomMedia := m.MediaID == r.mediaID
		r.mu.Unlock()
		if !matchesRoomMedia {
			select {
			case c.send <- marshal(map[string]any{"type": "error", "body": "media does not match room"}):
			default:
			}
			return
		}
		if !h.mediaReady(m.MediaID) {
			select {
			case c.send <- marshal(map[string]any{"type": "error", "body": "media not ready"}):
			default:
			}
			return
		}
		st := NewWatch(m.MediaID, nowMs())
		r.withLock(func() {
			r.watch = &st
			r.broadcast(marshal(map[string]any{"type": "activity", "activity": r.activityJSON()}))
		})

	case "end":
		r.withLock(func() {
			if r.watch != nil {
				r.watch = nil
				r.broadcast(marshal(map[string]any{"type": "activity", "activity": nil}))
			}
		})

	case "intent":
		r.withLock(func() {
			if r.watch != nil {
				if next, err := r.watch.Apply(m.Action, m.Position, nowMs()); err == nil {
					r.watch = &next
					r.broadcast(marshal(map[string]any{"type": "activity", "activity": r.activityJSON()}))
				}
			}
		})
	}
}

// recoverRoom is the per-room panic guard (NFR-7, CONVENTIONS.md "Panics"):
// deferred by every goroutine that touches a room's state. A panic anywhere
// in that room's handling is logged with the room id, that one room is torn
// down, and the goroutine returns normally — every other room is on its own
// mutex and keeps serving untouched.
func (h *Hub) recoverRoom(r *Room) {
	if err := recover(); err != nil {
		log.Printf("room panic id=%s err=%v", r.id, err)
		h.teardown(r)
	}
}

// fireEmpty is the emptyTimer callback (runs on its own goroutine via
// time.AfterFunc). It re-checks under lock that the room is still empty
// before tearing down — a rejoin that raced the timer (Stop() lost the
// race, or fired just before a reconnect) must not kill a live room.
func (h *Hub) fireEmpty(r *Room) {
	defer h.recoverRoom(r)
	r.mu.Lock()
	stillEmpty := len(r.clients) == 0
	r.mu.Unlock()
	if stillEmpty {
		h.teardown(r)
	}
}

// mediaReady reports whether a media row exists and is ready to play.
func (h *Hub) mediaReady(id int64) bool {
	var status string
	return h.db.QueryRow(`SELECT status FROM media WHERE id=?`, id).Scan(&status) == nil && status == "ready"
}
