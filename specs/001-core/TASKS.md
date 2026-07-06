# TASKS.md — Together V1 Refactor (specs/001-core)

Derived from `specs/001-core/PLAN.md`. One task per plan chunk (already single-prompt sized). Each task is completable in a single LLM prompt (~50–300 lines new code; deletions free).

**Global rules (apply to every task):**
- `gofmt -l internal cmd` empty and `go test ./...` green before commit.
- After any `npm run build`: `git restore cmd/server/webdist/index.html` before committing.
- UI tokens only from `design.md` via `@theme` in `web/src/app.css`. No raw hex.
- Go deps must not grow; end state is `github.com/coder/websocket` only.
- Each task ends in one conventional-style commit (message given per task).

**Spec deviations in force (from PLAN.md §"Spec deviations"):** D1 keep Go binary; D2 32-hex host token not JWT; D3 `http.ServeFile` from `STATIC_MEDIA_PATH`; D4 any participant may send `MEDIA_ACTION` (gate `hostOnlyMedia=false`); D5 `ROOM_STATE_SYNC` stateless recovery (no `SYNC_REQUEST`); D6 add `MEDIA_SELECT`/`MEDIA_SELECTED`; D7 delete SQLite/argon2/upload/ffmpeg.

**Dependency graph (quick view):**
```
T01 ─┬─ T03 ─┬─ T04 ─┬─ T05 ─┐
T02 ─┘        │       ├─ T06 ─┤
              │       └─ T07 ─┤
              └─ T08 ─┬─ T09 ─┼─ T10
                      │       ├─ T11 (also T05)
                      │       └─ T12 (also T06)
                                    │
   (T01,T02,T04,T05,T06,T07,T09,T10,T11,T12) ─> T13 ─> T14
```

---

## 1. T01 — In-memory room store

- **id:** T01
- **title:** In-memory ephemeral room store (`state` pkg)
- **objective:** Replace SQLite-backed state with a pure in-memory store: rooms, users, strokes, chat ring, kick sets — data + one lock, no goroutines.
- **inputs:** ARCHITECTURE §3.1 memory structs, §3.2 indices/constraints; PLAN Chunk 1.
- **outputs:** `state.Store` with `CreateRoom`, `Join`, `Leave`, `Promote`, `Kick`, `SetStatus`, `SetMedia`, `AppendStrokes`, `ClearCanvas`, `AppendChat`, `OldestUser`, `DeleteRoomIfEmpty`; table-driven tests.
- **dependencies:** none
- **files to create:** `internal/state/state.go`, `internal/state/state_test.go`
- **files to modify:** none
- **acceptance criteria:**
  - `go test ./internal/state/ -v` green.
  - Zero SQLite imports; no goroutines (pure data + `sync.RWMutex`).
  - Name validation (trimmed, 2–32, `^\S(.*\S)?$`), `MAX_ROOM_SIZE` capacity, kicked-ID rejection, HOST iff token match (first valid-token joiner is host), promote, `OldestUser` by `joinedAt`, chat ring wrap (200) all covered by tests.
  - 12-char base32 room id; 32-hex host token (D2). Strokes capped (~10k, drop-oldest).
- **difficulty:** M (biggest data unit; lots of methods but no concurrency logic)
- **context pack:**
  - Paste: `ARCHITECTURE.md` §3 (§3.1 structs, §3.2 constraints/indices), PLAN.md Chunk 1, existing `internal/live/watch.go` (for `WatchState` type the Room will later hold).
  - Contract sections to obey: §3.1 field names/types; §3.2 `displayName` regex + `room_capacity` + `canvas_limits`.
- **commit:** `feat: in-memory ephemeral room store (state pkg)`
- **do NOT:** touch `internal/db`; add persistence; add per-room locks; export the mutex.

---

## 2. T02 — Config + media catalog

- **id:** T02
- **title:** Env config + static media catalog scanner
- **objective:** Read env config once into a struct; scan `STATIC_MEDIA_PATH` into a hash-id catalog with per-id path resolution.
- **inputs:** ARCHITECTURE §8 config; §4.1 `GET /api/media` shape; PLAN Chunk 2.
- **outputs:** config struct in `main.go`; `catalog.Scan(dir) []Item{ID,Filename,SizeBytes,URL}`; `catalog.Path(dir,id)`; tests.
- **dependencies:** none
- **files to create:** `internal/catalog/catalog.go`, `internal/catalog/catalog_test.go`
- **files to modify:** `cmd/server/main.go`
- **acceptance criteria:**
  - Config: `TOGETHER_ADDR`, `MAX_ROOM_SIZE`(50), `PING_INTERVAL`(10000), `CANVAS_BATCH_RATE`(50), `STATIC_MEDIA_PATH`(`./media`), `MAX_SERVER_MEMORY_MB`(1800). Dropped: `TOGETHER_DATA`, `ADMIN_USER`, `ADMIN_PASS`.
  - `Scan` returns correct sizes + `/media/{id}` urls; ID = hex of filename hash; allowlist mp4/mkv/webm/mp3/m4a/srt/vtt; dot-files and unknown extensions excluded.
  - `Path` rejects unknown ids (no traversal — ids are hashes).
  - `go test ./internal/catalog/ -v` green with `t.TempDir()` fixtures.
- **difficulty:** S
- **context pack:**
  - Paste: `ARCHITECTURE.md` §8 (config) + §4.1 (`GET /api/media` response), PLAN.md Chunk 2.
  - Contract sections to obey: §8 var names/defaults (mapped: `PORT`→`TOGETHER_ADDR`); §4.1 item shape.
- **commit:** `feat: env config + static media catalog scanner`
- **do NOT:** add fsnotify/filesystem watch; read file contents; keep admin seeding.

---

## 3. T03 — HTTP endpoints

- **id:** T03
- **title:** Unauthenticated ephemeral HTTP API (rooms, media, downloads)
- **objective:** Rewrite REST surface to three unauthenticated endpoints; strip all auth wrappers and dead routes from the mux.
- **inputs:** ARCHITECTURE §4.1; D3; PLAN Chunk 3; T01 store, T02 catalog.
- **outputs:** rewritten `internal/api/rooms.go` + tests; mux edits in `main.go`.
- **dependencies:** T01, T02
- **files to modify (rewrite):** `internal/api/rooms.go`, `internal/api/rooms_test.go`, `cmd/server/main.go`
- **acceptance criteria:**
  - `POST /api/rooms` → `201 {roomId, hostToken}`; pre-create `runtime.ReadMemStats`, heap > `MAX_SERVER_MEMORY_MB` → `503`. No auth.
  - `GET /api/media` → `200 [{id,filename,sizeBytes,url}]`. No auth.
  - `GET /media/{id}` → `http.ServeFile` (Range/206 free). No auth.
  - All `auth.Require` wrappers removed; `/api/login`, `/api/me`, `/api/register`, `/api/invites`, upload routes deleted from route table.
  - httptest: create-room shape, media list, `Range: bytes=0-99` → 206, unknown id → 404.
  - Smoke on `:18080` with `curl --noproxy '*'` passes.
- **difficulty:** M
- **context pack:**
  - Paste: `ARCHITECTURE.md` §4.1 + §2.2 (static asset server) + §7.5, PLAN.md Chunk 3, current `internal/api/rooms.go`, current `cmd/server/main.go` route table.
  - Contract sections to obey: §4.1 (status codes, bodies, no auth); §2.2 "simple HTTP module".
- **commit:** `refactor: unauthenticated ephemeral HTTP API (rooms, media catalog, downloads)`
- **do NOT:** add rate limiting/CORS/TLS (Caddy's job); return file paths in JSON.

---

## 4. T04 — WS core protocol

- **id:** T04
- **title:** Ephemeral WS protocol — join, state sync, chat, status
- **objective:** Rewrite the hub to the new SCREAMING_SNAKE JSON protocol: join handshake, `ROOM_STATE_SYNC`, chat fan-out, status updates, ping/pong.
- **inputs:** ARCHITECTURE §4.2 events; D5; PLAN Chunk 4; T01 store; T03 mux.
- **outputs:** rewritten `internal/live/hub.go` + tests.
- **dependencies:** T01, T03
- **files to modify (rewrite):** `internal/live/hub.go`, `internal/live/hub_test.go`
- **acceptance criteria:**
  - `GET /ws/{roomId}` unauthenticated; first frame must be `JOIN_ROOM {roomId,displayName,hostToken?}` within 5s else close.
  - Envelope `{"type":"...",...payload}`, SCREAMING_SNAKE exactly per §4.2.
  - Join → `state.Join` → reply `ROOM_STATE_SYNC {users,hostId,mediaId,currentCanvas,recentChat}` (D5) + broadcast `USER_JOINED` to others.
  - `CHAT_MESSAGE` → ring append → `CHAT_BROADCAST {messageId,userId,displayName,content,timestamp}` (unix **seconds**).
  - `UPDATE_STATUS` (enum-validated) → `USER_UPDATED {userId,status,role}`.
  - ping/pong `serverTime` unix **ms**; `lastPing` updated on any inbound frame.
  - Disconnect → `state.Leave` + `USER_LEFT` (no migration yet).
  - Unknown type / malformed JSON → `ERROR {code,message}`, conn stays open.
  - Tests: real WS dials — join handshake, name rejection, two-client chat fan-out, second-join state sync, bad first frame closes.
- **difficulty:** L (protocol backbone; every later WS task extends it)
- **context pack:**
  - Paste: `ARCHITECTURE.md` §4.2 (full event list) + §7.3 (recovery), PLAN.md Chunk 4 + D5, current `internal/live/hub.go` skeleton (mutex/fan-out/join-leave), `web/src/lib/ws.js` (clock-sync ms contract).
  - Contract sections to obey: §4.2 exact event names/payloads; time-unit split (chat s, pong ms).
- **commit:** `refactor: ephemeral WS protocol — join, state sync, chat, status`
- **do NOT:** persist anything; implement media/canvas/manage yet; break ms/seconds split; add event replay.

---

## 5. T05 — Media sync

- **id:** T05
- **title:** Room media selection + synced playback intents over WS
- **objective:** Add host media selection and any-participant play/pause/seek mapped onto the surviving `watch.go` state machine, broadcasting server-projected absolute positions.
- **inputs:** ARCHITECTURE §4.2 `MEDIA_ACTION`/`MEDIA_SYNC`; D4, D6; PLAN Chunk 5; T04 hub; T02 catalog.
- **outputs:** hub edits + minimal `watch.go` wiring + tests.
- **dependencies:** T04, T02 (catalog validation), reuses `internal/live/watch.go`
- **files to modify:** `internal/live/hub.go`, `internal/live/watch.go` (minimal), `internal/live/hub_test.go`
- **acceptance criteria:**
  - `MEDIA_SELECT {mediaId}` Host-only (D6): validate id vs catalog → `state.SetMedia` → broadcast `MEDIA_SELECTED {mediaId}` → reset `WatchState`.
  - `MEDIA_ACTION {action,timestamp}` any participant (D4, `hostOnlyMedia` const): map to `watch.Apply` under room lock → broadcast `MEDIA_SYNC {action,timestamp,hostId}` with `timestamp` = `PositionAt` server projection.
  - `WatchState` held on the in-memory Room (no `state_json`).
  - `MEDIA_ACTION` with no selected media → `ERROR`.
  - Tests: select→play position advances; SEEK rebroadcast absolute; guest PLAY accepted; guest `MEDIA_SELECT` → `ERROR`. `go test ./internal/live/ -v` green incl. untouched `watch_test.go`.
- **difficulty:** M
- **context pack:**
  - Paste: `ARCHITECTURE.md` §4.2 (`MEDIA_ACTION`,`MEDIA_SYNC`) + §7.1 (drift), PLAN.md Chunk 5 + D4/D6, `internal/live/watch.go` (`Apply`/`PositionAt`), post-T04 `hub.go`.
  - Contract sections to obey: §4.2 media payloads; **do not change `PositionAt` math** (JS mirror in `sync.js`).
- **commit:** `feat: room media selection + synced playback intents over WS`
- **do NOT:** modify `PositionAt` math; let clients set arbitrary rate; checkpoint to storage.

---

## 6. T06 — Canvas events

- **id:** T06
- **title:** Batched collaborative canvas over WS with payload limits
- **objective:** Add validated `CANVAS_DRAW` fan-out (to others) and host-only `CANVAS_CLEAR`, with silent drop of oversized/malformed frames.
- **inputs:** ARCHITECTURE §3.2 canvas limits, §7.5 overload mitigation, §4.2; PLAN Chunk 6; T04 hub; T01 store.
- **outputs:** hub edits + tests.
- **dependencies:** T04, T01
- **files to modify:** `internal/live/hub.go`, `internal/live/hub_test.go`
- **acceptance criteria:**
  - `CANVAS_DRAW {points,color,width}`: validate ≤100 points, raw frame ≤1KB, finite coords, width 1–64, color `^#[0-9a-fA-F]{6}$` → `state.AppendStrokes` + broadcast `CANVAS_UPDATE {userId,points,color,width}` to **others**.
  - Oversized/malformed → dropped silently (no `ERROR`).
  - `CANVAS_CLEAR {}` Host-only → `state.ClearCanvas` + broadcast `CANVAS_CLEARED {}` to all.
  - Tests: >100-point batch dropped; >1KB frame dropped; guest clear → `ERROR`; late joiner gets strokes in `ROOM_STATE_SYNC.currentCanvas`.
- **difficulty:** M
- **context pack:**
  - Paste: `ARCHITECTURE.md` §3.2 (canvas_limits) + §7.5 (overload) + §4.2 (`CANVAS_DRAW`/`CANVAS_UPDATE`/`CANVAS_CLEAR`/`CANVAS_CLEARED`), PLAN.md Chunk 6, post-T04/T05 `hub.go`, `internal/state/state.go` (`AppendStrokes`/`ClearCanvas`).
  - Contract sections to obey: §7.5 (drop, don't error); §3.2 (100-point/1KB caps).
- **commit:** `feat: batched collaborative canvas over WS with payload limits`
- **do NOT:** echo strokes to sender; store strokes outside Room; server-side smoothing.

---

## 7. T07 — Roles, kick, host migration, culling

- **id:** T07
- **title:** Host controls, migration on disconnect, dead-conn culling, room GC
- **objective:** Add `MANAGE_USER` (promote/kick), 5s host-migration-to-oldest, stale-ping culling, and 60s empty-room GC — all with injectable durations.
- **inputs:** ARCHITECTURE §4.2 `MANAGE_USER`/`KICKED`, §7.4 migration; PLAN Chunk 7; T04 hub; T01 store.
- **outputs:** hub + state edits + tests.
- **dependencies:** T04, T01
- **files to modify:** `internal/live/hub.go`, `internal/state/state.go`, `internal/live/hub_test.go`
- **acceptance criteria:**
  - `MANAGE_USER {targetUserId,action}` Host-only. PROMOTE → target HOST, old host GUEST, `USER_UPDATED` for both. KICK → `KICKED {reason}` + close socket + add to room kicked set (blocked session-lifetime) + `USER_LEFT`.
  - Host disconnect → 5s timer; not back → promote `state.OldestUser` (by `joinedAt`) + `USER_UPDATED`; reconnect within grace (hostToken) cancels.
  - Dead-conn culling: per-room ticker (`PING_INTERVAL`×3); stale `lastPing` → force-disconnect via normal leave.
  - Empty-room GC: last user gone → delete after 60s grace.
  - Tests (injected short grace, no `time.Sleep` slop): kick blocks rejoin; promote swaps; host drop → oldest inherits; empty room reaped; all timers stopped on room delete.
- **difficulty:** L (timers + concurrency; highest leak risk)
- **context pack:**
  - Paste: `ARCHITECTURE.md` §7.4 (migration) + §4.2 (`MANAGE_USER`,`KICKED`,`USER_UPDATED`,`USER_LEFT`), PLAN.md Chunk 7, post-T04 `hub.go`, `internal/state/state.go` (`Promote`/`Kick`/`OldestUser`/`DeleteRoomIfEmpty`).
  - Contract sections to obey: §7.4 (oldest by `createdAt`/`joinedAt`, not newest); kick blocks for session lifetime.
- **commit:** `feat: host controls, migration on disconnect, dead-conn culling, room GC`
- **do NOT:** persist kick lists; migrate to newest user; leak timers.

---

## 8. T08 — Client plumbing for new protocol

- **id:** T08
- **title:** Client plumbing for ephemeral protocol (api, ws, routes, drift 1.5s)
- **objective:** Rewire frontend libs to the new unauthenticated protocol: two API calls, typed WS send helpers, tab-scoped identity, 1.5s drift threshold.
- **inputs:** PRD §6 (drift), PLAN Chunk 8; backend protocol from T03–T07.
- **outputs:** rewritten `api.js`, edited `ws.js`/`sync.js`/`App.svelte`/router; updated `sync.test.js`.
- **dependencies:** T03 (api), protocol defined by T04–T07
- **files to modify:** `web/src/lib/api.js` (rewrite), `web/src/lib/ws.js`, `web/src/lib/router.svelte.js` (if needed), `web/src/App.svelte`, `web/src/lib/sync.js`, `web/src/lib/sync.test.js`
- **acceptance criteria:**
  - `api.js`: only `createRoom()` + `getMedia()`; session/login/invite calls deleted.
  - Routes `#/` → Home, `#/r/{roomId}` → Room; `App.svelte` drops `/api/me` gate.
  - `ws.js`: backoff (1/2/4/8…cap 30s) + EMA offset kept; on (re)open send `JOIN_ROOM` from stored identity; typed helpers (`sendChat`, `sendMediaAction`, `sendCanvasDraw`, …); `connected` state exposed.
  - Identity in `sessionStorage` per-tab: `displayName`, `hostToken` keyed by roomId.
  - `sync.js` drift threshold 1.5s replacing 1s; `PositionAt` mirror stays byte-compatible with Go; tests updated.
  - `node --test web/src/lib/sync.test.js` green; `npm run build` succeeds with stubbed pages.
- **difficulty:** M
- **context pack:**
  - Paste: PLAN.md Chunk 8, current `web/src/lib/ws.js`, `web/src/lib/sync.js`, `web/src/lib/api.js`, `web/src/App.svelte`, `internal/live/watch.go` `PositionAt` (mirror source of truth).
  - Contract sections to obey: §4.2 client→server event names (helpers must emit them); §7.1/§7.3 (drift 1.5s, reconnect via `ROOM_STATE_SYNC`).
- **commit:** `refactor: client plumbing for ephemeral protocol (api, ws, routes, drift 1.5s)`
- **do NOT:** add router/state lib; touch `app.css` tokens; keep `upload.js` import; use `localStorage` for identity.

---

## 9. T09 — Shell: home, join gate, theater layout, offline overlay

- **id:** T09
- **title:** Home, join gate, collapsible theater layout, offline overlay
- **objective:** Build the app shell — create-room home, display-name gate, theater layout with collapsible side panel, and offline/reconnect overlay.
- **inputs:** ARCHITECTURE §5 hierarchy, §7.3 offline; PRD §4/§6; PLAN Chunk 9; T08 plumbing.
- **outputs:** new Home/JoinGate/SidePanel, rewritten Room page.
- **dependencies:** T08
- **files to create:** `web/src/pages/Home.svelte`, `web/src/components/JoinGate.svelte`, `web/src/components/SidePanel.svelte`
- **files to modify (rewrite):** `web/src/pages/Room.svelte`
- **acceptance criteria:**
  - Home: "Create room" → `POST /api/rooms` → store hostToken → nav `#/r/{id}` → copyable invite URL.
  - JoinGate: name prompt (2–32, trimmed, inline validation mirroring server regex) before WS connect for users without stored name.
  - Room layout §5: theater dominates; `SidePanel` collapsible (CSS grid + one `$state` bool, ≥44px toggle), holds Participants + Chat slots.
  - `connected === false` → dim UI, disable inputs, "Offline / Reconnecting…" banner.
  - Warm minimalist per PRD §4: rounded corners, `design.md` tokens only, Lucide icons, no emoji chrome.
  - Two-browser: create → share → guest names self → both listed; kill server → overlay; restart → auto-rejoin.
- **difficulty:** M
- **context pack:**
  - Paste: `ARCHITECTURE.md` §5 (component hierarchy) + §7.3, PLAN.md Chunk 9, `design.md` (tokens), post-T08 `ws.js`/`api.js`, current `web/src/App.svelte`.
  - Contract sections to obey: §5 layout tree; PRD §4/§6 (offline behavior, empty states).
- **commit:** `feat: home, join gate, collapsible theater layout, offline overlay`
- **do NOT:** build Login/Rooms/Admin; add motion ignoring `prefers-reduced-motion`; put chat logic here.

---

## 10. T10 — Participants + chat

- **id:** T10
- **title:** Participant list with host controls + chat retool
- **objective:** Render participant rows (role/status), host-only per-row controls, kicked terminal state; rewire chat to new events with ring-buffer history.
- **inputs:** ARCHITECTURE §4.2 `MANAGE_USER`/`CHAT_*`; PRD §3; PLAN Chunk 10; T09 shell, T08 helpers.
- **outputs:** new Participants component, edited Chat.
- **dependencies:** T09, T08
- **files to create:** `web/src/components/Participants.svelte`
- **files to modify:** `web/src/components/Chat.svelte`
- **acceptance criteria:**
  - Participant rows: name, host marker, status badge (WAITING/DOWNLOADING/FILE_READY/IN_SYNC) with token colors.
  - Host-only per row: "Promote to Host", "Remove from Room" → `MANAGE_USER`; hidden for guests; ≥44px targets.
  - On `KICKED`: terminal "Removed from room" state, no auto-reconnect.
  - Chat: `CHAT_MESSAGE`/`CHAT_BROADCAST`, temp display names, timestamps (unix seconds → local time), native-input emoji passthrough, history from `ROOM_STATE_SYNC.recentChat`.
  - Two-browser: promote swaps host badge live; kicked tab terminal + cannot rejoin; chat survives reconnect (ring replayed).
- **difficulty:** M
- **context pack:**
  - Paste: `ARCHITECTURE.md` §4.2 (`MANAGE_USER`,`CHAT_BROADCAST`,`USER_UPDATED`,`KICKED`), PRD §3, PLAN.md Chunk 10, current `web/src/components/Chat.svelte`, `design.md` (status token colors).
  - Contract sections to obey: §4.2 payload field names; time unit seconds for chat.
- **commit:** `feat: participant list with host controls + chat retool`
- **do NOT:** add emoji-picker dep; regex-sanitize (rely on Svelte escaping); paginate history.

---

## 11. T11 — Media layer: local playback

- **id:** T11
- **title:** Local-file player with download flow, mismatch warning, drift sync
- **objective:** Rewrite Player for the download-and-sync model: host catalog pick, local file load into `<video>`, echo-driven sync loop, mismatch toast, status reporting.
- **inputs:** ARCHITECTURE §7.1/§7.2 drift/mismatch; PRD §5/§6/§7; PLAN Chunk 11; T05 media protocol, T09 shell, T08 helpers.
- **outputs:** rewritten Player, Room edits.
- **dependencies:** T09, T08, T05
- **files to modify (rewrite):** `web/src/components/Player.svelte`; **edit:** `web/src/pages/Room.svelte`
- **acceptance criteria:**
  - Empty: no `mediaId` → "Waiting for the host to select media"; host also sees catalog (`GET /api/media`) → pick → `MEDIA_SELECT`.
  - Selected: "Download media" anchor (`/media/{id}`, `download`) → on click `UPDATE_STATUS DOWNLOADING`; `<input type="file">` → `URL.createObjectURL` into `<video>` → `UPDATE_STATUS FILE_READY`.
  - Mismatch: compare `file.name`/`file.size` vs catalog item → non-blocking toast, playback still allowed.
  - Sync loop: controls send `MEDIA_ACTION`; `<video>` mutates only on `MEDIA_SYNC`; 500ms loop — drift >1.5s hard seek (silent), >0.15s playbackRate nudge; report `IN_SYNC` within threshold while playing.
  - Native controls intercepted → forwarded as intents.
  - Two-browser: same file → play/pause/seek syncs either way; wrong file → toast but playable; injected drift self-heals in one tick.
- **difficulty:** L (most stateful component; echo-driven correctness)
- **context pack:**
  - Paste: `ARCHITECTURE.md` §7.1 (drift) + §7.2 (mismatch) + §4.2 (`MEDIA_*`,`UPDATE_STATUS`), PRD §5/§6/§7, PLAN.md Chunk 11, current `web/src/components/Player.svelte`, `web/src/lib/sync.js` (drift math — read only).
  - Contract sections to obey: §7.1 1.5s silent reseek; PRD §7 download-only (never stream server bytes into `<video>`).
- **commit:** `feat: local-file player with download flow, mismatch warning, drift sync`
- **do NOT:** stream from server into `<video>`; change `sync.js` math; auto-play before file load.

---

## 12. T12 — Canvas layer

- **id:** T12
- **title:** Collaborative canvas with 50ms batching, PNG export, host clear
- **objective:** Build the drawing surface: pure batching lib, optimistic local render, incoming stroke render, join replay, toolbar (width/color/export/host-clear).
- **inputs:** ARCHITECTURE §7.5, §4.2 canvas events; PRD §2.3/§3/§6; PLAN Chunk 12; T06 canvas protocol, T09 shell, T08 helpers.
- **outputs:** new Canvas component + pure `canvas.js` + tests; Room edit.
- **dependencies:** T09, T08, T06
- **files to create:** `web/src/components/Canvas.svelte`, `web/src/lib/canvas.js`, `web/src/lib/canvas.test.js`
- **files to modify:** `web/src/pages/Room.svelte`
- **acceptance criteria:**
  - `canvas.js` (node-testable): 50ms flush batching (`CANVAS_BATCH_RATE`), canvas-relative point normalization, 100-point batch split.
  - Canvas.svelte: pointer → optimistic render + buffer; flush → `sendCanvasDraw`; `CANVAS_UPDATE` → render others; `ROOM_STATE_SYNC.currentCanvas` replay on join/reconnect; transparent until first stroke.
  - Toolbar: brush width, palette from design tokens, Export → `toBlob('image/png')` + anchor download, host-only Clear → `CANVAS_CLEAR`; `CANVAS_CLEARED` wipes all.
  - Layered per theater layout; `prefers-reduced-motion` respected.
  - `node --test web/src/lib/canvas.test.js` green (batch split, fake-timer flush, normalization); two-browser draw mutual + near-instant local; export saves PNG; host clear wipes both.
- **difficulty:** M
- **context pack:**
  - Paste: `ARCHITECTURE.md` §7.5 + §4.2 (`CANVAS_DRAW`/`CANVAS_UPDATE`/`CANVAS_CLEAR`/`CANVAS_CLEARED`), PRD §2.3/§3/§6, PLAN.md Chunk 12, `design.md` (palette tokens), post-T08 `ws.js` (`sendCanvasDraw`).
  - Contract sections to obey: §7.5 client-side batching (one 50ms mechanism); §3.2 100-point cap.
- **commit:** `feat: collaborative canvas with 50ms batching, PNG export, host clear`
- **do NOT:** add rAF throttling beyond batcher; canvas libs; server smoothing; undo.

---

## 13. T13 — Delete the old world

- **id:** T13
- **title:** Remove auth, sqlite, upload pipeline — ephemeral in-memory only
- **objective:** Delete all superseded packages/files/service units, prune `go.mod` to one dep, finalize `main.go` wiring and docs.
- **inputs:** D7; PLAN Chunk 13; all backend + frontend replacements (T01–T12) in place.
- **outputs:** deletions + edited wiring/docs; full build proof.
- **dependencies:** T01, T02, T04, T05, T06, T07, T09, T10, T11, T12
- **files to delete:** `internal/auth/*`, `internal/db/*`, `internal/media/*`, `web/src/lib/upload.js`, `web/src/pages/{Login,Rooms,Admin}.svelte`, `deploy/backup.sh`, `deploy/together-backup.{service,timer}`
- **files to modify:** `cmd/server/main.go`, `go.mod`, `go.sum`, `README.md`, `CLAUDE.md`, `deploy/together.service`, `build.sh` (if paths changed)
- **acceptance criteria:**
  - `go mod tidy` → `go.mod` requires exactly `github.com/coder/websocket`.
  - `main.go` wiring: config → store → catalog → mux (`/api/rooms`, `/api/media`, `/media/`, `/ws/`, SPA fallback) → graceful shutdown closing all hubs.
  - `together.service`: DB env vars dropped, `STATIC_MEDIA_PATH` added, backup timer refs removed.
  - README + CLAUDE.md command sections updated (no ADMIN_USER/PASS, no ffmpeg, new env vars).
  - `go build ./...` green; `grep -r "modernc\|argon2\|x/crypto" go.mod internal cmd` empty.
  - `./build.sh` → binary serves SPA; `git restore cmd/server/webdist/index.html`; smoke on `:18080` — create/join/all four features; restart wipes rooms.
- **difficulty:** M (mechanical but wide; wiring + doc correctness)
- **context pack:**
  - Paste: PLAN.md Chunk 13 + D7, current `cmd/server/main.go`, `go.mod`, `deploy/together.service`, `build.sh`, `CLAUDE.md` (critical webdist hazard note).
  - Contract sections to obey: §6 dependency graph (single Go server); §8 env vars in service file.
- **commit:** `refactor: remove auth, sqlite, and upload pipeline — ephemeral in-memory only`
- **do NOT:** keep dead packages "for reference"; leave orphan env vars; commit built webdist.

---

## 14. T14 — End-to-end hardening

- **id:** T14
- **title:** Full-stack integration suite for ephemeral rooms, sync, canvas, roles
- **objective:** Add one real-stack integration test covering the full happy path + limits, run `-race` clean, finalize sync tests and the manual checklist.
- **inputs:** PLAN Chunk 14; the completed stack (T13).
- **outputs:** integration test + edited state/sync tests + progress checklist.
- **dependencies:** T13 (and transitively all)
- **files to create:** `internal/live/integration_test.go`
- **files to modify:** `internal/state/state_test.go`, `web/src/lib/sync.test.js`
- **acceptance criteria:**
  - Integration (httptest + real WS dials, no SQLite/ffmpeg): (1) create → host+2 guests join → state-sync shapes; (2) guest PLAY → all three converge on projected position; (3) canvas batch fan-out + late-join replay; (4) kick blocks rejoin, promote swaps role; (5) host drop → oldest inherits after injected short grace; (6) limits: 51st join rejected, 101-point batch dropped, 33-char name rejected.
  - `go test ./... -race` green.
  - `sync.test.js`: backward-clock clamp + 1.5s threshold both covered.
  - Final sweep: `gofmt -l internal cmd` empty; `npm run build`; `git restore cmd/server/webdist/index.html`; manual two-browser checklist appended to `.superpowers/sdd/progress.md`.
  - `go test ./... -race` + `node --test` both green in <30s.
- **difficulty:** M
- **context pack:**
  - Paste: PLAN.md Chunk 14, final `internal/live/hub.go` + `state.go`, `web/src/lib/sync.js` + `sync.test.js`, `ARCHITECTURE.md` §3.2 (limits under test) + §7.4 (migration under test).
  - Contract sections to obey: §3.2 caps (51 users, 100 points, 32-char name); §7.4 oldest-inherits.
- **commit:** `test: full-stack integration suite for ephemeral rooms, sync, canvas, roles`
- **do NOT:** add testify/framework; test Svelte headlessly; write per-function micro-suites over the scenario test.
