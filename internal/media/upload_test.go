package media

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"together/internal/auth"
	"together/internal/db"
)

func adminClient(t *testing.T) (*http.Client, *httptest.Server, string) {
	dir := t.TempDir()
	d, _ := db.Open(filepath.Join(dir, "t.db"))
	t.Cleanup(func() { d.Close() })
	auth.Seed(d, "admin", "password")
	mux := http.NewServeMux()
	auth.Routes(mux, d)
	UploadRoutes(mux, d, dir)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	c.Post(ts.URL+"/api/login", "application/json", strings.NewReader(`{"username":"admin","password":"password"}`))
	return c, ts, dir
}

func patch(t *testing.T, c *http.Client, url string, body []byte) *http.Response {
	req, _ := http.NewRequest("PATCH", url, bytes.NewReader(body))
	r, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestChunkedUploadWithResume(t *testing.T) {
	c, ts, dir := adminClient(t)
	r, _ := c.Post(ts.URL+"/api/admin/media", "application/json",
		strings.NewReader(`{"kind":"movie","title":"Test","origName":"t.mp4"}`))
	var m struct{ ID int64 }
	json.NewDecoder(r.Body).Decode(&m)

	patch(t, c, fmt.Sprintf("%s/api/admin/media/%d/blob?offset=0", ts.URL, m.ID), []byte("hello "))
	// simulate reconnect: ask for size
	r, _ = c.Get(fmt.Sprintf("%s/api/admin/media/%d/blob", ts.URL, m.ID))
	var sz struct{ Size int64 }
	json.NewDecoder(r.Body).Decode(&sz)
	if sz.Size != 6 {
		t.Fatalf("resume size want 6 got %d", sz.Size)
	}
	patch(t, c, fmt.Sprintf("%s/api/admin/media/%d/blob?offset=%d", ts.URL, m.ID, sz.Size), []byte("world"))

	r, _ = c.Post(fmt.Sprintf("%s/api/admin/media/%d/finish", ts.URL, m.ID), "", nil)
	if r.StatusCode != 202 {
		t.Fatalf("finish: %d", r.StatusCode)
	}
	got, _ := os.ReadFile(UploadPath(dir, m.ID))
	if string(got) != "hello world" {
		t.Fatalf("file content %q", got)
	}
	// job enqueued
	d, _ := db.Open(filepath.Join(dir, "t.db"))
	defer d.Close()
	var n int
	d.QueryRow(`SELECT count(*) FROM jobs WHERE media_id=? AND status='pending'`, m.ID).Scan(&n)
	if n != 1 {
		t.Fatalf("want 1 pending job got %d", n)
	}
}
