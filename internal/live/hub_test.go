package live

import (
	"context"
	"database/sql"
	"encoding/json"
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
	mux.HandleFunc("GET /ws/{id}", auth.Require(d, false, hub.Handle))
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
	if f := waitFor(t, b, "chat"); f["body"] != "hi love" || f["username"] != "alice" {
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
