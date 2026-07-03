package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"together/internal/auth"
	"together/internal/db"
)

// client spins up a fresh DB + server, seeds an admin user, and returns a
// client logged in as that admin, the server, and the DB (for tests that
// need to set up extra users/rows directly).
func client(t *testing.T) (*http.Client, *httptest.Server, *sql.DB) {
	t.Helper()
	d, _ := db.Open(filepath.Join(t.TempDir(), "t.db"))
	t.Cleanup(func() { d.Close() })
	auth.Seed(d, "admin", "password")
	mux := http.NewServeMux()
	auth.Routes(mux, d)
	Routes(mux, d)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return login(t, ts, "admin", "password"), ts, d
}

// login authenticates an existing user and returns a client carrying the session cookie.
func login(t *testing.T, ts *httptest.Server, username, password string) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	r, err := c.Post(ts.URL+"/api/login", "application/json",
		strings.NewReader(fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)))
	if err != nil || r.StatusCode != 200 {
		t.Fatalf("login %s: %v %d", username, err, statusOf(r))
	}
	return c
}

// addMember inserts a member user directly into the DB (bypassing the
// invite flow) and returns a client logged in as them.
func addMember(t *testing.T, d *sql.DB, ts *httptest.Server, username string) *http.Client {
	t.Helper()
	h, s := auth.Hash("password")
	if _, err := d.Exec(`INSERT INTO users (username, pass_hash, salt, role) VALUES (?,?,?,'member')`, username, h, s); err != nil {
		t.Fatalf("insert user %s: %v", username, err)
	}
	return login(t, ts, username, "password")
}

func createRoom(t *testing.T, c *http.Client, ts *httptest.Server, name string) int64 {
	t.Helper()
	r, err := c.Post(ts.URL+"/api/rooms", "application/json", strings.NewReader(fmt.Sprintf(`{"name":%q}`, name)))
	if err != nil || r.StatusCode != 200 {
		t.Fatalf("create room: %v %d", err, statusOf(r))
	}
	var room struct{ ID int64 }
	json.NewDecoder(r.Body).Decode(&room)
	return room.ID
}

func listRoomIDs(t *testing.T, c *http.Client, ts *httptest.Server) []int64 {
	t.Helper()
	r, _ := c.Get(ts.URL + "/api/rooms")
	var rooms []map[string]any
	json.NewDecoder(r.Body).Decode(&rooms)
	ids := make([]int64, len(rooms))
	for i, rm := range rooms {
		ids[i] = int64(rm["id"].(float64))
	}
	return ids
}

func deleteRoom(t *testing.T, c *http.Client, ts *httptest.Server, id int64) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/rooms/%d", ts.URL, id), nil)
	if err != nil {
		t.Fatalf("build delete request: %v", err)
	}
	r, err := c.Do(req)
	if err != nil {
		t.Fatalf("delete room %d: %v", id, err)
	}
	return r
}

func contains(ids []int64, id int64) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func statusOf(r *http.Response) int {
	if r == nil {
		return 0
	}
	return r.StatusCode
}

func TestRoomLifecycle(t *testing.T) {
	c, ts, _ := client(t)
	r, err := c.Post(ts.URL+"/api/rooms", "application/json", strings.NewReader(`{"name":"movie night"}`))
	if err != nil || r.StatusCode != 200 {
		t.Fatalf("create: %v %d", err, r.StatusCode)
	}
	var room struct{ ID int64 }
	json.NewDecoder(r.Body).Decode(&room)

	r, _ = c.Get(ts.URL + "/api/rooms")
	var rooms []map[string]any
	json.NewDecoder(r.Body).Decode(&rooms)
	if len(rooms) != 1 || rooms[0]["name"] != "movie night" {
		t.Fatalf("list: %+v", rooms)
	}

	r, _ = c.Get(ts.URL + "/api/rooms/1/messages")
	if r.StatusCode != 200 {
		t.Fatalf("messages: %d", r.StatusCode)
	}
}

// TestRoomDeleteAuthorization covers Finding 2: owners and admins may delete
// a room, other members may not.
func TestRoomDeleteAuthorization(t *testing.T) {
	admin, ts, d := client(t)
	alice := addMember(t, d, ts, "alice")
	bob := addMember(t, d, ts, "bob")

	// Owner deletes own room -> 204, gone from list.
	roomID := createRoom(t, alice, ts, "alice's room")
	if r := deleteRoom(t, alice, ts, roomID); r.StatusCode != 204 {
		t.Fatalf("owner delete: got %d, want 204", r.StatusCode)
	}
	if ids := listRoomIDs(t, alice, ts); contains(ids, roomID) {
		t.Fatalf("room %d still listed after owner delete: %v", roomID, ids)
	}

	// Non-owner member deletes someone else's room -> 404, still listed.
	roomID = createRoom(t, alice, ts, "alice's second room")
	if r := deleteRoom(t, bob, ts, roomID); r.StatusCode != 404 {
		t.Fatalf("non-owner delete: got %d, want 404", r.StatusCode)
	}
	if ids := listRoomIDs(t, alice, ts); !contains(ids, roomID) {
		t.Fatalf("room %d missing after failed non-owner delete: %v", roomID, ids)
	}

	// Admin deletes a member's room -> 204.
	if r := deleteRoom(t, admin, ts, roomID); r.StatusCode != 204 {
		t.Fatalf("admin delete: got %d, want 204", r.StatusCode)
	}
	if ids := listRoomIDs(t, alice, ts); contains(ids, roomID) {
		t.Fatalf("room %d still listed after admin delete: %v", roomID, ids)
	}
}

// TestRoomMessagesAscending covers Finding 3: messages come back ascending
// by id (not insertion order, not created_at) with fields populated.
func TestRoomMessagesAscending(t *testing.T) {
	admin, ts, d := client(t)
	roomID := createRoom(t, admin, ts, "chat room")

	var adminID int64
	if err := d.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&adminID); err != nil {
		t.Fatalf("lookup admin id: %v", err)
	}

	// Insert with out-of-order ids so a pass would only work off true id order.
	inserts := []struct {
		id   int64
		body string
	}{
		{30, "third"},
		{10, "first"},
		{20, "second"},
	}
	for _, m := range inserts {
		if _, err := d.Exec(`INSERT INTO messages (id, room_id, user_id, body, created_at) VALUES (?,?,?,?,?)`,
			m.id, roomID, adminID, m.body, 1000+m.id); err != nil {
			t.Fatalf("insert message %d: %v", m.id, err)
		}
	}

	r, err := admin.Get(fmt.Sprintf("%s/api/rooms/%d/messages", ts.URL, roomID))
	if err != nil || r.StatusCode != 200 {
		t.Fatalf("messages: %v %d", err, statusOf(r))
	}
	var msgs []struct {
		ID        int64  `json:"id"`
		Username  string `json:"username"`
		Body      string `json:"body"`
		CreatedAt int64  `json:"createdAt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&msgs); err != nil {
		t.Fatalf("decode messages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d: %+v", len(msgs), msgs)
	}
	wantBodies := []string{"first", "second", "third"}
	for i, m := range msgs {
		if m.Body != wantBodies[i] {
			t.Fatalf("message %d: got body %q, want %q (full order: %+v)", i, m.Body, wantBodies[i], msgs)
		}
		if m.Username != "admin" {
			t.Fatalf("message %d: got username %q, want admin", i, m.Username)
		}
		if m.CreatedAt == 0 {
			t.Fatalf("message %d: createdAt not populated", i)
		}
	}
}
