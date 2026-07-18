#!/usr/bin/env sh
# Production regression journey for the authentication, room, and upload boundaries.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
if [ -n "${TOGETHER_DATA:-}" ]; then
  printf '%s\n' 'security-e2e: TOGETHER_DATA is managed by this disposable journey' >&2
  exit 2
fi
TMP=$(mktemp -d "${TMPDIR:-/tmp}/together-security-e2e.XXXXXX")
ADDR=${TOGETHER_E2E_ADDR:-127.0.0.1:18080}
BASE="http://$ADDR"
SERVER_PID=
WEB_DIST_WAS_DIRTY=false

if ! git -C "$ROOT" diff --quiet -- cmd/server/webdist/index.html; then
  WEB_DIST_WAS_DIRTY=true
fi

fail() {
  printf '%s\n' "security-e2e: $*" >&2
  exit 1
}

cleanup() {
  status=$?
  trap - 0 HUP INT TERM
  if [ -n "${SERVER_PID:-}" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  if [ "$WEB_DIST_WAS_DIRTY" = false ]; then
    git -C "$ROOT" restore --source=HEAD -- cmd/server/webdist/index.html 2>/dev/null || true
  fi
  rm -rf "$TMP"
  exit "$status"
}
trap cleanup 0 HUP INT TERM

start_server() {
  TOGETHER_DATA="$TMP/data" TOGETHER_ADDR="$ADDR" TOGETHER_ROOM_IDLE=2s \
    ADMIN_USER=admin ADMIN_PASS='correct horse battery staple' \
    "$ROOT/together" >"$TMP/server.log" 2>&1 &
  SERVER_PID=$!
  for _ in $(seq 1 50); do
    if curl --fail --silent --show-error "$BASE/healthz" >/dev/null 2>&1; then
      return
    fi
    if ! kill -0 "$SERVER_PID" 2>/dev/null; then
      cat "$TMP/server.log" >&2 || true
      fail "production server exited before becoming healthy"
    fi
    sleep 0.1
  done
  cat "$TMP/server.log" >&2 || true
  fail "production server did not become healthy"
}

stop_server() {
  if [ -n "${SERVER_PID:-}" ]; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
    SERVER_PID=
  fi
}

cd "$ROOT"
./build.sh

# A new database must fail closed before the successful boot below creates it.
if TOGETHER_DATA="$TMP/weak" TOGETHER_ADDR="$ADDR" ADMIN_USER=admin ADMIN_PASS=short \
  timeout 5s ./together >"$TMP/weak.log" 2>&1; then
  fail "weak first-boot administrator password started the server"
fi

start_server

if [ "${TOGETHER_E2E_INJECT_FAILURE:-}" = "1" ]; then
  fail "injected failure after production server start"
fi

ffmpeg -hide_banner -loglevel error -f lavfi -i color=c=black:s=16x16:d=1 -f lavfi -i anullsrc=r=44100:cl=mono \
  -shortest -c:v libx264 -pix_fmt yuv420p -c:a aac "$TMP/fixture.mp4"

cat >"$TMP/journey.go" <<'EOF'
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/coder/websocket"
)

type journey struct { base string; client *http.Client }

func die(format string, args ...any) { panic(fmt.Sprintf(format, args...)) }
func newClient() *http.Client { j, err := cookiejar.New(nil); if err != nil { die("cookie jar: %v", err) }; return &http.Client{Jar: j, Timeout: 10 * time.Second} }
func (j journey) request(method, path, body string, headers map[string]string) (int, []byte) {
	req, err := http.NewRequest(method, j.base+path, bytes.NewBufferString(body)); if err != nil { die("request %s: %v", path, err) }
	for k, v := range headers { req.Header.Set(k, v) }; if body != "" { req.Header.Set("Content-Type", "application/json") }
	resp, err := j.client.Do(req); if err != nil { die("request %s: %v", path, err) }; defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body); if err != nil { die("read %s: %v", path, err) }; return resp.StatusCode, b
}
func (j journey) expect(method, path, body string, headers map[string]string, want int) []byte { got, out := j.request(method, path, body, headers); if got != want { die("%s %s = %d, want %d: %s", method, path, got, want, out) }; return out }
func jsonID(b []byte, key string) int64 { var v map[string]any; if json.Unmarshal(b, &v) != nil { die("invalid JSON: %s", b) }; n, ok := v[key].(float64); if !ok { die("missing %s in %s", key, b) }; return int64(n) }
func jsonString(b []byte, key string) string { var v map[string]any; if json.Unmarshal(b, &v) != nil { die("invalid JSON: %s", b) }; s, ok := v[key].(string); if !ok { die("missing %s in %s", key, b) }; return s }
func (j journey) login(user, pass string, headers map[string]string, want int) []byte { return j.expect("POST", "/api/login", fmt.Sprintf(`{"username":%q,"password":%q}`, user, pass), headers, want) }
func (j journey) upload(path, title string) int64 {
	fi, err := os.Stat(path); if err != nil { die("stat fixture: %v", err) }
	id := jsonID(j.expect("POST", "/api/admin/media", fmt.Sprintf(`{"title":%q,"origName":"fixture.mp4","sizeBytes":%d}`, title, fi.Size()), nil, 200), "id")
	f, err := os.Open(path); if err != nil { die("open fixture: %v", err) }; defer f.Close()
	req, err := http.NewRequest("PATCH", fmt.Sprintf("%s/api/admin/media/%d/blob?offset=0", j.base, id), f); if err != nil { die("upload request: %v", err) }
	req.Header.Set("Upload-Length", fmt.Sprint(fi.Size()))
	resp, err := j.client.Do(req); if err != nil { die("upload: %v", err) }; out, _ := io.ReadAll(resp.Body); resp.Body.Close(); if resp.StatusCode != 200 { die("upload = %d: %s", resp.StatusCode, out) }
	j.expect("POST", fmt.Sprintf("/api/admin/media/%d/finish", id), "", nil, 202)
	for n := 0; n < 100; n++ { time.Sleep(100 * time.Millisecond); b := j.expect("GET", "/api/media", "", nil, 200); var media []struct { ID int64 `json:"id"`; Status string `json:"status"` }; if json.Unmarshal(b, &media) != nil { die("media JSON: %s", b) }; for _, m := range media { if m.ID == id && m.Status == "ready" { return id } } }
	die("media %d did not become ready", id); return 0
}
func (j journey) boundedUpload() {
	if code, _ := j.request("POST", "/api/admin/media", `{"title":"missing size","origName":"x.mp4"}`, nil); code != 400 { die("missing declared size = %d, want 400", code) }
	tooLargeJSON := `{"title":"` + strings.Repeat("x", 4096) + `","origName":"x.mp4","sizeBytes":1}`
	if code, _ := j.request("POST", "/api/admin/media", tooLargeJSON, nil); code != 413 { die("oversize creation JSON = %d, want 413", code) }
	id := jsonID(j.expect("POST", "/api/admin/media", `{"title":"bounded","origName":"bounded.bin","sizeBytes":2}`, nil, 200), "id")
	patch := func(target, total, offset int64, body io.Reader, want int) {
		req, err := http.NewRequest("PATCH", fmt.Sprintf("%s/api/admin/media/%d/blob?offset=%d", j.base, target, offset), body); if err != nil { die("bounded upload request: %v", err) }
		req.Header.Set("Upload-Length", fmt.Sprint(total))
		resp, err := j.client.Do(req); if err != nil { die("bounded upload: %v", err) }; defer resp.Body.Close(); if resp.StatusCode != want { out, _ := io.ReadAll(resp.Body); die("bounded upload = %d, want %d: %s", resp.StatusCode, want, out) }
	}
	patch(id, 2, 0, strings.NewReader("abc"), 409)
	b := j.expect("GET", fmt.Sprintf("/api/admin/media/%d/blob", id), "", nil, 200); if jsonID(b, "size") != 0 { die("overrun wrote bytes: %s", b) }
	patch(id, 2, 0, strings.NewReader("ab"), 200)
	chunkID := jsonID(j.expect("POST", "/api/admin/media", fmt.Sprintf(`{"title":"chunk","origName":"chunk.bin","sizeBytes":%d}`, (8<<20)+2), nil, 200), "id")
	patch(chunkID, (8<<20)+2, 0, strings.NewReader(strings.Repeat("x", (8<<20)+1)), 413)
	b = j.expect("GET", fmt.Sprintf("/api/admin/media/%d/blob", chunkID), "", nil, 200); if jsonID(b, "size") != 0 { die("oversize chunk changed size: %s", b) }
}
func (j journey) room(media int64) (string, string) { b := j.expect("POST", "/api/rooms", fmt.Sprintf(`{"mediaId":%d}`, media), nil, 201); var v map[string]string; if json.Unmarshal(b, &v) != nil { die("room JSON: %s", b) }; return v["id"], v["joinToken"] }
func (j journey) guest(token string) journey { g := journey{base:j.base, client:newClient()}; g.expect("POST", "/api/rooms/join", fmt.Sprintf(`{"token":%q,"name":"guest"}`, token), nil, 200); return g }
func (j journey) ws(room string) *websocket.Conn { u, _ := url.Parse(j.base); u.Scheme = "ws"; u.Path = "/ws/" + room; c, _, err := websocket.Dial(context.Background(), u.String(), &websocket.DialOptions{HTTPClient:j.client}); if err != nil { die("dial: %v", err) }; return c }
func readFrame(c *websocket.Conn) map[string]any { _, b, err := c.Read(context.Background()); if err != nil { die("read websocket: %v", err) }; var v map[string]any; if json.Unmarshal(b, &v) != nil { die("frame JSON: %s", b) }; return v }
func main() {
	if len(os.Args) != 3 { die("usage: journey BASE FIXTURE") }; j := journey{base:os.Args[1], client:newClient()}; fixture := os.Args[2]
	j.login("admin", "correct horse battery staple", nil, 200)
	invite := jsonString(j.expect("POST", "/api/admin/invites", "", nil, 200), "code")
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(invite) { die("invite is not 32 lowercase hex: %q", invite) }
	member := journey{base:j.base, client:newClient()}; member.expect("POST", "/api/register", fmt.Sprintf(`{"code":%q,"username":"member","password":"longpassword"}`, invite), nil, 200)
	for n := 0; n < 5; n++ { j.login("unknown", "wrong", map[string]string{"X-Forwarded-For":"198.51.100.1"}, 401) }
	j.login("unknown", "wrong", map[string]string{"X-Forwarded-For":"198.51.100.1"}, 429)
	member.login("member", "longpassword", map[string]string{"X-Forwarded-For":"198.51.100.2"}, 200)
	status, body := j.request("POST", "/api/login", `{"username":"unknown","password":"wrong"}`, map[string]string{"X-Forwarded-For":"not-an-ip, , ???"}); if status != 401 || string(body) != "{\"error\":\"invalid username or password\"}\n" { die("malformed proxy login = %d: %s", status, body) }
	if status, _ := j.request("GET", "/healthz", "", nil); status != 200 { die("server unhealthy after malformed proxy input") }
	j.boundedUpload()
	mediaOne, mediaTwo := j.upload(fixture, "one"), j.upload(fixture, "two")
	room, token := j.room(mediaOne); guest := j.guest(token)
	c := guest.ws(room); defer c.CloseNow(); hello := readFrame(c); if hello["type"] != "hello" { die("first frame = %v, want hello", hello) }
	if err := c.Write(context.Background(), websocket.MessageText, []byte(fmt.Sprintf(`{"type":"start","mediaId":%d}`, mediaTwo))); err != nil { die("start write: %v", err) }
	for { frame := readFrame(c); if frame["type"] == "error" { if frame["body"] != "media does not match room" { die("unexpected start error: %v", frame) }; break } }
	c.CloseNow()
	connections := make([]*websocket.Conn, 0, 12); for n := 0; n < 12; n++ { conn := guest.ws(room); if readFrame(conn)["type"] != "hello" { die("socket %d missed hello", n+1) }; connections = append(connections, conn) }
	extra := guest.ws(room); _, _, err := extra.Read(context.Background()); extra.CloseNow(); if websocket.CloseStatus(err) != websocket.StatusPolicyViolation { die("thirteenth socket close = %v, want policy violation", err) }
	for _, conn := range connections { conn.CloseNow() }
	time.Sleep(250 * time.Millisecond)
	newToken := jsonString(j.expect("POST", "/api/rooms/"+room+"/token", "{}", nil, 200), "joinToken")
	if newToken == token { die("token regeneration did not replace token") }
	_, stale := j.request("GET", "/api/rooms/join/"+token, "", nil); _, unknown := j.request("GET", "/api/rooms/join/not-a-token", "", nil); if string(stale) != string(unknown) { die("stale and unknown token bodies differ: %s / %s", stale, unknown) }
	time.Sleep(3 * time.Second); if code, _ := j.request("GET", "/api/rooms/join/"+newToken, "", nil); code != 404 { die("empty room did not expire: %d", code) }
	for n := 0; n < 10; n++ { j.room(mediaOne) }; j.expect("POST", "/api/rooms", fmt.Sprintf(`{"mediaId":%d}`, mediaOne), nil, 429)
	if status, _ := j.request("GET", "/healthz", "", nil); status != 200 { die("server unhealthy at journey end") }
}
EOF

go run "$TMP/journey.go" "$BASE" "$TMP/fixture.mp4"

# Existing data must start without provisioning credentials and keep the account.
stop_server
TOGETHER_DATA="$TMP/data" TOGETHER_ADDR="$ADDR" TOGETHER_ROOM_IDLE=2s "$ROOT/together" >"$TMP/restart.log" 2>&1 &
SERVER_PID=$!
for _ in $(seq 1 50); do
  if curl --fail --silent "$BASE/healthz" >/dev/null 2>&1; then break; fi
  sleep 0.1
done
cat >"$TMP/restart.go" <<'EOF'
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"time"
)

func die(format string, args ...any) { panic(fmt.Sprintf(format, args...)) }
func main() {
	if len(os.Args) != 2 { die("usage: restart BASE") }
	jar, err := cookiejar.New(nil); if err != nil { die("cookie jar: %v", err) }
	c := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	req, err := http.NewRequest("POST", os.Args[1]+"/api/login", bytes.NewBufferString(`{"username":"member","password":"longpassword"}`)); if err != nil { die("login request: %v", err) }
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req); if err != nil { die("login: %v", err) }; if resp.StatusCode != 200 { die("restart login = %d", resp.StatusCode) }; resp.Body.Close()
	for _, path := range []string{"/api/media", "/api/rooms"} {
		resp, err = c.Get(os.Args[1] + path); if err != nil { die("GET %s: %v", path, err) }; body, _ := io.ReadAll(resp.Body); resp.Body.Close(); if resp.StatusCode != 200 { die("GET %s = %d", path, resp.StatusCode) }
		if path == "/api/media" { var media []map[string]any; if json.Unmarshal(body, &media) != nil || len(media) < 2 { die("durable media missing after restart: %s", body) } } else { var rooms []any; if json.Unmarshal(body, &rooms) != nil || len(rooms) != 0 { die("ephemeral rooms survived restart: %s", body) } }
	}
}
EOF
go run "$TMP/restart.go" "$BASE"

printf '%s\n' 'security-e2e: passed'
