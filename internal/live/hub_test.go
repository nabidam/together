package live

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"together/internal/auth"
	"together/internal/db"
)

type frame map[string]any

// newStack spins the full room stack (lifecycle routes + WS) over a temp DB
// seeded with two accounts (alice=1 admin, bob=2 member) and one ready video
// media row (id 1). Returns the server plus each account's session cookie.
func newStack(t *testing.T) (*httptest.Server, *Hub, *sql.DB, string, string) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	auth.Seed(d, "alice", "password") // id 1, admin
	bh, bs := auth.Hash("password")
	d.Exec(`INSERT INTO users (username, pass_hash, salt) VALUES ('bob', ?, ?)`, bh, bs) // id 2, member
	d.Exec(`INSERT INTO media (kind, title, status, file_path, size_bytes) VALUES ('video','The Movie','ready','x.mp4',10)`)

	hub := NewHub(d)
	mux := http.NewServeMux()
	hub.Routes(mux)
	// Mounted under the real room gate (not bare auth.Require) so a guest
	// cookie also passes: RequireRoom delegates to auth.Require for account
	// sessions, so account dials are unaffected.
	mux.HandleFunc("GET /ws/{id}", hub.RequireRoom(hub.Handle))
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	tok1, _ := auth.CreateSession(d, 1)
	tok2, _ := auth.CreateSession(d, 2)
	return ts, hub, d, "session=" + tok1, "session=" + tok2
}

// createRoom POSTs /api/rooms and returns the new room id and join token.
func createRoom(t *testing.T, ts *httptest.Server, cookie string, mediaID int64, name string) (string, string) {
	t.Helper()
	body := map[string]any{"mediaId": mediaID}
	if name != "" {
		body["name"] = name
	}
	req, _ := http.NewRequest("POST", ts.URL+"/api/rooms", strings.NewReader(string(marshal(body))))
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 201 {
		t.Fatalf("create room: want 201 got %d", res.StatusCode)
	}
	var out struct{ ID, JoinToken string }
	json.NewDecoder(res.Body).Decode(&out)
	return out.ID, out.JoinToken
}

func dial(t *testing.T, ts *httptest.Server, roomID, cookie string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/" + roomID
	c, _, err := websocket.Dial(context.Background(), url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Cookie": []string{cookie}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.CloseNow() })
	return c
}

func read(t *testing.T, c *websocket.Conn) frame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var f frame
	json.Unmarshal(data, &f)
	return f
}

func send(t *testing.T, c *websocket.Conn, v any) {
	t.Helper()
	data, _ := json.Marshal(v)
	if err := c.Write(context.Background(), websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
}

// joinAsGuest POSTs /api/rooms/join and returns a ready-to-use "Cookie"
// header value for dialing /ws/{id} as that guest.
func joinAsGuest(t *testing.T, ts *httptest.Server, token, name string) string {
	t.Helper()
	code, out, cookie := postJoin(t, ts.URL, token, name, "")
	if code != 200 || cookie == "" {
		t.Fatalf("guest join failed: code=%d out=%+v", code, out)
	}
	return "together_guest=" + cookie
}

func waitFor(t *testing.T, c *websocket.Conn, typ string) frame {
	t.Helper()
	for i := 0; i < 10; i++ {
		if f := read(t, c); f["type"] == typ {
			return f
		}
	}
	t.Fatalf("never received %q", typ)
	return nil
}

func TestChatAndPresenceBroadcast(t *testing.T) {
	ts, _, _, alice, bob := newStack(t)
	room, _ := createRoom(t, ts, alice, 1, "")
	a := dial(t, ts, room, alice)
	read(t, a) // hello
	b := dial(t, ts, room, bob)
	read(t, b) // hello
	waitFor(t, a, "presence")
	send(t, a, frame{"type": "chat", "body": "hi love"})
	if f := waitFor(t, b, "chat"); f["body"] != "hi love" || f["name"] != "alice" || f["isGuest"] != false {
		t.Fatalf("%+v", f)
	}
}

func TestActivitySync(t *testing.T) {
	ts, _, _, alice, bob := newStack(t)
	room, _ := createRoom(t, ts, alice, 1, "")
	a := dial(t, ts, room, alice)
	read(t, a)
	b := dial(t, ts, room, bob)
	read(t, b)
	// Room creation already started the activity; any participant can control it.
	send(t, b, frame{"type": "intent", "action": "play"})
	f := waitFor(t, a, "activity")
	st := f["activity"].(map[string]any)["state"].(map[string]any)
	if st["paused"] != false {
		t.Fatalf("play did not sync: %+v", st)
	}
}

func TestHelloCarriesActivityForLateJoiner(t *testing.T) {
	ts, _, _, alice, bob := newStack(t)
	room, _ := createRoom(t, ts, alice, 1, "")
	a := dial(t, ts, room, alice)
	read(t, a)
	b := dial(t, ts, room, bob)
	f := read(t, b) // hello
	if f["activity"] == nil {
		t.Fatal("late joiner must receive the running activity in hello")
	}
}

func TestWSRejectsUnknownRoom(t *testing.T) {
	ts, _, _, alice, _ := newStack(t)
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/deadbeefdeadbeef"
	_, _, err := websocket.Dial(context.Background(), url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Cookie": []string{alice}},
	})
	if err == nil {
		t.Fatal("dial to a non-existent room should fail the upgrade")
	}
}

// --- Task 5: WS protocol V2 ---

func TestStatusBroadcastsPresence(t *testing.T) {
	ts, _, _, alice, bob := newStack(t)
	room, _ := createRoom(t, ts, alice, 1, "")
	a := dial(t, ts, room, alice)
	read(t, a)                // hello
	waitFor(t, a, "presence") // a's own join is itself a broadcast recipient

	b := dial(t, ts, room, bob)
	read(t, b)                // hello
	waitFor(t, b, "presence") // b's own join, likewise self-delivered
	waitFor(t, a, "presence") // a observes b joining

	send(t, b, frame{"type": "status", "state": "file_ready"})

	for _, c := range []*websocket.Conn{a, b} {
		f := waitFor(t, c, "presence")
		users, _ := f["users"].([]any)
		found := false
		for _, u := range users {
			um := u.(map[string]any)
			if um["name"] == "bob" && um["status"] == "file_ready" {
				found = true
			}
		}
		if !found {
			t.Fatalf("status change not reflected in presence: %+v", f)
		}
	}
}

func TestChatRingDropsOldest(t *testing.T) {
	ts, _, _, alice, _ := newStack(t)
	room, _ := createRoom(t, ts, alice, 1, "")
	a := dial(t, ts, room, alice)
	read(t, a) // hello

	for i := 0; i < 210; i++ {
		send(t, a, frame{"type": "chat", "body": fmt.Sprintf("m%d", i)})
		waitFor(t, a, "chat") // drain own broadcast before sending the next
	}

	fresh := dial(t, ts, room, alice)
	h := read(t, fresh) // hello
	chatList, _ := h["chat"].([]any)
	if len(chatList) != 200 {
		t.Fatalf("want 200 messages in the ring, got %d", len(chatList))
	}
	first := chatList[0].(map[string]any)
	last := chatList[199].(map[string]any)
	if first["body"] != "m10" || last["body"] != "m209" {
		t.Fatalf("ring must drop oldest and keep order: first=%v last=%v", first["body"], last["body"])
	}
}

func TestHelloOnReconnectCarriesActivityChatAndHost(t *testing.T) {
	ts, _, _, alice, bob := newStack(t)
	room, tok := createRoom(t, ts, alice, 1, "")

	a := dial(t, ts, room, alice)
	ha := read(t, a) // hello
	you := ha["you"].(map[string]any)
	if you["isHost"] != true || you["isGuest"] != false || you["name"] != "alice" {
		t.Fatalf("room owner should be host: %+v", you)
	}

	b := dial(t, ts, room, bob)
	hb := read(t, b) // hello
	youB := hb["you"].(map[string]any)
	if youB["isHost"] != false {
		t.Fatalf("non-owner account should not be host: %+v", youB)
	}

	guestCookie := joinAsGuest(t, ts, tok, "Casey")
	g := dial(t, ts, room, guestCookie)
	hg := read(t, g) // hello
	youG := hg["you"].(map[string]any)
	if youG["isGuest"] != true || youG["isHost"] != false || youG["name"] != "Casey" {
		t.Fatalf("guest hello.you wrong: %+v", youG)
	}

	send(t, a, frame{"type": "chat", "body": "hello there"})
	waitFor(t, a, "chat")

	a.CloseNow()
	reconnect := dial(t, ts, room, alice)
	hr := read(t, reconnect) // hello
	if hr["activity"] == nil {
		t.Fatal("reconnect hello must carry the current activity")
	}
	chatList, _ := hr["chat"].([]any)
	found := false
	for _, c := range chatList {
		if c.(map[string]any)["body"] == "hello there" {
			found = true
		}
	}
	if !found {
		t.Fatalf("reconnect hello must carry chat history: %+v", chatList)
	}
}

func TestStatusNeverEntersWatchApply(t *testing.T) {
	ts, _, _, alice, _ := newStack(t)
	room, _ := createRoom(t, ts, alice, 1, "")
	a := dial(t, ts, room, alice)
	ha := read(t, a) // hello
	before, _ := json.Marshal(ha["activity"])

	send(t, a, frame{"type": "status", "state": "in_sync"})
	waitFor(t, a, "presence") // status only ever produces a presence broadcast

	b := dial(t, ts, room, alice)
	hb := read(t, b) // hello
	after, _ := json.Marshal(hb["activity"])

	if string(before) != string(after) {
		t.Fatalf("a status frame must never change activity: before=%s after=%s", before, after)
	}
}

func TestChatValidation_ErrorKeepsConnectionOpen(t *testing.T) {
	ts, _, _, alice, _ := newStack(t)
	room, _ := createRoom(t, ts, alice, 1, "")
	a := dial(t, ts, room, alice)
	read(t, a) // hello

	send(t, a, frame{"type": "chat", "body": ""})
	if f := waitFor(t, a, "error"); f["body"] == nil {
		t.Fatalf("empty chat should produce an error frame: %+v", f)
	}

	send(t, a, frame{"type": "chat", "body": strings.Repeat("x", 2001)})
	if f := waitFor(t, a, "error"); f["body"] == nil {
		t.Fatalf(">2000 char chat should produce an error frame: %+v", f)
	}

	send(t, a, frame{"type": "chat", "body": "still works"})
	if f := waitFor(t, a, "chat"); f["body"] != "still works" {
		t.Fatalf("connection must stay usable after errors: %+v", f)
	}
}

func TestGuestDialAppearsInPresenceAndChat(t *testing.T) {
	ts, _, _, alice, _ := newStack(t)
	room, tok := createRoom(t, ts, alice, 1, "")
	a := dial(t, ts, room, alice)
	read(t, a)                // hello
	waitFor(t, a, "presence") // a's own join is itself a broadcast recipient

	g := dial(t, ts, room, joinAsGuest(t, ts, tok, "Casey"))
	hg := read(t, g) // hello
	you := hg["you"].(map[string]any)
	if you["isGuest"] != true || you["name"] != "Casey" {
		t.Fatalf("guest hello.you wrong: %+v", you)
	}
	waitFor(t, g, "presence") // guest's own join, likewise self-delivered

	pf := waitFor(t, a, "presence")
	users, _ := pf["users"].([]any)
	found := false
	for _, u := range users {
		um := u.(map[string]any)
		if um["name"] == "Casey" && um["isGuest"] == true {
			found = true
		}
	}
	if !found {
		t.Fatalf("guest missing from presence: %+v", pf)
	}

	send(t, g, frame{"type": "chat", "body": "hi from guest"})
	if f := waitFor(t, a, "chat"); f["name"] != "Casey" || f["isGuest"] != true || f["body"] != "hi from guest" {
		t.Fatalf("guest chat frame wrong: %+v", f)
	}
}

// TestChatWritesNoDB is the compile-level-plus-runtime half of AC "zero DB
// writes on chat": no `messages` table exists at all, and sending chat
// leaves the schema and durable row counts untouched.
func TestChatWritesNoDB(t *testing.T) {
	ts, _, d, alice, _ := newStack(t)
	room, _ := createRoom(t, ts, alice, 1, "")
	a := dial(t, ts, room, alice)
	read(t, a) // hello

	var messagesTables int
	d.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='messages'`).Scan(&messagesTables)
	if messagesTables != 0 {
		t.Fatal("a messages table must not exist in V2")
	}

	var usersBefore, mediaBefore int
	d.QueryRow(`SELECT count(*) FROM users`).Scan(&usersBefore)
	d.QueryRow(`SELECT count(*) FROM media`).Scan(&mediaBefore)

	send(t, a, frame{"type": "chat", "body": "no db here"})
	waitFor(t, a, "chat")

	var usersAfter, mediaAfter int
	d.QueryRow(`SELECT count(*) FROM users`).Scan(&usersAfter)
	d.QueryRow(`SELECT count(*) FROM media`).Scan(&mediaAfter)
	if usersBefore != usersAfter || mediaBefore != mediaAfter {
		t.Fatalf("chat must not write durable rows: users %d->%d media %d->%d", usersBefore, usersAfter, mediaBefore, mediaAfter)
	}
}
