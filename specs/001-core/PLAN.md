# PLAN.md — Together V1 Refactor (specs/001-core)

Refactor of the existing Go monolith (accounts + SQLite + server-hosted streaming) into the
ephemeral, in-memory, local-playback product defined by `specs/001-core/PRD.md` and
`ARCHITECTURE.md`.

## Ground rules for every chunk

- Max ~300 lines of **new** code per chunk (deletions are free).
- Every chunk ends with a git commit (message given per chunk, conventional style).
- `gofmt -l internal cmd` must be empty and `go test ./...` green before each commit.
- After any `npm run build`: `git restore cmd/server/webdist/index.html` before committing.
- UI tokens only from `design.md` via `@theme` in `web/src/app.css`. No raw hex.

## Spec deviations (decided up front, applied throughout)

| # | Spec says | We do | Why |
|---|-----------|-------|-----|
| D1 | ARCHITECTURE §6: Node.js signaling server | Keep Go binary (`coder/websocket`) | Refactor of existing codebase; Go hub already proven |
| D2 | ARCHITECTURE §4.1: JWT `hostToken` | `crypto/rand` 32-hex token stored on Room | No JWT dep allowed; token is ephemeral anyway |
| D3 | ARCHITECTURE §1: Nginx/CDN static server | `http.ServeFile` from `STATIC_MEDIA_PATH` in same binary (behind Caddy) | ARCH §2.2 permits "simple HTTP module"; Range/206 free from stdlib |
| D4 | ARCHITECTURE §4.2: `MEDIA_ACTION` Host-only | Any participant may send it | PRD §2.2 story: "when **anyone** in the room clicks play…". PRD wins. Gate behind one `const hostOnlyMedia = false` for cheap flip |
| D5 | ARCHITECTURE §7.3: `SYNC_REQUEST` on reconnect | No such event; `ROOM_STATE_SYNC` carries users + canvas + recent chat (ring buffer, last 200) | Stateless hello-style recovery already proven in current hub; strictly simpler |
| D6 | ARCHITECTURE §4.2 has no host "select media" event | Add `MEDIA_SELECT: {mediaId}` (client→server, Host-only) and `MEDIA_SELECTED: {mediaId}` (server→client) | PRD §2.2 requires it; contract gap |
| D7 | — | SQLite, argon2id, sessions, invites, upload pipeline, ffmpeg all deleted; `go.mod` shrinks to `github.com/coder/websocket` only | PRD §7 memory-bound state, §8 out-of-scope accounts/streaming |

## What survives the refactor (reuse, don't rewrite)

- `internal/live/watch.go` — pure playback state machine (`WatchState`, `Apply`, `PositionAt`) and its test suite. `MEDIA_ACTION` maps directly onto existing intents.
- `web/src/lib/sync.js` — the JS mirror of `PositionAt` + tests. Change threshold only.
- `web/src/lib/ws.js` — reconnect/backoff + EMA clock offset (ping every 10s, `serverTime` ms).
- `web/src/lib/router.svelte.js`, `App.svelte` shell, `app.css` tokens, embed via `//go:embed`.
- Hub skeleton in `internal/live/hub.go` — per-room mutex, fan-out, join/leave plumbing.

---

## Phase 1 — Setup / State (in-memory replaces DB)

### Chunk 1: in-memory room store

**Files:** `internal/state/state.go` (new), `internal/state/state_test.go` (new)

**Requirements:**
- Structs per ARCHITECTURE §3.1: `Room` (id, hostID, mediaID, canvasState []Stroke, recentChat ring, createdAt, kicked set), `User` (id, roomID, displayName, role, status, playbackTime, lastPing, joinedAt), `Stroke` (userID, points [][2]float64, color, width).
- `Store` with one `sync.RWMutex` guarding maps `rooms`, `users`, `usersByRoom` (ARCH §3.2 indices). `// ponytail: one global lock, per-room locks if >10 rooms ever hurts`.
- `CreateRoom() (roomID, hostToken)` — 12-char base32 room id, 32-hex host token (D2).
- `Join(roomID, displayName, hostToken)` — validates name (trimmed, 2–32 chars, `^\S(.*\S)?$`), enforces `MAX_ROOM_SIZE`, rejects kicked user IDs, assigns HOST iff token matches else GUEST, first-ever joiner with valid token is host.
- `Leave`, `Promote`, `Kick` (adds to room kicked set), `SetStatus`, `SetMedia`, `AppendStrokes` (cap total stored strokes, e.g. 10k, drop-oldest), `ClearCanvas`, `AppendChat` (ring 200), `OldestUser(roomID)` for migration, `DeleteRoomIfEmpty`.
- Table-driven tests: name validation edges, capacity, kick-block, promote, oldest-user ordering, chat ring wrap.

**Acceptance:** `go test ./internal/state/ -v` green; zero SQLite imports; no goroutines in this package (pure data + lock).

**Do NOT:** touch `internal/db` yet (deleted in Chunk 13); add persistence of any kind; add per-room locks; export the mutex.

**Commit:** `feat: in-memory ephemeral room store (state pkg)`

### Chunk 2: config + media catalog

**Files:** `cmd/server/main.go` (edit), `internal/catalog/catalog.go` (new), `internal/catalog/catalog_test.go` (new)

**Requirements:**
- Env config per ARCHITECTURE §8, read once in `main()` into a plain struct: `TOGETHER_ADDR` (keep, maps PORT), `MAX_ROOM_SIZE` (50), `PING_INTERVAL` (10000), `CANVAS_BATCH_RATE` (50), `STATIC_MEDIA_PATH` (default `./media`), `MAX_SERVER_MEMORY_MB` (1800). Drop `TOGETHER_DATA`, `ADMIN_USER`, `ADMIN_PASS`.
- `catalog.Scan(dir)` → `[]Item{ID, Filename, SizeBytes, URL}`; ID = hex of filename hash; URL = `/media/{id}`; extensions allowlist (mp4, mkv, webm, mp3, m4a, srt, vtt). Rescan on each `GET /api/media` call — `// ponytail: rescan per request, cache if catalog grows past ~1k files`.
- `catalog.Path(dir, id)` resolves id → absolute file path, rejects unknown ids (no path traversal possible since ids are hashes).
- Tests with `t.TempDir()` fixture files.

**Acceptance:** `Scan` returns correct sizes/urls; unknown id → error; hidden/dot files and unknown extensions excluded.

**Do NOT:** watch the filesystem (fsnotify — new dep, banned); read media file contents; keep any admin seeding code path alive beyond compile.

**Commit:** `feat: env config + static media catalog scanner`

---

## Phase 2 — Backend routes

### Chunk 3: HTTP endpoints

**Files:** `internal/api/rooms.go` (rewrite), `internal/api/rooms_test.go` (rewrite), `cmd/server/main.go` (edit)

**Requirements:**
- `POST /api/rooms` → `201 {roomId, hostToken}` (ARCH §4.1). Before create: `runtime.ReadMemStats`; if heap > `MAX_SERVER_MEMORY_MB`, `503`. No auth.
- `GET /api/media` → `200 [{id, filename, sizeBytes, url}]`. No auth.
- `GET /media/{id}` → `http.ServeFile` (Range free). No auth (D3 — files are the point of the download model).
- Remove every `auth.Require` wrapper from mux; delete `/api/login`, `/api/me`, `/api/register`, `/api/invites`, upload routes from `main.go` route table (packages die in Chunk 13).
- httptest coverage: create room shape, media list, 206 Range request, unknown media id → 404.

**Acceptance:** `curl --noproxy '*'` smoke on `:18080`: create room, list media, download with `Range: bytes=0-99` → 206.

**Do NOT:** add rate limiting, CORS config, or TLS handling (Caddy's job); return file paths in JSON (only `/media/{id}` urls).

**Commit:** `refactor: unauthenticated ephemeral HTTP API (rooms, media catalog, downloads)`

### Chunk 4: WS core protocol

**Files:** `internal/live/hub.go` (rewrite), `internal/live/hub_test.go` (rewrite)

**Requirements:**
- `GET /ws/{roomId}` unauthenticated. First client frame MUST be `JOIN_ROOM {roomId, displayName, hostToken?}` within 5s, else close.
- Envelope: `{"type": "...", ...payload}` — SCREAMING_SNAKE types exactly per ARCHITECTURE §4.2.
- On join: `state.Join`; reply `ROOM_STATE_SYNC {users, hostId, mediaId, currentCanvas, recentChat}` (D5); broadcast `USER_JOINED {user}` to others.
- Handle `CHAT_MESSAGE` → append to ring → broadcast `CHAT_BROADCAST {messageId, userId, displayName, content, timestamp}` (timestamp unix **seconds**, keep existing convention).
- Handle `UPDATE_STATUS` (validate enum) → broadcast `USER_UPDATED {userId, status, role}`.
- Keep ping/pong with `serverTime` unix **ms** (existing clock-sync contract). Update `lastPing` on any inbound frame.
- On disconnect: `state.Leave`, broadcast `USER_LEFT {userId}` (host migration wired in Chunk 7 — for now host just leaves).
- Unknown type / malformed JSON → `ERROR {code, message}`, connection stays open.
- Tests: real WS dials via httptest — join handshake, name rejection, two-client chat fan-out, state sync on second join.

**Acceptance:** two `websocket.Dial` clients exchange chat; reconnecting client gets full `ROOM_STATE_SYNC`; bad first frame closes conn.

**Do NOT:** persist anything; implement media/canvas/manage events yet; break the ms/seconds time-unit split; add event replay.

**Commit:** `refactor: ephemeral WS protocol — join, state sync, chat, status`

### Chunk 5: media sync

**Files:** `internal/live/hub.go` (edit), `internal/live/watch.go` (edit, minimal), `internal/live/hub_test.go` (edit)

**Requirements:**
- `MEDIA_SELECT {mediaId}` — Host-only (D6): validate id against catalog, `state.SetMedia`, broadcast `MEDIA_SELECTED {mediaId}`, reset `WatchState`.
- `MEDIA_ACTION {action: PLAY|PAUSE|SEEK, timestamp}` — any participant (D4, `hostOnlyMedia` const). Map to existing `watch.Apply` intents under the room lock; broadcast `MEDIA_SYNC {action, timestamp, hostId}` where `timestamp` is the server-projected absolute position (`PositionAt`).
- `WatchState` lives on the in-memory Room (no more `state_json` checkpoint — table is gone).
- Reject `MEDIA_ACTION` when room has no selected media → `ERROR`.
- Tests: select→play→projected position advances; SEEK rebroadcast absolute; guest PLAY accepted (D4); MEDIA_SELECT from guest → `ERROR`.

**Acceptance:** `go test ./internal/live/ -v` green including untouched `watch_test.go`.

**Do NOT:** modify `PositionAt` math (JS mirror!); let clients set arbitrary rate; checkpoint to any storage.

**Commit:** `feat: room media selection + synced playback intents over WS`

### Chunk 6: canvas events

**Files:** `internal/live/hub.go` (edit), `internal/live/hub_test.go` (edit)

**Requirements:**
- `CANVAS_DRAW {points, color, width}`: validate — ≤100 points per payload (ARCH §3.2), raw frame ≤1KB (ARCH §7.5), finite coords, width 1–64, color matches `^#[0-9a-fA-F]{6}$`. Valid → `state.AppendStrokes` + broadcast `CANVAS_UPDATE {userId, points, color, width}` to **others** (sender rendered optimistically, PRD §3).
- Oversized/malformed payloads: drop silently (ARCH §7.5 — no ERROR spam at 20 fps).
- `CANVAS_CLEAR {}` — Host-only → `state.ClearCanvas` + broadcast `CANVAS_CLEARED {}` to all.
- Tests: batch >100 points dropped; >1KB frame dropped; clear by guest → `ERROR`; late joiner receives strokes in `ROOM_STATE_SYNC.currentCanvas`.

**Acceptance:** two-client test: A draws, B receives `CANVAS_UPDATE`, C joins late and gets full canvas.

**Do NOT:** echo strokes back to sender; store strokes anywhere but the Room struct; add smoothing/interpolation server-side (client concern).

**Commit:** `feat: batched collaborative canvas over WS with payload limits`

### Chunk 7: roles, kick, host migration, culling

**Files:** `internal/live/hub.go` (edit), `internal/state/state.go` (edit), `internal/live/hub_test.go` (edit)

**Requirements:**
- `MANAGE_USER {targetUserId, action: PROMOTE|KICK}` — Host-only. PROMOTE → target becomes HOST, old host becomes GUEST, broadcast `USER_UPDATED` for both. KICK → send `KICKED {reason}`, close socket, add to room kicked set (blocked for session lifetime, PRD §2.1), broadcast `USER_LEFT`.
- Host disconnect: start 5s timer (ARCH §7.4). If host's userID not back by then → promote `state.OldestUser` (by joinedAt), broadcast `USER_UPDATED`. Reconnect within grace cancels timer (rejoin carries hostToken → still host).
- Dead-connection culling: per-room ticker (interval = `PING_INTERVAL`×3); users with stale `lastPing` are force-disconnected → normal leave path.
- Empty-room GC: last user gone → delete room after 60s grace. `// ponytail: fixed 60s grace, make configurable if anyone asks`.
- Tests: kick blocks re-join; promote swaps roles; host drop → oldest inherits after grace (use short test grace via injected duration); empty room reaped.

**Acceptance:** migration test passes deterministically (injectable clock/durations, no `time.Sleep` slop beyond grace).

**Do NOT:** persist kick lists; migrate host to most-recent user (spec says **oldest**); leak timers (every timer stopped on room delete — test it).

**Commit:** `feat: host controls, migration on disconnect, dead-conn culling, room GC`

---

## Phase 3 — Frontend scaffolding

### Chunk 8: client plumbing for new protocol

**Files:** `web/src/lib/api.js` (rewrite), `web/src/lib/ws.js` (edit), `web/src/lib/router.svelte.js` (edit if needed), `web/src/App.svelte` (edit), `web/src/lib/sync.js` (edit), `web/src/lib/sync.test.js` (edit)

**Requirements:**
- `api.js`: only `createRoom()` and `getMedia()`. Delete session/login/invite calls.
- Routes: `#/` → Home, `#/r/{roomId}` → Room. App.svelte drops the `/api/me` gate — no auth.
- `ws.js`: keep backoff (1s, 2s, 4s, 8s… cap 30s) and EMA offset; on (re)open, send `JOIN_ROOM` from stored identity; expose typed send helpers (`sendChat`, `sendMediaAction`, `sendCanvasDraw`, …); surface `connected` state for the offline overlay.
- Identity: `sessionStorage` per-tab — `displayName`, and `hostToken` keyed by roomId (host refresh keeps host role through the 5s grace).
- `sync.js`: drift threshold 1.5s (PRD §6) replacing 1s; keep `PositionAt` mirror byte-compatible with Go; update tests.

**Acceptance:** `node --test web/src/lib/sync.test.js` green; `npm run build` succeeds with pages temporarily stubbed.

**Do NOT:** add a router/state library; touch `app.css` tokens; keep any `upload.js` import alive; use localStorage for identity (tab-scoped ephemerality fits the product).

**Commit:** `refactor: client plumbing for ephemeral protocol (api, ws, routes, drift 1.5s)`

---

## Phase 4 — Components

### Chunk 9: shell — home, join gate, theater layout, offline overlay

**Files:** `web/src/pages/Home.svelte` (new), `web/src/pages/Room.svelte` (rewrite), `web/src/components/JoinGate.svelte` (new), `web/src/components/SidePanel.svelte` (new)

**Requirements:**
- Home: one "Create room" button → `POST /api/rooms` → store hostToken → navigate `#/r/{id}` → show copyable invite URL.
- JoinGate: display-name prompt (2–32, trimmed, inline validation mirroring server regex) shown before WS connect for anyone without a stored name.
- Room layout per ARCHITECTURE §5: theater area dominates; `SidePanel` collapsible (CSS grid + one `$state` bool, ≥44px toggle target), holds Participants + Chat slots.
- ConnectionBoundary behavior inside Room.svelte: `connected === false` → dim UI, disable inputs, "Offline / Reconnecting…" banner (PRD §6). `// ponytail: overlay lives in Room.svelte, extract component if a second page ever needs it`.
- Warm minimalist per PRD §4: rounded corners, tokens from `design.md` only, Lucide icons, no emoji in chrome.

**Acceptance:** two browsers: create → share link → guest names self → both listed; kill server → overlay appears; restart → auto-rejoin via backoff.

**Do NOT:** build Login/Rooms/Admin replacements; add transitions that ignore `prefers-reduced-motion`; put chat logic here.

**Commit:** `feat: home, join gate, collapsible theater layout, offline overlay`

### Chunk 10: participants + chat

**Files:** `web/src/components/Participants.svelte` (new), `web/src/components/Chat.svelte` (edit)

**Requirements:**
- Participant rows: name, role marker (host), status badge — WAITING / DOWNLOADING / FILE_READY / IN_SYNC (PRD §3) with token colors.
- Host-only controls per row: "Promote to Host", "Remove from Room" → `MANAGE_USER`. Hidden for guests. ≥44px targets.
- On `KICKED`: show terminal "Removed from room" state, no auto-reconnect (blocked server-side anyway).
- Chat: rewire to `CHAT_MESSAGE`/`CHAT_BROADCAST`, temp display names, timestamps (unix seconds → local time), emoji passthrough (native input — no picker lib), history from `ROOM_STATE_SYNC.recentChat`.

**Acceptance:** promote swaps host badge live in both browsers; kicked tab shows terminal state and cannot rejoin; chat survives a reconnect (ring buffer replayed).

**Do NOT:** add an emoji picker dependency; sanitize by regex (render as text nodes — Svelte default escaping is the fix); paginate history.

**Commit:** `feat: participant list with host controls + chat retool`

### Chunk 11: media layer — local playback

**Files:** `web/src/components/Player.svelte` (rewrite), `web/src/pages/Room.svelte` (edit)

**Requirements:**
- Empty state: no `mediaId` → "Waiting for the host to select media" (PRD §6). Host additionally sees the catalog (from `GET /api/media`) and picks → `MEDIA_SELECT`.
- Media selected: show "Download media" anchor (`/media/{id}`, `download` attr) → send `UPDATE_STATUS DOWNLOADING` on click; `<input type="file">` to load local copy → `URL.createObjectURL` into `<video>` → `UPDATE_STATUS FILE_READY`.
- Mismatch check (PRD §5/§6): compare `file.name`/`file.size` to catalog item; mismatch → non-blocking toast, playback still allowed.
- Sync loop (keep existing echo-driven pattern): controls send `MEDIA_ACTION`; `<video>` mutates only on `MEDIA_SYNC` broadcasts; 500ms loop — drift >1.5s hard seek (silent, PRD §6), >0.15s playbackRate nudge; report `IN_SYNC` when within threshold while playing.
- Block native controls from acting locally-only (intercept, forward as intents — existing Player pattern survives).

**Acceptance:** two browsers, both load the same local file: play/pause/seek from either side syncs the other; wrong file → warning toast but playable; drift injected via devtools seek self-heals within one loop tick.

**Do NOT:** stream anything from server into `<video>` (PRD §7 — download-and-sync only); change `sync.js` math; auto-play before user file load.

**Commit:** `feat: local-file player with download flow, mismatch warning, drift sync`

### Chunk 12: canvas layer

**Files:** `web/src/components/Canvas.svelte` (new), `web/src/lib/canvas.js` (new), `web/src/lib/canvas.test.js` (new), `web/src/pages/Room.svelte` (edit)

**Requirements:**
- `canvas.js` (pure, node-testable): stroke buffer with 50ms flush batching (`CANVAS_BATCH_RATE`), point normalization to canvas-relative coords, batch-size cap 100 → split.
- Canvas.svelte: pointer events → optimistic local render (PRD §3) + buffer; flush → `sendCanvasDraw`; incoming `CANVAS_UPDATE` → render others' strokes; `ROOM_STATE_SYNC.currentCanvas` replay on join/reconnect; transparent until first stroke (PRD §6).
- Toolbar: brush width, color (palette from design tokens), **Export** → `canvas.toBlob('image/png')` + anchor download (PRD §2.3), **Clear** (host-only) → `CANVAS_CLEAR`; `CANVAS_CLEARED` wipes everyone.
- Layered over/beside media per theater layout; `prefers-reduced-motion` respected.

**Acceptance:** `node --test web/src/lib/canvas.test.js` green (batch split, flush timing with fake timers, normalization); two-browser draw is mutual and near-instant locally; export saves a PNG; host clear wipes both.

**Do NOT:** requestAnimationFrame throttling beyond the 50ms batcher (keep one mechanism); canvas libs; server-side stroke smoothing; undo (out of scope).

**Commit:** `feat: collaborative canvas with 50ms batching, PNG export, host clear`

---

## Phase 5 — Integration

### Chunk 13: delete the old world

**Files (deleted):** `internal/auth/*`, `internal/db/*`, `internal/media/*`, `web/src/lib/upload.js`, `web/src/pages/Login.svelte`, `web/src/pages/Rooms.svelte`, `web/src/pages/Admin.svelte`, `deploy/backup.sh`, `deploy/together-backup.service`, `deploy/together-backup.timer`
**Files (edited):** `cmd/server/main.go`, `go.mod`, `go.sum`, `README.md`, `CLAUDE.md`, `deploy/together.service`, `build.sh` (only if paths changed)

**Requirements:**
- Delete every listed file; `go mod tidy` → `go.mod` requires exactly `github.com/coder/websocket` (D7).
- `main.go` final wiring: config → state store → catalog → mux (`/api/rooms`, `/api/media`, `/media/`, `/ws/`, embedded SPA fallback) → graceful shutdown closing all hubs.
- `together.service`: drop DB env vars; add `STATIC_MEDIA_PATH`; remove backup timer references.
- README + CLAUDE.md command sections updated (no ADMIN_USER/PASS, no ffmpeg requirement, new env vars).
- Full build: `./build.sh` → binary serves SPA; `git restore cmd/server/webdist/index.html`.

**Acceptance:** `go build ./...` with pruned go.mod; `grep -r "modernc\|argon2\|x/crypto" go.mod internal cmd` empty; binary smoke test on `:18080` — create room, join via browser, all four features work; server restart wipes rooms (PRD §7 — verify, that's a feature).

**Do NOT:** keep dead packages "for reference" (git history is the reference); leave orphan env vars in service files; commit built webdist.

**Commit:** `refactor: remove auth, sqlite, and upload pipeline — ephemeral in-memory only`

---

## Phase 6 — Testing

### Chunk 14: end-to-end hardening

**Files:** `internal/live/integration_test.go` (new), `internal/state/state_test.go` (edit), `web/src/lib/sync.test.js` (edit)

**Requirements:**
- One integration test file running the real stack (httptest server, real WS dials, no SQLite/ffmpeg anywhere):
  1. create room → host + 2 guests join → state sync shapes correct
  2. guest sends PLAY → all three converge on projected position
  3. canvas batch → fan-out + late-join replay
  4. kick → blocked rejoin; promote → role swap
  5. host socket drop → oldest guest inherits after grace (injected short grace)
  6. limits: 51st join rejected, 101-point batch dropped, 33-char name rejected
- Race check: `go test ./... -race` green.
- `sync.test.js`: backward-clock clamp + 1.5s threshold cases both covered.
- Final sweep: `gofmt -l internal cmd` empty; `npm run build`; `git restore cmd/server/webdist/index.html`; manual two-browser checklist appended to `.superpowers/sdd/progress.md`.

**Acceptance:** `go test ./... -race` and `node --test` both green in <30s total (no ffmpeg fixtures anymore — suite gets faster).

**Do NOT:** add testify or any test framework; test Svelte components headlessly (manual two-browser check per repo convention); write per-function micro-suites over the scenario test.

**Commit:** `test: full-stack integration suite for ephemeral rooms, sync, canvas, roles`
