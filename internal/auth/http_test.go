package auth

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"together/internal/db"
)

func mustJar(t *testing.T) http.CookieJar {
	j, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func testServer(t *testing.T) *httptest.Server {
	d, _ := db.Open(filepath.Join(t.TempDir(), "t.db"))
	t.Cleanup(func() { d.Close() })
	Seed(d, "admin", "pw")
	mux := http.NewServeMux()
	Routes(mux, d)
	mux.HandleFunc("GET /api/secret", Require(d, false, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(From(r).Username))
	}))
	return httptest.NewServer(mux)
}

func TestLoginFlow(t *testing.T) {
	ts := testServer(t)
	defer ts.Close()
	jar := &http.Client{Jar: mustJar(t)}

	if r, _ := jar.Get(ts.URL + "/api/secret"); r.StatusCode != 401 {
		t.Fatalf("want 401 got %d", r.StatusCode)
	}
	r, err := jar.Post(ts.URL+"/api/login", "application/json",
		strings.NewReader(`{"username":"admin","password":"pw"}`))
	if err != nil || r.StatusCode != 200 {
		t.Fatalf("login: %v %d", err, r.StatusCode)
	}
	r, _ = jar.Get(ts.URL + "/api/secret")
	if r.StatusCode != 200 {
		t.Fatalf("want 200 got %d", r.StatusCode)
	}
}
