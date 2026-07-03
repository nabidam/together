package api

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"together/internal/auth"
	"together/internal/db"
)

func client(t *testing.T) (*http.Client, *httptest.Server) {
	d, _ := db.Open(filepath.Join(t.TempDir(), "t.db"))
	t.Cleanup(func() { d.Close() })
	auth.Seed(d, "admin", "password")
	mux := http.NewServeMux()
	auth.Routes(mux, d)
	Routes(mux, d)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	c.Post(ts.URL+"/api/login", "application/json",
		strings.NewReader(`{"username":"admin","password":"password"}`))
	return c, ts
}

func TestRoomLifecycle(t *testing.T) {
	c, ts := client(t)
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
