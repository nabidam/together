package live

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unicode"

	"together/internal/auth"
)

const (
	roomNameMax    = 64
	guestNameMax   = 32
	participantCap = 12
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
	// ponytail: meta needs the room-auth gate (account session OR guest
	// session scoped to this room), which is task 4's live.RequireRoom.
	// Mounted behind plain account auth for now so the route exists and its
	// response shape is locked in; task 4 swaps the middleware only.
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
		auth.Require(h.db, false, h.roomMeta)(w, r)
	default:
		writeErr(w, 404, "not found")
	}
}

// secureCookie mirrors auth.secureCookie (unexported, can't be reused from
// here): Secure once TLS-terminated by Caddy, plain http only in dev/tests.
func secureCookie(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
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
	rooms := make([]*Room, 0, len(h.rooms))
	for _, rm := range h.rooms {
		rooms = append(rooms, rm)
	}
	h.mu.Unlock()

	for _, rm := range rooms {
		rm.mu.Lock()
		match := rm.joinToken == token
		rm.mu.Unlock()
		if match {
			return rm, true
		}
	}
	return nil, false
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

	var kind, title, status string
	err := h.db.QueryRow(`SELECT kind, title, status FROM media WHERE id=?`, in.MediaID).Scan(&kind, &title, &status)
	if err != nil || status != "ready" {
		writeErr(w, 404, "media not found or not ready")
		return
	}
	if name == "" {
		name = title
	}

	st := NewWatch(in.MediaID, nowMs())
	rm := &Room{
		id:         randHex(8),
		name:       name,
		ownerID:    auth.From(r).ID,
		mediaID:    in.MediaID,
		mediaTitle: title,
		kind:       kind,
		joinToken:  randHex(16),
		watch:      &st, // creating a room starts its watch activity (§4.3)
		clients:    map[*client]bool{},
	}

	h.mu.Lock()
	h.rooms[rm.id] = rm
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
	rm.mu.Lock()
	rm.joinToken = randHex(16)
	tok := rm.joinToken
	rm.mu.Unlock()
	writeJSON(w, 200, map[string]any{"joinToken": tok})
}

// isHost is true for the room owner or any admin (admins delete anything, V1 rule).
func (h *Hub) isHost(rm *Room, u auth.User) bool {
	return rm.ownerID == u.ID || u.Role == "admin"
}

// teardown removes a room from the hub and signals its live connections to
// close. Each connection's own Handle defer performs the channel close and
// client removal, so teardown must not close send here (double close panics).
// This is the v0 path (task 2); task 6 adds the room_closed broadcast, empty
// timer, and per-room recover around this same code path.
func (h *Hub) teardown(rm *Room) {
	h.mu.Lock()
	delete(h.rooms, rm.id)
	h.mu.Unlock()

	rm.mu.Lock()
	for c := range rm.clients {
		c.cancel() // unblocks the reader/writer; its defer closes send + drops the client
	}
	rm.mu.Unlock()
}
