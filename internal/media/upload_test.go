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
	"strconv"
	"strings"
	"testing"

	"together/internal/auth"
	"together/internal/db"
)

func adminClient(t *testing.T) (*http.Client, *httptest.Server, string) {
	dir := t.TempDir()
	d, _ := db.Open(filepath.Join(dir, "t.db"))
	t.Cleanup(func() { d.Close() })
	auth.Seed(d, "admin", "correct horse battery staple")
	mux := http.NewServeMux()
	auth.Routes(mux, d)
	UploadRoutes(mux, d, dir, DefaultMaxUploadBytes)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	c.Post(ts.URL+"/api/login", "application/json", strings.NewReader(`{"username":"admin","password":"correct horse battery staple"}`))
	return c, ts, dir
}

func patch(t *testing.T, c *http.Client, url string, body []byte, total int64) *http.Response {
	req, _ := http.NewRequest("PATCH", url, bytes.NewReader(body))
	req.Header.Set("Upload-Length", strconv.FormatInt(total, 10))
	r, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestChunkedUploadWithResume(t *testing.T) {
	c, ts, dir := adminClient(t)
	r, _ := c.Post(ts.URL+"/api/admin/media", "application/json",
		strings.NewReader(`{"title":"Test","origName":"t.mp4","sizeBytes":11}`))
	var m struct{ ID int64 }
	json.NewDecoder(r.Body).Decode(&m)

	patch(t, c, fmt.Sprintf("%s/api/admin/media/%d/blob?offset=0", ts.URL, m.ID), []byte("hello "), 11)
	// simulate reconnect: ask for size
	r, _ = c.Get(fmt.Sprintf("%s/api/admin/media/%d/blob", ts.URL, m.ID))
	var sz struct{ Size int64 }
	json.NewDecoder(r.Body).Decode(&sz)
	if sz.Size != 6 {
		t.Fatalf("resume size want 6 got %d", sz.Size)
	}
	patch(t, c, fmt.Sprintf("%s/api/admin/media/%d/blob?offset=%d", ts.URL, m.ID, sz.Size), []byte("world"), 11)

	// test subtitle endpoint
	r, _ = c.Post(fmt.Sprintf("%s/api/admin/media/%d/subtitle", ts.URL, m.ID), "text/plain",
		bytes.NewReader([]byte("1\n00:00:00,000 --> 00:00:05,000\nHello\n")))
	if r.StatusCode != 201 {
		t.Fatalf("subtitle POST: want 201 got %d", r.StatusCode)
	}
	var sub struct{ ID int }
	if err := json.NewDecoder(r.Body).Decode(&sub); err != nil {
		t.Fatalf("subtitle response decode: %v", err)
	}
	if sub.ID != 0 {
		t.Fatalf("subtitle id: want 0 got %d", sub.ID)
	}

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

func TestCreateMedia_IgnoresClientKind(t *testing.T) {
	c, ts, dir := adminClient(t)
	r, _ := c.Post(ts.URL+"/api/admin/media", "application/json",
		strings.NewReader(`{"kind":"audio","title":"X","origName":"song.opus","sizeBytes":1}`))
	if r.StatusCode != 200 {
		t.Fatalf("create: want 200 got %d", r.StatusCode)
	}
	var m struct{ ID int64 }
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if m.ID == 0 {
		t.Fatalf("want nonzero ID")
	}
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var kind string
	if err := d.QueryRow(`SELECT kind FROM media WHERE id=?`, m.ID).Scan(&kind); err != nil {
		t.Fatal(err)
	}
	if kind != "video" {
		t.Fatalf("client kind selected stored kind: got %q, want provisional video", kind)
	}
}

func TestCreateMedia_TitleRequired(t *testing.T) {
	c, ts, _ := adminClient(t)
	r, _ := c.Post(ts.URL+"/api/admin/media", "application/json",
		strings.NewReader(`{"origName":"song.mp3","sizeBytes":1}`))
	if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty title: want 400 got %d", r.StatusCode)
	}
}

func TestUploadBoundaries(t *testing.T) {
	c, ts, dir := adminClient(t)
	create := func(size int64) int64 {
		r, err := c.Post(ts.URL+"/api/admin/media", "application/json", strings.NewReader(fmt.Sprintf(`{"title":"Test","origName":"t.mp4","sizeBytes":%d}`, size)))
		if err != nil || r.StatusCode != http.StatusOK {
			t.Fatalf("create: response=%v err=%v", r, err)
		}
		defer r.Body.Close()
		var out struct{ ID int64 }
		json.NewDecoder(r.Body).Decode(&out)
		return out.ID
	}
	oversizedJSON := `{"title":"Test","origName":"t.mp4","sizeBytes":1,"padding":"` + strings.Repeat("x", 4096) + `"}`
	r, _ := c.Post(ts.URL+"/api/admin/media", "application/json", strings.NewReader(oversizedJSON))
	if r.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized JSON: want 413 got %d", r.StatusCode)
	}
	r, _ = c.Post(ts.URL+"/api/admin/media", "application/json", strings.NewReader(fmt.Sprintf(`{"title":"Test","origName":"t.mp4","sizeBytes":%d}`, DefaultMaxUploadBytes+1)))
	if r.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("configured maximum: want 413 got %d", r.StatusCode)
	}

	id := create(16 << 20)
	for offset := int64(0); offset < 16<<20; offset += 8 << 20 {
		r := patch(t, c, fmt.Sprintf("%s/api/admin/media/%d/blob?offset=%d", ts.URL, id, offset), make([]byte, 8<<20), 16<<20)
		if r.StatusCode != http.StatusOK {
			t.Fatalf("chunk at %d: want 200 got %d", offset, r.StatusCode)
		}
	}
	r, _ = c.Post(fmt.Sprintf("%s/api/admin/media/%d/finish", ts.URL, id), "", nil)
	if r.StatusCode != http.StatusAccepted {
		t.Fatalf("complete finish: want 202 got %d", r.StatusCode)
	}

	id = create(8 << 20)
	r = patch(t, c, fmt.Sprintf("%s/api/admin/media/%d/blob?offset=0", ts.URL, id), make([]byte, 8<<20+1), 8<<20)
	if r.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized chunk: want 413 got %d", r.StatusCode)
	}
	if fi, err := os.Stat(UploadPath(dir, id)); err == nil && fi.Size() != 0 {
		t.Fatalf("oversized chunk committed %d bytes", fi.Size())
	}

	id = create(2)
	r = patch(t, c, fmt.Sprintf("%s/api/admin/media/%d/blob?offset=0", ts.URL, id), []byte("abc"), 2)
	if r.StatusCode != http.StatusConflict {
		t.Fatalf("overrun: want 409 got %d", r.StatusCode)
	}
	r, _ = c.Post(fmt.Sprintf("%s/api/admin/media/%d/finish", ts.URL, id), "", nil)
	if r.StatusCode != http.StatusConflict {
		t.Fatalf("premature finish: want 409 got %d", r.StatusCode)
	}
	r, _ = c.Post(fmt.Sprintf("%s/api/admin/media/%d/subtitle", ts.URL, id), "text/plain", bytes.NewReader(make([]byte, 10<<20+1)))
	if r.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized subtitle: want 413 got %d", r.StatusCode)
	}
}

func TestLegacyUploadEstablishesLengthOnce(t *testing.T) {
	c, ts, dir := adminClient(t)
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	res, err := d.Exec(`INSERT INTO media (kind, title, status) VALUES ('video', 'legacy', 'uploading')`)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	r := patch(t, c, fmt.Sprintf("%s/api/admin/media/%d/blob?offset=0", ts.URL, id), []byte("a"), 2)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("legacy patch: want 200 got %d", r.StatusCode)
	}
	r = patch(t, c, fmt.Sprintf("%s/api/admin/media/%d/blob?offset=1", ts.URL, id), []byte("b"), 3)
	if r.StatusCode != http.StatusConflict {
		t.Fatalf("conflicting total: want 409 got %d", r.StatusCode)
	}
}
