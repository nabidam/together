package live

import (
	"context"
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

func dial(t *testing.T, ts *httptest.Server, cookie string) *websocket.Conn {
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/1"
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
	data, _ := json.Marshal(v)
	if err := c.Write(context.Background(), websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, c *websocket.Conn, typ string) frame {
	for i := 0; i < 10; i++ {
		if f := read(t, c); f["type"] == typ {
			return f
		}
	}
	t.Fatalf("never received %q", typ)
	return nil
}

func setup(t *testing.T) (*httptest.Server, string, string) {
	d, _ := db.Open(filepath.Join(t.TempDir(), "t.db"))
	t.Cleanup(func() { d.Close() })
	auth.Seed(d, "alice", "password")
	bh, bs := auth.Hash("password")
	d.Exec(`INSERT INTO users (username, pass_hash, salt) VALUES ('bob', ?, ?)`, bh, bs)
	d.Exec(`INSERT INTO rooms (name, owner_id) VALUES ('r', 1)`)
	d.Exec(`INSERT INTO media (kind, title, status, file_path) VALUES ('movie','m','ready','x.mp4')`)
	hub := NewHub(d)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/{id}", auth.Require(d, false, hub.Handle))
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	tok1, _ := auth.CreateSession(d, 1)
	tok2, _ := auth.CreateSession(d, 2)
	return ts, "session=" + tok1, "session=" + tok2
}

func TestChatAndPresenceBroadcast(t *testing.T) {
	ts, alice, bob := setup(t)
	a := dial(t, ts, alice)
	read(t, a) // hello
	b := dial(t, ts, bob)
	read(t, b) // hello
	waitFor(t, a, "presence")
	send(t, a, frame{"type": "chat", "body": "hi love"})
	if f := waitFor(t, b, "chat"); f["body"] != "hi love" || f["username"] != "alice" {
		t.Fatalf("%+v", f)
	}
}

func TestActivitySync(t *testing.T) {
	ts, alice, bob := setup(t)
	a := dial(t, ts, alice)
	read(t, a)
	b := dial(t, ts, bob)
	read(t, b)
	send(t, a, frame{"type": "start", "mediaId": 1})
	waitFor(t, a, "activity")
	waitFor(t, b, "activity")
	send(t, b, frame{"type": "intent", "action": "play"}) // anyone can control
	f := waitFor(t, a, "activity")
	st := f["activity"].(map[string]any)["state"].(map[string]any)
	if st["paused"] != false {
		t.Fatalf("play did not sync: %+v", st)
	}
}

func TestHelloCarriesActivityForLateJoiner(t *testing.T) {
	ts, alice, bob := setup(t)
	a := dial(t, ts, alice)
	read(t, a)
	send(t, a, frame{"type": "start", "mediaId": 1})
	waitFor(t, a, "activity")
	b := dial(t, ts, bob)
	f := read(t, b) // hello
	if f["activity"] == nil {
		t.Fatal("late joiner must receive running activity in hello")
	}
}
