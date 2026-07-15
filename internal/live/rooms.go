package live

import (
	"encoding/json"
	"net/http"
	"strings"

	"together/internal/auth"
)

const roomNameMax = 64

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
