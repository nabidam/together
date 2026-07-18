package auth

import (
	"encoding/json"
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
	Seed(d, "admin", "correct horse battery staple")
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
		strings.NewReader(`{"username":"admin","password":"correct horse battery staple"}`))
	if err != nil || r.StatusCode != 200 {
		t.Fatalf("login: %v %d", err, r.StatusCode)
	}
	r, _ = jar.Get(ts.URL + "/api/secret")
	if r.StatusCode != 200 {
		t.Fatalf("want 200 got %d", r.StatusCode)
	}
}

// adminClient logs in as the seeded admin and returns a client carrying its session cookie.
func adminClient(t *testing.T, ts *httptest.Server) *http.Client {
	t.Helper()
	c := &http.Client{Jar: mustJar(t)}
	r, err := c.Post(ts.URL+"/api/login", "application/json",
		strings.NewReader(`{"username":"admin","password":"correct horse battery staple"}`))
	if err != nil || r.StatusCode != 200 {
		t.Fatalf("admin login: %v %d", err, r.StatusCode)
	}
	return c
}

// newInvite creates an invite code as admin and returns it.
func newInvite(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	c := adminClient(t, ts)
	r, err := c.Post(ts.URL+"/api/admin/invites", "application/json", nil)
	if err != nil || r.StatusCode != 200 {
		t.Fatalf("create invite: %v %d", err, r.StatusCode)
	}
	defer r.Body.Close()
	var out struct{ Code string }
	if err := json.NewDecoder(r.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Code == "" {
		t.Fatal("empty invite code")
	}
	return out.Code
}

func TestRegisterFlow(t *testing.T) {
	ts := testServer(t)
	defer ts.Close()
	code := newInvite(t, ts)

	jar := &http.Client{Jar: mustJar(t)}
	r, err := jar.Post(ts.URL+"/api/register", "application/json",
		strings.NewReader(`{"code":"`+code+`","username":"newbie","password":"longpassword"}`))
	if err != nil || r.StatusCode != 200 {
		t.Fatalf("register: %v %d", err, r.StatusCode)
	}

	r, err = jar.Get(ts.URL + "/api/me")
	if err != nil || r.StatusCode != 200 {
		t.Fatalf("/api/me after register: %v %d", err, r.StatusCode)
	}
	var u User
	if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
		t.Fatal(err)
	}
	if u.Username != "newbie" || u.Role != "member" {
		t.Fatalf("got %+v", u)
	}
}

func TestRegisterBadOrUsedCode(t *testing.T) {
	ts := testServer(t)
	defer ts.Close()

	jar := &http.Client{Jar: mustJar(t)}
	r, err := jar.Post(ts.URL+"/api/register", "application/json",
		strings.NewReader(`{"code":"bogus","username":"someone","password":"longpassword"}`))
	if err != nil || r.StatusCode != 400 {
		t.Fatalf("bogus code: want 400 got %v %d", err, r.StatusCode)
	}

	code := newInvite(t, ts)
	jar2 := &http.Client{Jar: mustJar(t)}
	r, err = jar2.Post(ts.URL+"/api/register", "application/json",
		strings.NewReader(`{"code":"`+code+`","username":"first","password":"longpassword"}`))
	if err != nil || r.StatusCode != 200 {
		t.Fatalf("first use: %v %d", err, r.StatusCode)
	}
	jar3 := &http.Client{Jar: mustJar(t)}
	r, err = jar3.Post(ts.URL+"/api/register", "application/json",
		strings.NewReader(`{"code":"`+code+`","username":"second","password":"longpassword"}`))
	if err != nil || r.StatusCode != 400 {
		t.Fatalf("reused code: want 400 got %v %d", err, r.StatusCode)
	}
}

// TestFailedRegisterDoesNotBurnCode is the regression test for the bug where a
// failed user insert (e.g. duplicate username) left the invite code permanently
// burned even though no account was created.
func TestFailedRegisterDoesNotBurnCode(t *testing.T) {
	ts := testServer(t)
	defer ts.Close()
	code := newInvite(t, ts)

	jar := &http.Client{Jar: mustJar(t)}
	r, err := jar.Post(ts.URL+"/api/register", "application/json",
		strings.NewReader(`{"code":"`+code+`","username":"admin","password":"longpassword"}`))
	if err != nil || r.StatusCode != 400 {
		t.Fatalf("taken username: want 400 got %v %d", err, r.StatusCode)
	}

	jar2 := &http.Client{Jar: mustJar(t)}
	r, err = jar2.Post(ts.URL+"/api/register", "application/json",
		strings.NewReader(`{"code":"`+code+`","username":"freshuser","password":"longpassword"}`))
	if err != nil || r.StatusCode != 200 {
		t.Fatalf("retry with same code: want 200 got %v %d", err, r.StatusCode)
	}
}

func TestCreateInviteRequiresAdmin(t *testing.T) {
	ts := testServer(t)
	defer ts.Close()
	code := newInvite(t, ts)

	jar := &http.Client{Jar: mustJar(t)}
	r, err := jar.Post(ts.URL+"/api/register", "application/json",
		strings.NewReader(`{"code":"`+code+`","username":"member1","password":"longpassword"}`))
	if err != nil || r.StatusCode != 200 {
		t.Fatalf("register member: %v %d", err, r.StatusCode)
	}

	r, err = jar.Post(ts.URL+"/api/admin/invites", "application/json", nil)
	if err != nil || r.StatusCode != 403 {
		t.Fatalf("non-admin invite create: want 403 got %v %d", err, r.StatusCode)
	}
}
