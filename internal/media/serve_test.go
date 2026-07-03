package media

import (
	"encoding/json"
	neturl "net/url"
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
	ServeRoutes(mux, d)
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
