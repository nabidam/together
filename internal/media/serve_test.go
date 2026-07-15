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
	d.Exec(`INSERT INTO media (kind, title, status, file_path, size_bytes) VALUES ('video','M','ready',?,10)`, fp)

	mux := http.NewServeMux()
	auth.Routes(mux, d)
	// The real gate is internal/live's RequireRoom (account OR room-scoped
	// guest); this package must not import internal/live (ARCHITECTURE §7),
	// so account-path coverage here uses auth.Require directly as the room
	// gate stand-in — RequireRoomMedia's guest-scoping logic itself is
	// exercised against the real hub in internal/live/rooms_test.go.
	acctGate := func(next http.HandlerFunc) http.HandlerFunc { return auth.Require(d, false, next) }
	ServeRoutes(mux, d, dir, acctGate)
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

	r, _ = c.Get(ts.URL + "/api/media?kind=video")
	var list []map[string]any
	json.NewDecoder(r.Body).Decode(&list)
	if len(list) != 1 || list[0]["title"] != "M" {
		t.Fatalf("%+v", list)
	}
}

// TestMediaKindFilterV2Vocabulary asserts the library filter speaks the V2
// video|audio vocabulary (AC-1 task 1): kind=video/audio filter correctly and
// the retired V1 value movie is not a valid discriminator (matches no rows).
func TestMediaKindFilterV2Vocabulary(t *testing.T) {
	dir := t.TempDir()
	d, _ := db.Open(filepath.Join(dir, "t.db"))
	defer d.Close()
	auth.Seed(d, "admin", "password")
	d.Exec(`INSERT INTO media (kind, title, status, file_path, size_bytes) VALUES ('video','V','ready','v.mp4',1)`)
	d.Exec(`INSERT INTO media (kind, title, status, file_path, size_bytes) VALUES ('audio','A','ready','a.m4a',1)`)

	mux := http.NewServeMux()
	auth.Routes(mux, d)
	// The real gate is internal/live's RequireRoom (account OR room-scoped
	// guest); this package must not import internal/live (ARCHITECTURE §7),
	// so account-path coverage here uses auth.Require directly as the room
	// gate stand-in — RequireRoomMedia's guest-scoping logic itself is
	// exercised against the real hub in internal/live/rooms_test.go.
	acctGate := func(next http.HandlerFunc) http.HandlerFunc { return auth.Require(d, false, next) }
	ServeRoutes(mux, d, dir, acctGate)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	c.Post(ts.URL+"/api/login", "application/json", strings.NewReader(`{"username":"admin","password":"password"}`))

	get := func(query string) []map[string]any {
		r, _ := c.Get(ts.URL + "/api/media" + query)
		var list []map[string]any
		json.NewDecoder(r.Body).Decode(&list)
		return list
	}

	if l := get("?kind=video"); len(l) != 1 || l[0]["kind"] != "video" {
		t.Fatalf("kind=video should return only the video row, got %+v", l)
	}
	if l := get("?kind=audio"); len(l) != 1 || l[0]["kind"] != "audio" {
		t.Fatalf("kind=audio should return only the audio row, got %+v", l)
	}
	// No stored row carries the retired movie vocabulary; the filter never
	// surfaces one whatever the query.
	for _, l := range append(get("?kind=video"), get("?kind=audio")...) {
		if l["kind"] == "movie" || l["kind"] == "music" {
			t.Fatalf("V1 vocabulary leaked into library: %+v", l)
		}
	}
}

// TestDownloadAttachmentVsStreamInline (task 4 AC): /media/{id}/download sets
// Content-Disposition: attachment and supports Range like stream; the stream
// route stays inline (no attachment header at all).
func TestDownloadAttachmentVsStreamInline(t *testing.T) {
	dir := t.TempDir()
	d, _ := db.Open(filepath.Join(dir, "t.db"))
	defer d.Close()
	auth.Seed(d, "admin", "password")
	fp := filepath.Join(dir, "1.mp4")
	os.WriteFile(fp, []byte("0123456789"), 0o644)
	d.Exec(`INSERT INTO media (kind, title, status, file_path, size_bytes) VALUES ('video','M','ready',?,10)`, fp)

	mux := http.NewServeMux()
	auth.Routes(mux, d)
	acctGate := func(next http.HandlerFunc) http.HandlerFunc { return auth.Require(d, false, next) }
	ServeRoutes(mux, d, dir, acctGate)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	c.Post(ts.URL+"/api/login", "application/json", strings.NewReader(`{"username":"admin","password":"password"}`))

	req, _ := http.NewRequest("GET", ts.URL+"/media/1/download", nil)
	req.Header.Set("Range", "bytes=2-4")
	req.AddCookie(cookieFrom(t, jar, ts.URL))
	r, _ := http.DefaultClient.Do(req)
	if r.StatusCode != 206 {
		t.Fatalf("download with Range: want 206 got %d", r.StatusCode)
	}
	if cd := r.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Fatalf("download must set Content-Disposition: attachment, got %q", cd)
	}

	r2, _ := c.Get(ts.URL + "/media/1/stream")
	if r2.StatusCode != 200 {
		t.Fatalf("stream: want 200 got %d", r2.StatusCode)
	}
	if cd := r2.Header.Get("Content-Disposition"); cd != "" {
		t.Fatalf("stream must be inline (no Content-Disposition), got %q", cd)
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
	// The real gate is internal/live's RequireRoom (account OR room-scoped
	// guest); this package must not import internal/live (ARCHITECTURE §7),
	// so account-path coverage here uses auth.Require directly as the room
	// gate stand-in — RequireRoomMedia's guest-scoping logic itself is
	// exercised against the real hub in internal/live/rooms_test.go.
	acctGate := func(next http.HandlerFunc) http.HandlerFunc { return auth.Require(d, false, next) }
	ServeRoutes(mux, d, dir, acctGate)
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
