package live

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
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
// Uses a 30m TOGETHER_ROOM_IDLE — irrelevant to everything except task 6's
// timer tests, which use newStackIdle instead to get a test-fast duration.
func newStack(t *testing.T) (*httptest.Server, *Hub, *sql.DB, string, string) {
	t.Helper()
	return newStackIdle(t, 30*time.Minute)
}

// newStackIdle is newStack with an injectable TOGETHER_ROOM_IDLE (AC-5.3):
// task 6's empty-timer tests need a millisecond-scale idle window instead of
// the real 30m default so the suite stays fast.
func newStackIdle(t *testing.T, idle time.Duration) (*httptest.Server, *Hub, *sql.DB, string, string) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	auth.Seed(d, "alice", "correct horse battery staple") // id 1, admin
	bh, bs := auth.Hash("correct horse battery staple")
	d.Exec(`INSERT INTO users (username, pass_hash, salt) VALUES ('bob', ?, ?)`, bh, bs) // id 2, member
	d.Exec(`INSERT INTO media (kind, title, status, file_path, size_bytes) VALUES ('video','The Movie','ready','x.mp4',10)`)

	hub := NewHub(d, idle)
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

func TestWSStart_RejectsMediaOutsideRoomScope(t *testing.T) {
	ts, hub, d, alice, _ := newStack(t)
	d.Exec(`INSERT INTO media (kind, title, status, file_path, size_bytes) VALUES ('video','Other Movie','ready','other.mp4',10)`)
	room, token := createRoom(t, ts, alice, 1, "")
	guest := joinAsGuest(t, ts, token, "Casey")
	c := dial(t, ts, room, guest)
	read(t, c) // hello

	r, ok := hub.getRoom(room)
	if !ok {
		t.Fatal("room disappeared")
	}
	r.mu.Lock()
	before := *r.watch
	r.mu.Unlock()

	send(t, c, frame{"type": "start", "mediaId": 2})
	if f := waitFor(t, c, "error"); f["body"] != "media does not match room" {
		t.Fatalf("wrong fixed-media error: %+v", f)
	}

	r.mu.Lock()
	after := *r.watch
	r.mu.Unlock()
	if after.MediaID != before.MediaID || after.Version != before.Version || after.Position != before.Position {
		t.Fatalf("mismatched start changed activity: before=%+v after=%+v", before, after)
	}
}

func TestWSStart_RoomMediaAndControlsRemainAvailable(t *testing.T) {
	ts, _, _, alice, bob := newStack(t)
	room, _ := createRoom(t, ts, alice, 1, "")
	a := dial(t, ts, room, alice)
	read(t, a) // hello
	b := dial(t, ts, room, bob)
	read(t, b) // hello
	waitFor(t, a, "presence")

	send(t, b, frame{"type": "start", "mediaId": 1})
	if f := waitFor(t, a, "activity"); f["activity"].(map[string]any)["state"].(map[string]any)["mediaId"] != float64(1) {
		t.Fatalf("room-media start did not broadcast its activity: %+v", f)
	}

	send(t, b, frame{"type": "intent", "action": "play"})
	if f := waitFor(t, a, "activity"); f["activity"].(map[string]any)["state"].(map[string]any)["paused"] != false {
		t.Fatalf("play changed after fixed-media guard: %+v", f)
	}
	send(t, b, frame{"type": "intent", "action": "seek", "position": 42})
	if f := waitFor(t, a, "activity"); f["activity"].(map[string]any)["state"].(map[string]any)["position"] != float64(42) {
		t.Fatalf("seek changed after fixed-media guard: %+v", f)
	}
	send(t, b, frame{"type": "intent", "action": "pause"})
	if f := waitFor(t, a, "activity"); f["activity"].(map[string]any)["state"].(map[string]any)["paused"] != true {
		t.Fatalf("pause changed after fixed-media guard: %+v", f)
	}
	send(t, b, frame{"type": "end"})
	if f := waitFor(t, a, "activity"); f["activity"] != nil {
		t.Fatalf("end changed after fixed-media guard: %+v", f)
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

func TestWSCapacity_RejectsThirteenthBeforePresence(t *testing.T) {
	ts, hub, _, alice, _ := newStack(t)
	room, token := createRoom(t, ts, alice, 1, "")

	// A guest identity exists before its socket opens, but it must not consume
	// a live slot until it actually connects.
	guest := joinAsGuest(t, ts, token, "Casey")
	connections := make([]*websocket.Conn, 0, participantCap)
	for i := 0; i < participantCap-1; i++ {
		connections = append(connections, dial(t, ts, room, alice))
		read(t, connections[len(connections)-1]) // hello
	}
	connections = append(connections, dial(t, ts, room, guest))
	guestHello := read(t, connections[len(connections)-1])
	if guestHello["you"].(map[string]any)["isGuest"] != true {
		t.Fatalf("guest connection must enter presence as a guest: %+v", guestHello)
	}

	rm, ok := hub.getRoom(room)
	if !ok {
		t.Fatal("room disappeared")
	}
	rm.mu.Lock()
	if got := len(rm.clients); got != participantCap {
		rm.mu.Unlock()
		t.Fatalf("want %d live clients, got %d", participantCap, got)
	}
	rm.mu.Unlock()

	// The HTTP upgrade succeeds, then the server immediately sends the close
	// control frame. No hello or presence frame may precede it.
	overflow := dial(t, ts, room, alice)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _, err := overflow.Read(ctx)
	if got := websocket.CloseStatus(err); got != websocket.StatusPolicyViolation {
		t.Fatalf("13th connection close status = %v, want policy violation; err=%v", got, err)
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()
	if got := len(rm.clients); got != participantCap {
		t.Fatalf("overflow connection entered Room.clients: got %d", got)
	}
}

func TestWSCapacity_RacingForLastSlotAdmitsOne(t *testing.T) {
	ts, hub, _, alice, _ := newStack(t)
	room, _ := createRoom(t, ts, alice, 1, "")
	for i := 0; i < participantCap-1; i++ {
		c := dial(t, ts, room, alice)
		read(t, c) // hello
	}

	type result struct {
		conn *websocket.Conn
		err  error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/" + room
	for range 2 {
		go func() {
			<-start
			conn, _, err := websocket.Dial(context.Background(), url, &websocket.DialOptions{
				HTTPHeader: http.Header{"Cookie": []string{alice}},
			})
			results <- result{conn: conn, err: err}
		}()
	}
	close(start)

	admitted := 0
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatalf("racing handshake failed before admission: %v", result.err)
		}
		t.Cleanup(func() { result.conn.CloseNow() })
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, data, err := result.conn.Read(ctx)
		cancel()
		if err == nil {
			var f frame
			if json.Unmarshal(data, &f) != nil || f["type"] != "hello" {
				t.Fatalf("admitted connection must receive hello, got %s", data)
			}
			admitted++
			continue
		}
		if got := websocket.CloseStatus(err); got != websocket.StatusPolicyViolation {
			t.Fatalf("rejected racing connection close status = %v, want policy violation; err=%v", got, err)
		}
	}
	if admitted != 1 {
		t.Fatalf("racing final-slot handshakes admitted %d connections, want 1", admitted)
	}

	rm, ok := hub.getRoom(room)
	if !ok {
		t.Fatal("room disappeared")
	}
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if got := len(rm.clients); got != participantCap {
		t.Fatalf("want %d live clients after race, got %d", participantCap, got)
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

func TestTwoHostTabsCoexist(t *testing.T) {
	ts, _, _, alice, _ := newStack(t)
	room, _ := createRoom(t, ts, alice, 1, "")

	first := dial(t, ts, room, alice)
	firstHello := read(t, first)
	if firstHello["you"].(map[string]any)["isHost"] != true {
		t.Fatalf("first owner tab must retain host powers: %+v", firstHello)
	}

	second := dial(t, ts, room, alice)
	secondHello := read(t, second)
	if secondHello["you"].(map[string]any)["isHost"] != true {
		t.Fatalf("second owner tab must also receive host powers: %+v", secondHello)
	}
	waitFor(t, first, "presence")

	// Two tabs are independent clients: either may act, and the other must
	// receive the authoritative update.
	send(t, second, frame{"type": "intent", "action": "play"})
	if activity := waitFor(t, first, "activity"); activity["activity"].(map[string]any)["state"].(map[string]any)["paused"] != false {
		t.Fatalf("host-tab intent did not reach the other tab: %+v", activity)
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

// --- Task 6: teardown path, empty timer, per-room recover ---

// TestDeleteRoom_RoomClosedThenSocketsClose covers the host-DELETE trigger
// of the ARCHITECTURE §5 teardown path: both connected clients must receive
// the room_closed frame BEFORE their sockets close (task 6's critical
// subtlety — a hard ctx cancel would drop the buffered frame instead), the
// room and its guest sessions must be gone from the hub, and the dead join
// token must 404.
func TestDeleteRoom_RoomClosedThenSocketsClose(t *testing.T) {
	ts, hub, _, alice, bob := newStack(t)
	room, tok := createRoom(t, ts, alice, 1, "")

	a := dial(t, ts, room, alice)
	read(t, a) // hello
	b := dial(t, ts, room, bob)
	read(t, b)                // hello
	waitFor(t, a, "presence") // a observes b joining

	// A guest with a live cookie but no open WS connection — teardown must
	// still drop its session from Hub.guests even though it never had a
	// *client* to close.
	joinAsGuest(t, ts, tok, "Casey")
	hub.mu.Lock()
	guestsBefore := len(hub.guests)
	hub.mu.Unlock()
	if guestsBefore != 1 {
		t.Fatalf("want 1 guest session before teardown, got %d", guestsBefore)
	}

	if code, _ := doReq(t, "DELETE", ts.URL, "/api/rooms/"+room, alice); code != 200 {
		t.Fatalf("host delete want 200 got %d", code)
	}

	// Both survivors must receive room_closed...
	for name, c := range map[string]*websocket.Conn{"a": a, "b": b} {
		if f := waitFor(t, c, "room_closed"); f == nil {
			t.Fatalf("%s never received room_closed", name)
		}
	}
	// ...and only THEN see their socket close (next read errors out).
	for name, c := range map[string]*websocket.Conn{"a": a, "b": b} {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, _, err := c.Read(ctx)
		cancel()
		if err == nil {
			t.Fatalf("%s socket should be closed after room_closed", name)
		}
	}

	if _, ok := hub.getRoom(room); ok {
		t.Fatal("room should be gone from the hub after teardown")
	}
	hub.mu.Lock()
	guestsAfter := len(hub.guests)
	hub.mu.Unlock()
	if guestsAfter != 0 {
		t.Fatalf("guest sessions must be dropped at teardown, got %d", guestsAfter)
	}

	if code, _, _ := postJoin(t, ts.URL, tok, "Late", ""); code != 404 {
		t.Fatalf("join on the old token after teardown want 404 got %d", code)
	}
}

// TestEmptyRoomTimer_FiresAfterIdle: with a ~50ms TOGETHER_ROOM_IDLE, the
// last live WS connection closing starts the timer, and a guest session
// that was minted over HTTP but never dialed WS does not count as a live
// connection — it must not keep the room warm past the fire.
func TestEmptyRoomTimer_FiresAfterIdle(t *testing.T) {
	ts, hub, _, alice, _ := newStackIdle(t, 50*time.Millisecond)
	room, tok := createRoom(t, ts, alice, 1, "")

	a := dial(t, ts, room, alice)
	read(t, a) // hello

	joinAsGuest(t, ts, tok, "Ghost") // cookie'd, never dials WS

	a.CloseNow() // the only live connection drops -> emptyTimer.Reset(idle)

	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, ok := hub.getRoom(room); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("empty room was not torn down after TOGETHER_ROOM_IDLE fired")
		}
		time.Sleep(5 * time.Millisecond)
	}

	hub.mu.Lock()
	n := len(hub.guests)
	hub.mu.Unlock()
	if n != 0 {
		t.Fatalf("the ghost guest session should be dropped by teardown too, got %d", n)
	}
}

// TestEmptyRoomTimer_RejoinWithinIdleStopsIt verifies the Stop()-on-join
// half of AC-5.3: a rejoin inside the idle window must cancel the pending
// fire, so the room survives past what would have been the original
// deadline had the timer not been stopped.
func TestEmptyRoomTimer_RejoinWithinIdleStopsIt(t *testing.T) {
	idle := 150 * time.Millisecond
	ts, hub, _, alice, _ := newStackIdle(t, idle)
	room, _ := createRoom(t, ts, alice, 1, "")

	a := dial(t, ts, room, alice)
	read(t, a)   // hello
	a.CloseNow() // starts the idle timer, deadline ~= now + 150ms

	time.Sleep(50 * time.Millisecond) // well inside the window

	b := dial(t, ts, room, alice) // rejoin -> Stop()
	read(t, b)                    // hello

	// Past the ORIGINAL deadline (50ms + 200ms > 150ms) with a healthy
	// margin: if Stop() hadn't run, the room would already be gone by now.
	time.Sleep(200 * time.Millisecond)

	if _, ok := hub.getRoom(room); !ok {
		t.Fatal("a rejoin inside the idle window must keep the room alive past the original deadline")
	}
}

// TestRoomPanic_TearsDownOnlyThatRoom exercises the per-room recover
// (NFR-7): a panic injected into room A's dispatch must be caught, logged
// with room A's id, and torn down room A alone — room B's two independent
// clients keep exchanging frames and the process stays up. Run with -race:
// this is the concurrency-sensitive path in the whole task.
func TestRoomPanic_TearsDownOnlyThatRoom(t *testing.T) {
	ts, hub, _, alice, bob := newStack(t)
	roomA, _ := createRoom(t, ts, alice, 1, "Room A")
	roomB, _ := createRoom(t, ts, alice, 1, "Room B")

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	a := dial(t, ts, roomA, alice)
	read(t, a) // hello
	rmA, ok := hub.getRoom(roomA)
	if !ok {
		t.Fatal("room A should exist before injecting a panic")
	}
	rmA.setPanicTrigger(func() { panic("injected for TestRoomPanic_TearsDownOnlyThatRoom") })

	b1 := dial(t, ts, roomB, alice)
	read(t, b1) // hello
	b2 := dial(t, ts, roomB, bob)
	read(t, b2)                // hello
	waitFor(t, b1, "presence") // b1 observes b2 joining

	// Any inbound frame on room A's connection now panics inside dispatch.
	send(t, a, frame{"type": "chat", "body": "trigger"})

	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, ok := hub.getRoom(roomA); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("room A was not torn down after the injected panic")
		}
		time.Sleep(5 * time.Millisecond)
	}

	if !strings.Contains(logBuf.String(), "room panic id="+roomA) {
		t.Fatalf("panic log missing room A's id: %s", logBuf.String())
	}

	// Room B must be entirely unaffected: still registered, still live.
	if _, ok := hub.getRoom(roomB); !ok {
		t.Fatal("room B should be unaffected by room A's panic")
	}
	send(t, b1, frame{"type": "chat", "body": "still alive"})
	if f := waitFor(t, b2, "chat"); f["body"] != "still alive" {
		t.Fatalf("room B should still exchange frames after room A's panic: %+v", f)
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
