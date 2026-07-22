package live

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"together/internal/auth"
)

const (
	roomNameMax    = 64
	guestNameMax   = 32
	participantCap = 12
	ownerRoomCap   = 10
	roomCap        = 100
)

// GuestSession is a hub-level (not per-connection) identity minted by
// POST /api/rooms/join. It dies at room teardown and survives socket drops —
// a guest cookie is the durable half of a guest's presence; the WS `client`
// (task 5) is the transient half.
type GuestSession struct {
	guestID string
	roomID  string
	name    string
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// Routes mounts the room lifecycle surface. Rooms are hub memory, so these
// replace V1's DB-backed internal/api/rooms.go. All are account-authed; the
// host check lives inside delete/regenerate.
func (h *Hub) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/rooms", auth.Require(h.db, false, h.listRooms))
	mux.HandleFunc("POST /api/rooms", auth.Require(h.db, false, h.createRoom))
	mux.HandleFunc("DELETE /api/rooms/{id}", auth.Require(h.db, false, h.deleteRoom))
	mux.HandleFunc("POST /api/rooms/{id}/token", auth.Require(h.db, false, h.regenToken))

	// Public join surface: unauthenticated by design (§4.3) — unguessability
	// of the room id / join token is the rate limiter (SPEC §9.11).
	mux.HandleFunc("POST /api/rooms/join", h.joinRoom)

	// GET /api/rooms/join/{token} and GET /api/rooms/{id}/meta both carry a
	// dynamic segment but in different positions ("join" vs {id}, {token} vs
	// "meta"); net/http's ServeMux (Go 1.22+) refuses to register two GET
	// patterns where neither is uniformly more specific than the other. One
	// wildcard route dispatches by hand instead of fighting the mux over it.
	//
	mux.HandleFunc("GET /api/rooms/{tail...}", h.roomsGetDispatch)
}

func (h *Hub) roomsGetDispatch(w http.ResponseWriter, r *http.Request) {
	tail := r.PathValue("tail")
	switch {
	case strings.HasPrefix(tail, "join/"):
		r.SetPathValue("token", strings.TrimPrefix(tail, "join/"))
		h.peekRoom(w, r)
	case strings.HasSuffix(tail, "/meta"):
		r.SetPathValue("id", strings.TrimSuffix(tail, "/meta"))
		h.RequireRoom(h.roomMeta)(w, r)
	case strings.HasSuffix(tail, "/token"):
		r.SetPathValue("id", strings.TrimSuffix(tail, "/token"))
		auth.Require(h.db, false, h.roomToken)(w, r)
	default:
		writeErr(w, 404, "not found")
	}
}

// secureCookie mirrors auth.secureCookie (unexported, can't be reused from
// here): Secure once TLS-terminated by Caddy, plain http only in dev/tests.
func secureCookie(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// guestCtxKey scopes the context value RequireRoom/RequireRoomMedia attach
// for guest connections. Deliberately a separate, package-private key from
// auth's own (which live cannot construct — it's unexported in package
// auth): the account path below delegates to auth.Require itself so
// downstream auth.From(r) keeps working unmodified for account connections.
type guestCtxKey struct{}

// GuestFrom returns the GuestSession a request authenticated as, if it came
// through RequireRoom/RequireRoomMedia as a guest rather than an account. ok
// is false for account connections and for requests that never passed one of
// those gates.
func GuestFrom(r *http.Request) (gs *GuestSession, ok bool) {
	gs, ok = r.Context().Value(guestCtxKey{}).(*GuestSession)
	return gs, ok
}

// requireGuestOr is the shared room-scoped gate behind RequireRoom and
// RequireRoomMedia (ARCHITECTURE §2, §4.4): a valid account session (any
// account — accounts have library-wide access) passes unconditionally,
// delegated to auth.Require so it also installs auth's own request context.
// Absent an account session, a together_guest cookie must resolve to a live
// GuestSession that satisfies match; on success the session is attached via
// guestCtxKey. No credential at all → 401. A guest cookie that fails match
// (wrong room/media, unknown token, or a torn-down room) → 404 — the same
// body as "not found", so a guest probing outside its room gets no oracle
// (§8, no-oracle — the same discipline as roomByToken).
func (h *Hub) requireGuestOr(next http.HandlerFunc, match func(r *http.Request, gs *GuestSession) bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("session"); err == nil {
			if _, err := auth.UserByToken(h.db, c.Value); err == nil {
				auth.Require(h.db, false, next)(w, r)
				return
			}
		}

		c, err := r.Cookie("together_guest")
		if err != nil {
			writeErr(w, 401, "unauthorized")
			return
		}
		h.mu.Lock()
		gs, ok := h.guests[c.Value]
		h.mu.Unlock()
		if !ok || !match(r, gs) {
			writeErr(w, 404, "not found")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), guestCtxKey{}, gs)))
	}
}

// RequireRoom gates room-id routes — /ws/{id} and /api/rooms/{id}/meta —
// where the {id} path segment IS the room id: a guest passes iff its
// session's roomID matches that id.
func (h *Hub) RequireRoom(next http.HandlerFunc) http.HandlerFunc {
	return h.requireGuestOr(next, func(r *http.Request, gs *GuestSession) bool {
		return gs.roomID == r.PathValue("id")
	})
}

// RequireRoomMedia gates media-byte routes — /media/{id}/download,
// /media/{id}/stream, /media/{id}/subs/{sid} — where {id} is a MEDIA id, not
// a room id: a guest passes iff its room still exists and that room's
// mediaID equals the path media id. A guest session grants exactly one
// room's media, nothing else in the library (§4.4).
func (h *Hub) RequireRoomMedia(next http.HandlerFunc) http.HandlerFunc {
	return h.requireGuestOr(next, func(r *http.Request, gs *GuestSession) bool {
		mediaID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			return false
		}
		rm, ok := h.getRoom(gs.roomID)
		if !ok {
			return false
		}
		rm.mu.Lock()
		defer rm.mu.Unlock()
		return rm.mediaID != 0 && rm.mediaID == mediaID
	})
}

// sanitizeGuestName strips control characters and enforces the 1–32 char
// bound from §3.2. Byte length, not rune count — guest names are display
// strings, not a security boundary, and ASCII is the expected common case.
func sanitizeGuestName(raw string) (string, error) {
	var b strings.Builder
	for _, r := range raw {
		if !unicode.IsControl(r) {
			b.WriteRune(r)
		}
	}
	name := b.String()
	if len(name) < 1 || len(name) > guestNameMax {
		return "", fmt.Errorf("name must be 1-%d characters", guestNameMax)
	}
	return name, nil
}

// roomByToken finds the room whose current joinToken matches. Callers get an
// undifferentiated miss for both a dead (regenerated-away) token and one that
// never existed — there is no oracle distinguishing the two (§8, no-oracle).
func (h *Hub) roomByToken(token string) (*Room, bool) {
	h.mu.Lock()
	rm, ok := h.tokens[token]
	h.mu.Unlock()
	return rm, ok
}

// suffixNameLocked appends " (2)", " (3)"... on collision with a currently
// connected guest's name in the same room. Caller holds h.mu. Collisions are
// checked only against live entries in h.guests — a torn-down room's guests
// are gone with it, and a departed guest (removed from this map, task 6)
// frees its name immediately; nothing here remembers past occupants.
func (h *Hub) suffixNameLocked(roomID, name string) string {
	taken := map[string]bool{}
	for _, gs := range h.guests {
		if gs.roomID == roomID {
			taken[gs.name] = true
		}
	}
	if !taken[name] {
		return name
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s (%d)", name, n)
		if !taken[candidate] {
			return candidate
		}
	}
}

func (h *Hub) joinRoom(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
		Name  string `json:"name"`
	}
	json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in)

	rm, ok := h.roomByToken(in.Token)
	if !ok {
		writeErr(w, 404, "room not found")
		return
	}

	// A live guest cookie already scoped to this room returns the same
	// identity verbatim (FR-5): no new session, no re-suffix, name unminted.
	if c, err := r.Cookie("together_guest"); err == nil {
		h.mu.Lock()
		gs, exists := h.guests[c.Value]
		h.mu.Unlock()
		if exists && gs.roomID == rm.id {
			writeJSON(w, 200, map[string]any{"roomId": rm.id})
			return
		}
	}

	name, err := sanitizeGuestName(in.Name)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}

	rm.mu.Lock()
	clientCount := len(rm.clients)
	rm.mu.Unlock()

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rooms[rm.id] != rm || h.tokens[in.Token] != rm {
		writeErr(w, 404, "room not found")
		return
	}

	guestCount := 0
	for _, gs := range h.guests {
		if gs.roomID == rm.id {
			guestCount++
		}
	}
	// ponytail: clientCount and guestCount are sampled under two separate
	// locks (rm.mu then h.mu), so a concurrent join/connect can slip past the
	// cap by one or two. Acceptable for a soft UX limit on a ≤10-user app;
	// upgrade path is folding both maps under one lock if it ever bites.
	if clientCount+guestCount >= participantCap {
		writeErr(w, 409, "room is full")
		return
	}

	name = h.suffixNameLocked(rm.id, name)
	gs := &GuestSession{guestID: randHex(8), roomID: rm.id, name: name}
	token := randHex(16)
	h.guests[token] = gs

	http.SetCookie(w, &http.Cookie{Name: "together_guest", Value: token, Path: "/",
		HttpOnly: true, Secure: secureCookie(r), SameSite: http.SameSiteLaxMode})
	writeJSON(w, 200, map[string]any{"roomId": rm.id})
}

func (h *Hub) peekRoom(w http.ResponseWriter, r *http.Request) {
	rm, ok := h.roomByToken(r.PathValue("token"))
	if !ok {
		writeErr(w, 404, "room not found")
		return
	}
	rm.mu.Lock()
	name := rm.name
	rm.mu.Unlock()
	writeJSON(w, 200, map[string]any{"roomName": name})
}

type mediaMeta struct {
	ID        int64   `json:"id"`
	Title     string  `json:"title"`
	SizeBytes int64   `json:"sizeBytes"`
	Duration  float64 `json:"duration"`
}

type subtitleMeta struct {
	ID    int64  `json:"id"`
	Label string `json:"label"`
}

func (h *Hub) roomMeta(w http.ResponseWriter, r *http.Request) {
	rm, ok := h.getRoom(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "room not found")
		return
	}
	rm.mu.Lock()
	name, kind, mediaID := rm.name, rm.kind, rm.mediaID
	rm.mu.Unlock()
	if mediaID == 0 {
		writeJSON(w, 200, map[string]any{"name": name, "kind": kind, "media": nil, "subtitles": []subtitleMeta{}})
		return
	}

	media := mediaMeta{ID: mediaID}
	err := h.db.QueryRow(`SELECT title, coalesce(size_bytes,0), coalesce(duration,0) FROM media WHERE id=?`, mediaID).
		Scan(&media.Title, &media.SizeBytes, &media.Duration)
	if err != nil {
		writeErr(w, 404, "media not found")
		return
	}

	subs := []subtitleMeta{}
	rows, err := h.db.Query(`SELECT id, label FROM subtitles WHERE media_id=?`, mediaID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var s subtitleMeta
			if rows.Scan(&s.ID, &s.Label) == nil {
				subs = append(subs, s)
			}
		}
	}

	writeJSON(w, 200, map[string]any{"name": name, "kind": kind, "media": media, "subtitles": subs})
}

type roomListItem struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	MediaID      int64  `json:"mediaId"`
	MediaTitle   string `json:"mediaTitle"`
	Kind         string `json:"kind"`
	Participants int    `json:"participants"`
}

func (h *Hub) listRooms(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	rooms := make([]*Room, 0, len(h.rooms))
	for _, rm := range h.rooms {
		rooms = append(rooms, rm)
	}
	h.mu.Unlock()

	out := []roomListItem{}
	for _, rm := range rooms {
		rm.mu.Lock()
		item := roomListItem{ID: rm.id, Name: rm.name, MediaID: rm.mediaID, MediaTitle: rm.mediaTitle, Kind: rm.kind, Participants: len(rm.presence())}
		rm.mu.Unlock()
		out = append(out, item)
	}
	writeJSON(w, 200, out)
}

func (h *Hub) createRoom(w http.ResponseWriter, r *http.Request) {
	var in struct {
		MediaID int64  `json:"mediaId"`
		Name    string `json:"name"`
	}
	json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in)
	name := strings.TrimSpace(in.Name)
	if len(name) > roomNameMax {
		writeErr(w, 400, "room name too long (max 64 chars)")
		return
	}

	var kind, title string
	if in.MediaID != 0 {
		var ok bool
		kind, title, ok = h.readyMedia(in.MediaID)
		if !ok {
			writeErr(w, 404, "media not found or not ready")
			return
		}
	}
	if name == "" {
		name = title
		if name == "" {
			name = "Untitled room"
		}
	}

	h.mu.Lock()
	ownerID := auth.From(r).ID
	ownerRooms := 0
	for _, existing := range h.rooms {
		if existing.ownerID == ownerID {
			ownerRooms++
		}
	}
	if ownerRooms >= ownerRoomCap || len(h.rooms) >= roomCap {
		h.mu.Unlock()
		writeErr(w, http.StatusTooManyRequests, "room limit reached")
		return
	}

	rm := &Room{
		id:         randHex(8),
		name:       name,
		ownerID:    ownerID,
		mediaID:    in.MediaID,
		mediaTitle: title,
		kind:       kind,
		joinToken:  randHex(16),
		// Playback starts only after media is selected.
		clients:      map[*client]bool{},
		reconnecting: map[string]*reconnectingClient{},
		chat:         []ChatMsg{},
	}
	// mediaId remains accepted for existing API clients. New clients omit it
	// and choose media from inside the room.
	if in.MediaID != 0 {
		st := NewWatch(in.MediaID, nowMs())
		rm.watch = &st
	}
	// Creation is an empty state too: a room with no first connection must
	// expire instead of reserving capacity indefinitely.
	rm.emptyTimer = time.AfterFunc(h.idle, func() { h.fireEmpty(rm) })
	h.rooms[rm.id] = rm
	h.tokens[rm.joinToken] = rm
	h.mu.Unlock()

	writeJSON(w, 201, map[string]any{"id": rm.id, "joinToken": rm.joinToken})
}

func (h *Hub) deleteRoom(w http.ResponseWriter, r *http.Request) {
	rm, ok := h.getRoom(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "room not found")
		return
	}
	u := auth.From(r)
	if !h.isHost(rm, u) {
		writeErr(w, 403, "only the host can end this room")
		return
	}
	h.teardown(rm)
	writeJSON(w, 200, map[string]any{})
}

func (h *Hub) regenToken(w http.ResponseWriter, r *http.Request) {
	rm, ok := h.getRoom(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "room not found")
		return
	}
	if !h.isHost(rm, auth.From(r)) {
		writeErr(w, 403, "only the host can regenerate the link")
		return
	}
	h.mu.Lock()
	if h.rooms[rm.id] != rm {
		h.mu.Unlock()
		writeErr(w, 404, "room not found")
		return
	}
	rm.mu.Lock()
	old := rm.joinToken
	rm.joinToken = randHex(16)
	tok := rm.joinToken
	delete(h.tokens, old)
	h.tokens[tok] = rm
	rm.mu.Unlock()
	h.mu.Unlock()
	writeJSON(w, 200, map[string]any{"joinToken": tok})
}

// roomToken returns the current invite token only to a room host. Metadata is
// intentionally available to any room participant, so exposing the token from
// roomMeta would leak the invite to guests and non-host account users.
func (h *Hub) roomToken(w http.ResponseWriter, r *http.Request) {
	rm, ok := h.getRoom(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "room not found")
		return
	}
	if !h.isHost(rm, auth.From(r)) {
		writeErr(w, 403, "only the host can view the link")
		return
	}
	rm.mu.Lock()
	tok := rm.joinToken
	rm.mu.Unlock()
	writeJSON(w, 200, map[string]any{"joinToken": tok})
}

// isHost is true for the room owner or any admin (admins delete anything, V1 rule).
func (h *Hub) isHost(rm *Room, u auth.User) bool {
	return rm.ownerID == u.ID || u.Role == "admin"
}

// teardown is the ONE code path behind all three triggers (host DELETE,
// empty-timer fire, per-room panic recovery — ARCHITECTURE §5/§8). Order:
// broadcast room_closed -> close sockets -> cancel timer -> delete room from
// map -> drop its guest sessions -> token forgotten. Nothing touches SQLite.
//
// "Close sockets" here means closing each client's send channel (via
// closeOnce, shared with the Handle defer so whichever runs first wins) —
// NOT calling c.cancel(). The room_closed frame was just enqueued ahead of
// that close in the same channel; the writer goroutine drains it before it
// notices the channel is closed, so the frame is actually delivered before
// the connection goes away. A hard ctx cancel here would race that delivery
// and could drop the frame instead.
//
// rm.closed makes the whole thing idempotent: a host DELETE racing a timer
// fire, or a panic while another trigger is mid-teardown, is a no-op on
// whichever call arrives second.
func (h *Hub) teardown(rm *Room) {
	// The hub lock serializes token-index updates with regeneration. Keeping
	// this order (hub, then room) prevents teardown from resurrecting a token.
	h.mu.Lock()
	rm.mu.Lock()
	if rm.closed {
		rm.mu.Unlock()
		h.mu.Unlock()
		return
	}
	rm.closed = true
	rm.broadcast(marshal(map[string]any{"type": "room_closed"}))
	for c := range rm.clients {
		c.closeOnce.Do(func() { close(c.send) })
	}
	if rm.emptyTimer != nil {
		rm.emptyTimer.Stop()
	}
	for _, reconnecting := range rm.reconnecting {
		reconnecting.timer.Stop()
	}
	rm.mu.Unlock()

	delete(h.rooms, rm.id)
	delete(h.tokens, rm.joinToken)
	for tok, gs := range h.guests {
		if gs.roomID == rm.id {
			delete(h.guests, tok)
		}
	}
	h.mu.Unlock()
}
