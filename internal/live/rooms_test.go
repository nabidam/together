package live

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"together/internal/auth"
	"together/internal/db"
)

// post is a small JSON helper returning status + decoded body.
func post(t *testing.T, ts, path, cookie, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", ts+path, strings.NewReader(body))
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func doReq(t *testing.T, method, ts, path, cookie string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, ts+path, nil)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func listRooms(t *testing.T, ts, cookie string) []map[string]any {
	t.Helper()
	req, _ := http.NewRequest("GET", ts+"/api/rooms", nil)
	req.Header.Set("Cookie", cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out []map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	return out
}

func TestCreateRoom_Validation(t *testing.T) {
	ts, _, _, alice, _ := newStack(t)

	// ready media → 201 with id + join token
	code, out := post(t, ts.URL, "/api/rooms", alice, `{"mediaId":1}`)
	if code != 201 {
		t.Fatalf("ready media want 201 got %d (%+v)", code, out)
	}
	id, _ := out["id"].(string)
	tok, _ := out["joinToken"].(string)
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(id) {
		t.Fatalf("room id must be 16 hex chars, got %q", id)
	}
	if !regexp.MustCompile(`^[0-9a-f]{32,}$`).MatchString(tok) {
		t.Fatalf("join token must be >=128-bit hex, got %q", tok)
	}

	// name defaults to media title
	rooms := listRooms(t, ts.URL, alice)
	if len(rooms) != 1 || rooms[0]["name"] != "The Movie" {
		t.Fatalf("name should default to media title: %+v", rooms)
	}

	// name >64 chars → 400
	long := `{"mediaId":1,"name":"` + strings.Repeat("x", 65) + `"}`
	if code, _ := post(t, ts.URL, "/api/rooms", alice, long); code != 400 {
		t.Fatalf("long name want 400 got %d", code)
	}

	// unknown media → 404
	if code, _ := post(t, ts.URL, "/api/rooms", alice, `{"mediaId":999}`); code != 404 {
		t.Fatalf("unknown media want 404 got %d", code)
	}
}

func TestCreateRoom_RejectsNonReadyMedia(t *testing.T) {
	ts, _, d, alice, _ := newStack(t)
	d.Exec(`INSERT INTO media (kind, title, status) VALUES ('video','Encoding','processing')`) // id 2
	if code, _ := post(t, ts.URL, "/api/rooms", alice, `{"mediaId":2}`); code != 404 {
		t.Fatalf("non-ready media want 404 got %d", code)
	}
}

func TestListRooms_Shape(t *testing.T) {
	ts, _, _, alice, _ := newStack(t)
	id, _ := createRoom(t, ts, alice, 1, "Movie night")
	rooms := listRooms(t, ts.URL, alice)
	if len(rooms) != 1 {
		t.Fatalf("want 1 room, got %d", len(rooms))
	}
	r := rooms[0]
	if r["id"] != id || r["name"] != "Movie night" || r["mediaId"].(float64) != 1 ||
		r["mediaTitle"] != "The Movie" || r["kind"] != "video" || r["participants"].(float64) != 0 {
		t.Fatalf("room list shape wrong: %+v", r)
	}
}

func TestDeleteRoom_HostOnly(t *testing.T) {
	ts, hub, _, alice, bob := newStack(t)
	id, _ := createRoom(t, ts, alice, 1, "")

	// bob is a non-host member → 403 (alice=1 is admin AND owner, so use a
	// second member for the negative case: create as bob, delete as... bob owns
	// it. Instead: alice owns it, bob (member, non-owner) is forbidden.)
	if code, _ := doReq(t, "DELETE", ts.URL, "/api/rooms/"+id, bob); code != 403 {
		t.Fatalf("non-host delete want 403 got %d", code)
	}
	if _, ok := hub.getRoom(id); !ok {
		t.Fatal("room should survive a forbidden delete")
	}

	// host delete → 200, gone from list
	if code, _ := doReq(t, "DELETE", ts.URL, "/api/rooms/"+id, alice); code != 200 {
		t.Fatalf("host delete want 200 got %d", code)
	}
	if _, ok := hub.getRoom(id); ok {
		t.Fatal("room should be gone after host delete")
	}
	if len(listRooms(t, ts.URL, alice)) != 0 {
		t.Fatal("deleted room still listed")
	}

	// unknown id → 404
	if code, _ := doReq(t, "DELETE", ts.URL, "/api/rooms/nope", alice); code != 404 {
		t.Fatalf("unknown delete want 404 got %d", code)
	}
}

func TestRegenerateToken_ReplacesValue(t *testing.T) {
	ts, _, _, alice, bob := newStack(t)
	id, tok0 := createRoom(t, ts, alice, 1, "")

	// non-host → 403
	if code, _ := post(t, ts.URL, "/api/rooms/"+id+"/token", bob, `{}`); code != 403 {
		t.Fatalf("non-host regenerate want 403 got %d", code)
	}

	code, out := post(t, ts.URL, "/api/rooms/"+id+"/token", alice, `{}`)
	if code != 200 {
		t.Fatalf("host regenerate want 200 got %d", code)
	}
	tok1, _ := out["joinToken"].(string)
	if tok1 == "" || tok1 == tok0 {
		t.Fatalf("regenerate must replace the token: %q -> %q", tok0, tok1)
	}
}

// TestRoomsAreEphemeral: a fresh hub over the same DB starts with no rooms,
// while the durable media/account rows survive — the crash-recovery contract
// (AC-5.6): rooms/chat vanish, library and accounts intact.
func TestRoomsAreEphemeral(t *testing.T) {
	ts, _, d, alice, _ := newStack(t)
	createRoom(t, ts, alice, 1, "")

	// simulate a restart: a new hub over the same durable DB
	fresh := NewHub(d)
	fresh.mu.Lock()
	n := len(fresh.rooms)
	fresh.mu.Unlock()
	if n != 0 {
		t.Fatalf("a restarted hub must have no rooms, got %d", n)
	}
	var media, users int
	d.QueryRow(`SELECT count(*) FROM media`).Scan(&media)
	d.QueryRow(`SELECT count(*) FROM users`).Scan(&users)
	if media != 1 || users != 2 {
		t.Fatalf("durable data must survive: media=%d users=%d", media, users)
	}
}

// --- Task 3: guest sessions + public join surface ---

// postJoin POSTs /api/rooms/join and returns the status, decoded body, and
// the raw together_guest cookie value the response set (empty if none).
func postJoin(t *testing.T, ts, token, name, cookie string) (int, map[string]any, string) {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"token": token, "name": name})
	req, _ := http.NewRequest("POST", ts+"/api/rooms/join", strings.NewReader(string(b)))
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	guestCookie := ""
	for _, c := range res.Cookies() {
		if c.Name == "together_guest" {
			guestCookie = c.Value
		}
	}
	return res.StatusCode, out, guestCookie
}

func TestSanitizeGuestName(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"strips control chars", "Ali\x00ce\x07", "Alice", false},
		{"empty after strip is 400", "\x00\x01", "", true},
		{"empty string is 400", "", "", true},
		{"32 chars is ok", strings.Repeat("a", 32), strings.Repeat("a", 32), false},
		{"33 chars is 400", strings.Repeat("a", 33), "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := sanitizeGuestName(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("want error, got name %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestSuffixNameLocked_CollisionAndDeparture(t *testing.T) {
	h := NewHub(nil)
	h.guests["t1"] = &GuestSession{guestID: "g1", roomID: "room1", name: "Alice"}

	h.mu.Lock()
	got := h.suffixNameLocked("room1", "Alice")
	h.mu.Unlock()
	if got != "Alice (2)" {
		t.Fatalf("want suffix on collision, got %q", got)
	}

	h.guests["t2"] = &GuestSession{guestID: "g2", roomID: "room1", name: "Alice (2)"}
	h.mu.Lock()
	got = h.suffixNameLocked("room1", "Alice")
	h.mu.Unlock()
	if got != "Alice (3)" {
		t.Fatalf("want next free suffix, got %q", got)
	}

	delete(h.guests, "t1") // simulate departure
	h.mu.Lock()
	got = h.suffixNameLocked("room1", "Alice")
	h.mu.Unlock()
	if got != "Alice" {
		t.Fatalf("a departed guest's name must be free again, got %q", got)
	}

	h.guests["t3"] = &GuestSession{guestID: "g3", roomID: "room2", name: "Alice"}
	h.mu.Lock()
	got = h.suffixNameLocked("room1", "Alice")
	h.mu.Unlock()
	if got != "Alice" {
		t.Fatalf("collisions must be scoped to the room, got %q", got)
	}
}

func TestJoinRoom_ValidTokenSetsCookie(t *testing.T) {
	ts, _, _, alice, _ := newStack(t)
	_, tok := createRoom(t, ts, alice, 1, "")

	code, out, cookie := postJoin(t, ts.URL, tok, "Bob", "")
	if code != 200 {
		t.Fatalf("want 200 got %d (%+v)", code, out)
	}
	if out["roomId"] == nil || out["roomId"] == "" {
		t.Fatalf("want roomId in response: %+v", out)
	}
	if cookie == "" {
		t.Fatal("want together_guest cookie set")
	}
}

func TestJoinRoom_BadName(t *testing.T) {
	ts, _, _, alice, _ := newStack(t)
	_, tok := createRoom(t, ts, alice, 1, "")

	if code, out, _ := postJoin(t, ts.URL, tok, "", ""); code != 400 {
		t.Fatalf("empty name want 400 got %d (%+v)", code, out)
	}
	if code, out, _ := postJoin(t, ts.URL, tok, strings.Repeat("x", 33), ""); code != 400 {
		t.Fatalf("33-char name want 400 got %d (%+v)", code, out)
	}
}

func TestJoinRoom_NameCollisionSuffixes(t *testing.T) {
	ts, hub, _, alice, _ := newStack(t)
	_, tok := createRoom(t, ts, alice, 1, "")

	_, out1, c1 := postJoin(t, ts.URL, tok, "Sam", "")
	if out1["roomId"] == nil {
		t.Fatalf("first join failed: %+v", out1)
	}
	_, out2, c2 := postJoin(t, ts.URL, tok, "Sam", "")
	if out2["roomId"] == nil {
		t.Fatalf("second join failed: %+v", out2)
	}

	hub.mu.Lock()
	n1, n2 := hub.guests[c1].name, hub.guests[c2].name
	hub.mu.Unlock()
	if n1 != "Sam" || n2 != "Sam (2)" {
		t.Fatalf("want Sam / Sam (2), got %q / %q", n1, n2)
	}
}

func TestJoinRoom_LiveCookieReusesIdentity(t *testing.T) {
	ts, hub, _, alice, _ := newStack(t)
	_, tok := createRoom(t, ts, alice, 1, "")

	code1, out1, c1 := postJoin(t, ts.URL, tok, "Sam", "")
	if code1 != 200 {
		t.Fatalf("first join want 200 got %d", code1)
	}

	code2, out2, c2 := postJoin(t, ts.URL, tok, "Sam", "together_guest="+c1)
	if code2 != 200 {
		t.Fatalf("re-join want 200 got %d", code2)
	}
	if out1["roomId"] != out2["roomId"] {
		t.Fatalf("roomId mismatch: %v vs %v", out1["roomId"], out2["roomId"])
	}
	if c2 != "" {
		t.Fatal("re-join with a live guest cookie should not mint a new one")
	}

	hub.mu.Lock()
	n := len(hub.guests)
	name := hub.guests[c1].name
	hub.mu.Unlock()
	if n != 1 {
		t.Fatalf("re-join must not create a second session, got %d guests", n)
	}
	if name != "Sam" {
		t.Fatalf("identity name must stay unchanged (no re-suffix), got %q", name)
	}
}

func TestJoinRoom_RoomFull(t *testing.T) {
	ts, _, _, alice, _ := newStack(t)
	_, tok := createRoom(t, ts, alice, 1, "")

	for i := 0; i < participantCap; i++ {
		code, out, _ := postJoin(t, ts.URL, tok, fmt.Sprintf("Guest%d", i), "")
		if code != 200 {
			t.Fatalf("join %d want 200 got %d (%+v)", i, code, out)
		}
	}
	code, out, _ := postJoin(t, ts.URL, tok, "Overflow", "")
	if code != 409 {
		t.Fatalf("13th participant want 409 got %d (%+v)", code, out)
	}
}

func TestJoinRoom_DeadAndUnknownTokenByteIdentical(t *testing.T) {
	ts, _, _, alice, _ := newStack(t)
	id, tok := createRoom(t, ts, alice, 1, "")

	post(t, ts.URL, "/api/rooms/"+id+"/token", alice, `{}`) // kills tok

	deadBody, deadCode := joinRawBody(t, ts.URL, tok)
	neverBody, neverCode := joinRawBody(t, ts.URL, "never-existed-token")

	if deadCode != 404 || neverCode != 404 {
		t.Fatalf("both want 404: dead=%d never=%d", deadCode, neverCode)
	}
	if deadBody != neverBody {
		t.Fatalf("bodies must be byte-identical (no oracle): dead=%q never=%q", deadBody, neverBody)
	}
}

// joinRawBody POSTs /api/rooms/join and returns the raw response body text
// and status, for exact byte-for-byte comparisons.
func joinRawBody(t *testing.T, ts, token string) (string, int) {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"token": token, "name": "X"})
	req, _ := http.NewRequest("POST", ts+"/api/rooms/join", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return string(body), res.StatusCode
}

func TestPeekRoom(t *testing.T) {
	ts, _, _, alice, _ := newStack(t)
	_, tok := createRoom(t, ts, alice, 1, "Movie night")

	code, out := doReq(t, "GET", ts.URL, "/api/rooms/join/"+tok, "")
	if code != 200 || out["roomName"] != "Movie night" {
		t.Fatalf("want 200 {roomName: Movie night}, got %d %+v", code, out)
	}

	code, _ = doReq(t, "GET", ts.URL, "/api/rooms/join/nope", "")
	if code != 404 {
		t.Fatalf("unknown token want 404 got %d", code)
	}
}

func TestRegenerateToken_GuestCookieSurvives(t *testing.T) {
	ts, _, _, alice, _ := newStack(t)
	id, tok0 := createRoom(t, ts, alice, 1, "")

	_, out0, guestCookie := postJoin(t, ts.URL, tok0, "Sam", "")
	if out0["roomId"] == nil || guestCookie == "" {
		t.Fatalf("expected a successful first join with a guest cookie: %+v", out0)
	}

	_, out := post(t, ts.URL, "/api/rooms/"+id+"/token", alice, `{}`)
	tok1, _ := out["joinToken"].(string)
	if tok1 == "" {
		t.Fatalf("expected a new token: %+v", out)
	}

	if code, _, _ := postJoin(t, ts.URL, tok0, "Other", ""); code != 404 {
		t.Fatalf("old token join want 404 got %d", code)
	}
	if code, _, _ := postJoin(t, ts.URL, tok1, "Other", ""); code != 200 {
		t.Fatalf("new token join want 200 got %d", code)
	}

	// the already-joined guest's cookie is not tied to the token
	code, out2, _ := postJoin(t, ts.URL, tok1, "Sam", "together_guest="+guestCookie)
	if code != 200 || out2["roomId"] != id {
		t.Fatalf("surviving guest cookie should still work, got %d %+v", code, out2)
	}
}

func TestRoomMeta(t *testing.T) {
	ts, _, _, alice, _ := newStack(t)
	id, _ := createRoom(t, ts, alice, 1, "")

	code, out := doReq(t, "GET", ts.URL, "/api/rooms/"+id+"/meta", alice)
	if code != 200 {
		t.Fatalf("want 200 got %d (%+v)", code, out)
	}
	media, _ := out["media"].(map[string]any)
	if out["kind"] != "video" || media == nil || media["title"] != "The Movie" || media["sizeBytes"].(float64) != 10 {
		t.Fatalf("meta shape wrong: %+v", out)
	}
	if _, ok := out["subtitles"].([]any); !ok {
		t.Fatalf("subtitles should be an array: %+v", out)
	}

	code, _ = doReq(t, "GET", ts.URL, "/api/rooms/nope/meta", alice)
	if code != 404 {
		t.Fatalf("unknown room meta want 404 got %d", code)
	}
}

// --- Task 4: RequireRoom / RequireRoomMedia ---

// newRoomGateStack is a second, self-contained stack (distinct from
// newStack in hub_test.go) seeded with TWO ready media rows and TWO rooms so
// guest-vs-wrong-room/media scoping has something to fail against. It mounts
// /ws/{id} behind the real hub.RequireRoom and a stand-in media-byte route
// behind the real hub.RequireRoomMedia — a tiny inline handler instead of
// internal/media.ServeRoutes, because internal/live must not import
// internal/media (ARCHITECTURE §7 module direction); RequireRoomMedia's
// gating logic is exactly the same either way, and the real ServeFile/
// attachment-header behavior is covered on the account path in
// internal/media/serve_test.go.
func newRoomGateStack(t *testing.T) (ts *httptest.Server, hub *Hub, alice, bob string) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	auth.Seed(d, "alice", "password") // id 1, admin
	bh, bs := auth.Hash("password")
	d.Exec(`INSERT INTO users (username, pass_hash, salt) VALUES ('bob', ?, ?)`, bh, bs)                                   // id 2, member
	d.Exec(`INSERT INTO media (kind, title, status, file_path, size_bytes) VALUES ('video','Movie A','ready','a.mp4',10)`) // id 1
	d.Exec(`INSERT INTO media (kind, title, status, file_path, size_bytes) VALUES ('video','Movie B','ready','b.mp4',10)`) // id 2

	h := NewHub(d)
	mux := http.NewServeMux()
	h.Routes(mux)
	mux.HandleFunc("GET /ws/{id}", h.RequireRoom(h.Handle))
	mux.HandleFunc("GET /media/{id}/probe", h.RequireRoomMedia(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)

	tok1, _ := auth.CreateSession(d, 1)
	tok2, _ := auth.CreateSession(d, 2)
	return s, h, "session=" + tok1, "session=" + tok2
}

func TestRequireRoom_MetaEndpoint(t *testing.T) {
	ts, _, alice, bob := newRoomGateStack(t)
	roomA, _ := createRoom(t, ts, alice, 1, "")

	// no credential at all → 401
	if code, _ := doReq(t, "GET", ts.URL, "/api/rooms/"+roomA+"/meta", ""); code != 401 {
		t.Fatalf("no credential want 401 got %d", code)
	}

	// any account (not just the host) passes — bob is a non-host member
	if code, _ := doReq(t, "GET", ts.URL, "/api/rooms/"+roomA+"/meta", bob); code != 200 {
		t.Fatalf("non-host account want 200 got %d", code)
	}
}

func TestRequireRoom_GuestScopedToOwnRoom(t *testing.T) {
	ts, _, alice, _ := newRoomGateStack(t)
	roomA, tokA := createRoom(t, ts, alice, 1, "Room A")
	roomB, tokB := createRoom(t, ts, alice, 2, "Room B")

	_, _, guestA := postJoin(t, ts.URL, tokA, "Ali", "")
	_, _, guestB := postJoin(t, ts.URL, tokB, "Bea", "")

	// a guest's own room's meta → 200
	if code, _ := doReq(t, "GET", ts.URL, "/api/rooms/"+roomA+"/meta", "together_guest="+guestA); code != 200 {
		t.Fatalf("guest A on room A want 200 got %d", code)
	}
	// a guest reaching another room → 404 (no oracle vs unknown room)
	if code, _ := doReq(t, "GET", ts.URL, "/api/rooms/"+roomB+"/meta", "together_guest="+guestA); code != 404 {
		t.Fatalf("guest A on room B want 404 got %d", code)
	}
	if code, _ := doReq(t, "GET", ts.URL, "/api/rooms/"+roomA+"/meta", "together_guest="+guestB); code != 404 {
		t.Fatalf("guest B on room A want 404 got %d", code)
	}
	// garbage/never-issued guest cookie → 404, same shape as a real mismatch
	if code, _ := doReq(t, "GET", ts.URL, "/api/rooms/"+roomA+"/meta", "together_guest=nope"); code != 404 {
		t.Fatalf("unknown guest cookie want 404 got %d", code)
	}
}

func TestRequireRoomMedia_GuestScopedToOwnRoomMedia(t *testing.T) {
	ts, _, alice, _ := newRoomGateStack(t)
	_, tokA := createRoom(t, ts, alice, 1, "Room A") // room A's media is id 1
	createRoom(t, ts, alice, 2, "Room B")            // room B's media is id 2

	_, _, guestA := postJoin(t, ts.URL, tokA, "Ali", "")

	// guest downloads/probes its own room's media → 200
	if code, _ := doReq(t, "GET", ts.URL, "/media/1/probe", "together_guest="+guestA); code != 200 {
		t.Fatalf("guest A on media 1 want 200 got %d", code)
	}
	// guest reaching any other media id → 404, no oracle
	if code, _ := doReq(t, "GET", ts.URL, "/media/2/probe", "together_guest="+guestA); code != 404 {
		t.Fatalf("guest A on media 2 want 404 got %d", code)
	}
	// no credential at all → 401
	if code, _ := doReq(t, "GET", ts.URL, "/media/1/probe", ""); code != 401 {
		t.Fatalf("no credential want 401 got %d", code)
	}
	// an account session passes regardless of which media
	if code, _ := doReq(t, "GET", ts.URL, "/media/2/probe", alice); code != 200 {
		t.Fatalf("account on any media want 200 got %d", code)
	}
}
