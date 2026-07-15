package live

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
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
