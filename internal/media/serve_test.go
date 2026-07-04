package media

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"together/internal/auth"
	"together/internal/db"
)

func TestStreamSupportsRangeAndAuth(t *testing.T) {
	dir := t.TempDir()
	d, _ := db.Open(filepath.Join(dir, "t.db"))
	defer d.Close()
	auth.Seed(d, "admin", "password")
	fp := filepath.Join(dir, "1.mp4")
	os.WriteFile(fp, []byte("0123456789"), 0o644)
	d.Exec(`INSERT INTO media (kind, title, status, file_path, size_bytes) VALUES ('movie','M','ready',?,10)`, fp)

	mux := http.NewServeMux()
	auth.Routes(mux, d)
	ServeRoutes(mux, d, dir)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// unauthenticated → 401
	if r, _ := http.Get(ts.URL + "/media/1/stream"); r.StatusCode != 401 {
		t.Fatalf("want 401 got %d", r.StatusCode)
	}

	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	c.Post(ts.URL+"/api/login", "application/json", strings.NewReader(`{"username":"admin","password":"password"}`))

	req, _ := http.NewRequest("GET", ts.URL+"/media/1/stream", nil)
	req.Header.Set("Range", "bytes=2-4")
	req.AddCookie(cookieFrom(t, jar, ts.URL))
	r, _ := http.DefaultClient.Do(req)
	if r.StatusCode != 206 {
		t.Fatalf("want 206 got %d", r.StatusCode)
	}

	r, _ = c.Get(ts.URL + "/api/media?kind=movie")
	var list []map[string]any
	json.NewDecoder(r.Body).Decode(&list)
	if len(list) != 1 || list[0]["title"] != "M" {
		t.Fatalf("%+v", list)
	}
}

func cookieFrom(t *testing.T, jar http.CookieJar, base string) *http.Cookie {
	u, _ := neturl.Parse(base)
	for _, c := range jar.Cookies(u) {
		if c.Name == "session" {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func TestSubtitleGatingOnMediaReady(t *testing.T) {
	dir := t.TempDir()
	d, _ := db.Open(filepath.Join(dir, "t.db"))
	defer d.Close()
	auth.Seed(d, "admin", "password")

	// Create a temp VTT file
	vttPath := filepath.Join(dir, "subs.vtt")
	os.WriteFile(vttPath, []byte("WEBVTT\n\n00:00:00.000 --> 00:00:01.000\nHello"), 0o644)

	// Insert processing media (not ready)
	d.Exec(`INSERT INTO media (id, kind, title, status, file_path, size_bytes) VALUES (1,'movie','Processing','processing','',0)`)
	// Insert subtitle for processing media
	d.Exec(`INSERT INTO subtitles (id, media_id, label, file_path) VALUES (1,1,'English',?)`, vttPath)

	// Insert ready media
	fp := filepath.Join(dir, "1.mp4")
	os.WriteFile(fp, []byte("0123456789"), 0o644)
	d.Exec(`INSERT INTO media (id, kind, title, status, file_path, size_bytes) VALUES (2,'movie','Ready','ready',?,10)`, fp)
	// Insert subtitle for ready media
	d.Exec(`INSERT INTO subtitles (id, media_id, label, file_path) VALUES (2,2,'English',?)`, vttPath)

	mux := http.NewServeMux()
	auth.Routes(mux, d)
	ServeRoutes(mux, d, dir)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	c.Post(ts.URL+"/api/login", "application/json", strings.NewReader(`{"username":"admin","password":"password"}`))

	// Processing media subs should 404
	r, _ := c.Get(ts.URL + "/media/1/subs/1")
	if r.StatusCode != 404 {
		t.Fatalf("processing media subs: want 404 got %d", r.StatusCode)
	}

	// Ready media subs should 200 with text/vtt
	r, _ = c.Get(ts.URL + "/media/2/subs/2")
	if r.StatusCode != 200 {
		t.Fatalf("ready media subs: want 200 got %d", r.StatusCode)
	}
	if ct := r.Header.Get("Content-Type"); ct != "text/vtt" {
		t.Fatalf("want Content-Type text/vtt got %q", ct)
	}
}
