# ARCHITECTURE.md — Together V2

Living document — the current technical truth for the V2 refactor (ephemeral rooms + local playback). Sources: `specs/001-core/PRD.md`, `UX.md` (screen ids referenced throughout), `specs/001-core/SPEC.md` §9 resolved decisions. Supersedes the previous version of this file, which described a system that was never built.

## 1. System overview

One Go monolith (single static binary) behind Caddy (TLS termination) on a 2 vCPU / 2 GB VPS. SQLite (WAL) holds only durable data: accounts, sessions, invite codes, media library, subtitles, pipeline jobs. Everything room-shaped — rooms, presence, chat, playback state, guest sessions, invite tokens — lives **in server memory** and dies with the room or the process.

The server is a state authority, not a media streamer: during synced playback each client plays a **local file** (`blob:` objectURL) and exchanges only JSON over WebSocket. Media bytes leave the server only for (a) one-time authed downloads, (b) subtitle files, (c) the explicit opt-in streaming fallback.

Frontend: Svelte 5 (runes) SPA, Vite-built, embedded in the binary via `go:embed`, styled with shadcn-svelte on Tailwind v4, all tokens mapped once from `design.md` (NxCode) into `@theme` in `web/src/app.css`.

**Dependency budget (binding):** Go — exactly `github.com/coder/websocket`, `modernc.org/sqlite`, `golang.org/x/crypto`; everything else stdlib (routing = `http.ServeMux` method patterns, config = env vars, logging = `log`). Tests: stdlib `testing` only. Never live-transcode; ffmpeg runs only in the ingest job worker, one job at a time, `nice -n 19`.

## 2. Modules & boundaries

```
cmd/server/          main: mux wiring, embedded SPA + fallback, env config
internal/db/         Open(): schema DDL + V2 boot cutover (drops, column guard)
internal/auth/       argon2id, account sessions (SQLite), invite codes,
                     Require(db, adminOnly, h) — account-session middleware
internal/live/       THE room world (all in-memory):
                     watch.go   pure sync state machine (WatchState, Apply, PositionAt)
                     hub.go     rooms map, per-room mutex + recover, WS handling,
                                broadcast, chat ring, presence/status, empty-timer
                     rooms.go   room lifecycle: create/list/end/regenerate,
                                guest sessions + join, RequireRoom middleware
internal/media/      upload.go   resumable chunked admin upload
                     pipeline.go ffprobe/ffmpeg ingest worker (+ audio branch),
                                 crash-reclaim of stuck jobs
                     serve.go    library list, download/stream/subs (RequireRoom)
web/                 Svelte SPA (see §6)
```

Boundary rules:
- Only `internal/db` writes DDL. Only `internal/live` touches room state; nothing else may hold a pointer into a room. Only `internal/media/pipeline.go` invokes ffmpeg/ffprobe.
- `internal/api/rooms.go` (V1's DB-backed rooms handlers) is **deleted**; its route surface moves to `internal/live/rooms.go` because rooms are now hub state.
- `watch.go` stays pure (no I/O, no clock ownership — caller passes `now`). `web/src/lib/sync.js` is its line-for-line JS mirror; change both together, both stay tested.
- Two session systems, one gate: `auth.Require` (account, SQLite-backed) for account surfaces; `live.RequireRoom` (account **or** in-memory guest session scoped to the target room) for room surfaces only: `/ws/{id}`, media download/stream/subs, room media-meta.

## 3. Data model

### 3.1 Durable — SQLite DDL (post-cutover)

```sql
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY,
  username TEXT NOT NULL UNIQUE,
  pass_hash BLOB NOT NULL,
  salt BLOB NOT NULL,
  role TEXT NOT NULL DEFAULT 'member',            -- 'admin' | 'member'
  created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
CREATE TABLE IF NOT EXISTS sessions (
  token TEXT PRIMARY KEY,                          -- 128-bit crypto-random, hex
  user_id INTEGER NOT NULL REFERENCES users(id),
  expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS invite_codes (
  code TEXT PRIMARY KEY,
  created_by INTEGER NOT NULL,
  used_by INTEGER                                  -- NULL = unused; set transactionally with user insert
);
CREATE TABLE IF NOT EXISTS media (
  id INTEGER PRIMARY KEY,
  kind TEXT NOT NULL,                              -- 'video' | 'audio' (set from ffprobe at ingest)
  title TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'uploading',        -- uploading|processing|ready|failed
  file_path TEXT,
  orig_name TEXT,
  size_bytes INTEGER,                              -- exact bytes; the client-side match key (FR-11)
  duration REAL,
  error TEXT,
  created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
CREATE TABLE IF NOT EXISTS subtitles (
  id INTEGER PRIMARY KEY,
  media_id INTEGER NOT NULL REFERENCES media(id),
  label TEXT NOT NULL,
  file_path TEXT NOT NULL                          -- .vtt, converted at upload
);
CREATE TABLE IF NOT EXISTS jobs (
  id INTEGER PRIMARY KEY,
  media_id INTEGER NOT NULL REFERENCES media(id),
  status TEXT NOT NULL DEFAULT 'pending',          -- pending|running|done|failed
  error TEXT,
  created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
```

**V2 boot cutover** (runs after the idempotent schema, itself idempotent):

```sql
DROP TABLE IF EXISTS rooms;      -- V1 persistent rooms: not migrated (SPEC §9.11)
DROP TABLE IF EXISTS messages;   -- chat is in-memory now
DROP TABLE IF EXISTS activities; -- state_json checkpointing deleted (SPEC §9.2)
UPDATE media SET kind='video' WHERE kind='movie';  -- V1 vocabulary → V2 (SPEC §9.9)
UPDATE media SET kind='audio' WHERE kind='music';
```

`media.kind` already exists in the shipped V1 schema (no `ALTER`), but with values `movie|music`; the cutover normalizes them to `video|audio` and retires the `db.go` migration-note ponytail by being the first post-release schema change. No migration framework yet.

Indices: primary keys + `users.username` UNIQUE suffice at this scale (`idx_messages_room` dies with its table). `size_bytes` and `status` are scanned over a ≤hundreds-row library — no index warranted.

### 3.2 Ephemeral — hub memory (shapes are contract, not code)

```
Hub
  mu        sync.Mutex                  // guards rooms map + guestSessions map
  rooms     map[roomID]*Room
  guests    map[guestCookieToken]*GuestSession

Room                                    // all fields guarded by Room.mu
  id          string                    // opaque, crypto-random 16 hex chars
  name        string
  ownerID     int64                     // account user id; isHost := conn.userID == ownerID, computed live
  mediaID     int64                     // fixed at creation (M1)
  kind        string                    // 'video' | 'audio', copied from media row
  joinToken   string                    // ≥128-bit crypto-random hex; regenerate replaces it
  watch       WatchState                // position, rate, paused, updatedAt — the V1 machine, unchanged
  chat        [200]ChatMsg ring         // drop-oldest (FR-25)
  clients     set[*client]              // live WS connections; presence derives from this
  emptyTimer  *time.Timer               // armed when clients hits 0; Stop() on join; fire → teardown

GuestSession                            // dies at room teardown; survives socket drops (FR-5)
  guestID   string                      // stable identity for reconnect/no-re-suffix
  roomID    string
  name      string                      // post-suffix display name, fixed at join

client (per WS connection)
  userID    int64  | 0                  // account connections
  guestID   string | ""                 // guest connections
  name, status                          // status: downloading|file_ready|in_sync (client-reported)
```

Constraints enforced at the boundary: guest name 1–32 chars after control-char strip; ≤12 participants per room; suffixing checked against currently-connected names only; a reconnecting guestID keeps its name verbatim.

## 4. API contract

All JSON. Error body shape everywhere: `{"error": "human-readable message"}`. Auth column: **acct** = `auth.Require` (account session cookie `together_session`, HttpOnly, SameSite=Lax, Secure behind TLS), **admin** = same with role check, **room** = `live.RequireRoom` (account session **or** guest cookie `together_guest` whose session's room matches the target), **none** = public.

### 4.1 Auth & accounts (unchanged from V1)

| Method & path | Req → Resp | Codes | Auth |
|---|---|---|---|
| POST `/api/login` | `{username,password}` → `{id,username,role}` + session cookie | 200, 401 | none |
| POST `/api/register` | `{code,username,password}` → same as login | 200, 400 (bad code/name — code not burned), 409 taken | none |
| POST `/api/logout` | — → `{}` , clears cookie | 200 | acct |
| GET `/api/me` | → `{id,username,role}` | 200, 401 | acct |
| POST `/api/admin/invites` | `{}` → `{code}` | 200 | admin |
| GET `/api/admin/invites` | → `[{code}]` (unused only) | 200 | admin |

### 4.2 Library & admin media (V1 surface; pipeline gains audio branch)

| Method & path | Req → Resp | Codes | Auth |
|---|---|---|---|
| GET `/api/media` | → `[{id,kind,title,status,sizeBytes,duration,subtitles:[{id,label}]}]` | 200 | acct |
| POST `/api/admin/media` | `{title,origName,sizeBytes}` → `{id}` (upload session) | 200 | admin |
| GET `/api/admin/media/{id}/blob` | → `{offset}` (resume point) | 200, 404 | admin |
| PATCH `/api/admin/media/{id}/blob` | 8 MB chunk at `Upload-Offset` header → `{offset}` | 200, 409 offset mismatch | admin |
| POST `/api/admin/media/{id}/finish` | `{}` → `{}` (enqueues job) | 200 | admin |
| POST `/api/admin/media/{id}/subtitle` | multipart `.srt` + label → `{id}` | 200, 400 | admin |
| DELETE `/api/admin/media/{id}` | → `{}` (row, files, subs, jobs) | 200, 404 | admin |

Pipeline decision tree at `finish` (one worker, `nice -n 19`): probe → **no video stream** ⇒ audio branch: aac/m4a/mp3 → move as-is, else `-c:a aac` into `.m4a`; **video** ⇒ V1 tree unchanged (mp4/h264/aac move; mkv/h264/aac remux; h264+other-audio audio-only transcode; else libx264). `kind` set from the probe. No libx264 path for audio, ever.

### 4.3 Rooms (new surface — replaces V1 `internal/api/rooms.go`)

| Method & path | Req → Resp | Codes | Auth |
|---|---|---|---|
| GET `/api/rooms` | → `[{id,name,mediaId,mediaTitle,kind,participants}]` (live rooms from hub) | 200 | acct |
| POST `/api/rooms` | `{mediaId, name?}` → `{id, joinToken}`; name defaults to media title; media must be `ready`; creates room **and** starts its watch activity | 201, 400, 404 media | acct |
| DELETE `/api/rooms/{id}` | → `{}` — immediate teardown, broadcasts `room_closed` | 200, 403 not host, 404 | acct (host check inside) |
| POST `/api/rooms/{id}/token` | `{}` → `{joinToken}` — regenerate; old token dead for new joins, connected guests persist | 200, 403, 404 | acct (host) |
| POST `/api/rooms/join` | `{token, name}` → `{roomId}` + guest cookie (`together_guest`, HttpOnly, session-scoped). Re-join with a live guest cookie for that room skips name minting and returns the same identity. | 200; 404 dead/unknown token (same body as unknown room — no oracle); 400 bad name; 409 room full | **none — public (with the peek route below, the only unauthenticated surface besides login/register)** |
| GET `/api/rooms/{id}/meta` | → `{name, kind, media:{id,title,sizeBytes,duration}, subtitles:[{id,label}]}` — serves the acquisition panel's size check (FR-11); S6's pre-join room name comes from the peek route below | 200, 404 | room |
| GET `/api/rooms/join/{token}` | → `{roomName}` (pre-join peek for S6 header) | 200, 404 | none |

Room ids and join tokens come from the same crypto-random generator as session tokens. Unguessability is the rate-limiter (SPEC §9.11); no other throttling.

### 4.4 Media bytes (auth widened from V1 `acct` to `room`)

| Method & path | Behavior | Codes | Auth |
|---|---|---|---|
| GET `/media/{id}/download` | `http.ServeFile` + `Content-Disposition: attachment` (Range/resume free) — serves FR-10 | 200/206, 404 | room |
| GET `/media/{id}/stream` | `http.ServeFile`, inline — the M4 opt-in fallback only | 200/206, 404 | room |
| GET `/media/{id}/subs/{sid}` | `.vtt` bytes for `<track>` — guest cookie rides the same-origin fetch | 200, 404 | room |

`RequireRoom` for guests additionally checks `{id}` == their room's `mediaID` — a guest session grants exactly one room's media, nothing else in the library.

### 4.5 WebSocket `GET /ws/{roomId}` (auth: room)

JSON frames. Deltas from V1 marked ●.

**Inbound:**

| Type | Payload | Notes |
|---|---|---|
| `chat` | `{body}` | ≤2000 chars, non-empty |
| `intent` | `{action:"play"\|"pause"\|"seek", position?}` | any participant; applied via `watch.Apply` under room mutex |
| `start` / `end` | `{mediaId}` / `{}` | unchanged V1 semantics (room creation already starts the activity; kept for restart) |
| `ping` | `{t}` | every 10 s |
| ● `status` | `{state:"downloading"\|"file_ready"\|"in_sync"}` | presence-only; never enters the `intent`/`watch.go` path |

**Outbound:**

| Type | Payload | Notes |
|---|---|---|
| ● `hello` | `{you:{name,isHost,isGuest}, users:[Presence], activity, chat:[ChatMsg], room:{name,kind,mediaId}}` | full stateless recovery on join/reconnect (FR-20); no event replay |
| ● `presence` | `{users:[{name,isHost,isGuest,status}]}` | server-owned; broadcast on join/leave/status change |
| ● `chat` | `{name, isGuest, body, createdAt}` | `createdAt` unix **seconds**; neutral `name` replaces V1 `username` |
| `activity` | `{id,type:"watch",state:{position,rate,paused,updatedAt}}` or `null` | absolute state; clients project via `PositionAt` |
| `pong` | `{t, serverTime}` | `serverTime` unix **ms** — EMA clock offset in `ws.js`; do not mix with chat seconds |
| `error` | `{body}` | |
| ● `room_closed` | `{}` | broadcast at teardown; client renders V1 Room Closed view, then socket closes |

## 5. Room lifecycle & concurrency

- One goroutine-safe hub; per-room mutex serializes everything inside a room (V1 discipline, kept).
- **Per-room `recover()`** in every hub goroutine that touches a room — load-bearing (NFR-7): with checkpointing deleted, a panicking room must log, tear itself down, and leave the process serving.
- Empty detection: zero open WS connections (a cookie'd-but-disconnected guest does not keep it warm). Last close → `emptyTimer.Reset(idle)`; any join → `Stop()`; fire → teardown.
- Teardown (host end, timer fire, or panic path — one code path, under hub lock): broadcast `room_closed` → close sockets → cancel timer → delete room from map → drop its guest sessions → token forgotten. Nothing touches SQLite.
- Server crash: rooms/chat/guests vanish (accepted, NFR-5); boot reclaims stuck `running` jobs (V1 behavior, kept) and runs the §3.1 cutover idempotently.

## 6. Frontend component hierarchy (mapped to UX.md ids)

Stack: Svelte 5 runes, hash router (`router.svelte.js`, still no router dep), shadcn-svelte components skinned to NxCode tokens via `@theme` in `app.css` — V1's `.btn-primary`/`.input`/`.card` CSS primitives are deleted. No raw hex in components; lucide icons; no emoji in chrome.

```
App.svelte — route switch; #/join/{token} renders BEFORE the /api/me gate
├─ JoinGuest.svelte ......................... S6  (name form; terminal invalid/full states)
├─ Login.svelte ............................. S1
├─ Register.svelte .......................... S2
├─ Home.svelte .............................. S3  (live rooms list, header, admin link)
│   └─ MediaPickerDialog.svelte ............. M1  (shadcn Dialog; ready items only)
├─ Room.svelte .............................. S4/S5 shell (WS owner; hello/presence/chat state)
│   ├─ RoomStrip.svelte                          (leave, title, host badge)
│   │   └─ RoomMenu.svelte                       (DropdownMenu: copy link · regenerate · end)
│   │       ├─ EndRoomDialog.svelte ......... M2
│   │       └─ RegenerateLinkDialog.svelte .. M3
│   ├─ AcquisitionPanel.svelte .............. UX §3.4 (download / load-copy / size-mismatch state)
│   │   └─ PlayFromServerDialog.svelte ...... M4
│   ├─ Player.svelte ........................ S4 video (echo-driven; blob: src; arm overlay; drift loop)
│   ├─ AudioPlayer.svelte ................... S5 now-playing (same intent/echo contract, kind-switched)
│   ├─ RoomClosed.svelte .................... V1  (terminal; Back-to-rooms for accounts only)
│   └─ SidePanel.svelte                          (collapsible; open by default when kind=audio)
│       ├─ Participants.svelte                   (dot ladder + Tooltip)
│       └─ Chat.svelte                           (ring-buffer history from hello)
└─ Admin.svelte ............................. S7  (upload card, library table + kind column, invites)

web/src/lib/
  ws.js        connect/reconnect (exponential backoff), EMA clock offset, message bus
  sync.js      PositionAt mirror of watch.go (backward-clock clamp) — tested, change in lockstep
  localfile.js file picker → size check vs /api/rooms/{id}/meta → createObjectURL (FR-11)
  api.js       fetch wrappers; upload.js resumable-upload client (localStorage continuation)
  router.svelte.js  hash router
```

Player contract (unchanged V1 core): user actions send intents only; the `<video>`/`<audio>` element mutates only on broadcast state; 500 ms loop drift-corrects (>1 s hard seek, >0.15 s playbackRate nudge); `crossorigin` dropped (inert on `blob:`); subtitles stay server-loaded `<track>` elements.

## 7. Dependency graph

```
Browsers ── HTTPS ──> Caddy (TLS) ──> together binary (:8080)
                                        ├─ net/http ServeMux ── go:embed SPA (+ SPA fallback)
                                        ├─ internal/auth ──────────┐
                                        ├─ internal/live (hub ← watch) ── in-memory rooms
                                        ├─ internal/media (serve / upload / pipeline ── exec ffmpeg,ffprobe)
                                        └─ internal/db ── modernc.org/sqlite ── data/together.db (WAL)
                                                                   └── restic nightly DB backup (systemd timer)
Go deps: coder/websocket · modernc.org/sqlite · x/crypto      (exactly these)
JS deps: svelte 5 · vite · tailwind v4 · shadcn-svelte · lucide-svelte (accepted V2 ceiling)
```

Module direction: `cmd/server` → {auth, live, media, db}; `live` → {auth (session lookup), db (media row reads)}; `media` → {auth, db}; `watch.go` → nothing. No cycles; `db` imports nothing internal.

## 8. Error handling strategy

- **HTTP:** correct status + `{"error": msg}`; never a stack trace. 401 → SPA routes to S1 (except `#/join/*`). Join-token probes get an undifferentiated 404 (no live-room oracle).
- **WS:** malformed frame → `error` frame, connection kept; oversize/invalid chat or name → refused with `error`; socket write failure → drop that client only, presence updates.
- **Room panic:** per-room recover → log with room id → teardown that room → process lives (NFR-7).
- **Client sync:** drift bands per FR-18; backward `serverTime` clamped in both `PositionAt` implementations; reconnect = exponential backoff (1 s… cap 30 s) then full `hello` restore — no client-side event queue.
- **Acquisition:** size mismatch is an inline blocked state (never a modal, never auto-stream); streaming happens only through M4 confirmation.
- **Pipeline:** job failure → `media.status='failed'` + `error` shown in S7; boot reclaims `running` jobs; delete is the recovery path.
- **Uploads:** offset mismatch → 409 with server offset; client resumes from it (localStorage continuation keyed on filename+size).

## 9. Configuration strategy

Env vars only, read once in `main` (no config files, no flags, no Viper):

| Var | Default | Purpose |
|---|---|---|
| `TOGETHER_ADDR` | `:8080` | listen address |
| `TOGETHER_DATA` | `./data` | SQLite file + media dirs |
| `ADMIN_USER` / `ADMIN_PASS` | — | seed admin, first boot only |
| `TOGETHER_ROOM_IDLE` | `30m` | empty-room close delay (Go duration; overridable for tests — AC-5.3) |

Compile-time constants (not config): room cap 12, chat ring 200, chat max 2000 chars, guest name max 32, room name max 64, chunk size 8 MB, ping 10 s, drift bands 1 s / 0.15 s, backoff cap 30 s.

## 10. Kernel-journey traceability (UX F1 → contract)

| F1 step | Serving contract |
|---|---|
| 1. Host signs in → S3 | `POST /api/login`, `GET /api/me`, `GET /api/rooms`, `GET /api/media` (M1 list) |
| 2. Create room, pick media | `POST /api/rooms {mediaId,name?}` → `{id,joinToken}`; auto-started activity |
| 3. Copy invite link | client-side from `joinToken` (`#/join/{token}`); regenerate = `POST /api/rooms/{id}/token` |
| 4. Guest opens link, names self, joins | `GET /api/rooms/join/{token}` (S6 room name) → `POST /api/rooms/join {token,name}` → guest cookie → `GET /ws/{roomId}` → `hello` |
| 5. Download media, size check, File Ready | `GET /api/rooms/{id}/meta` (sizeBytes) → `GET /media/{id}/download` (room auth) → local pick → WS `status:file_ready` → `presence` broadcast |
| 6. Arm on gesture | client-only (arm overlay) → WS `status:in_sync` |
| 7. Host plays → both in sync | WS `intent:play` → `watch.Apply` → `activity` broadcast → echo-driven players |
| 8. Guest scrubs → both jump | WS `intent:seek{position}` → same broadcast path (guests un-gated, FR-16) |
| 9. Drop → reopen link → mid-scene restore | guest cookie survives → `POST /api/rooms/join` (same identity) or direct `GET /ws/{roomId}` → `hello{activity,chat}` → `PositionAt` projection |
| 10. Chat about the ending | WS `chat` in → `chat` broadcast; ring buffer feeds later `hello`s |
| 11. Host ends room | `DELETE /api/rooms/{id}` (or WS-adjacent host UI → same endpoint) → teardown → `room_closed` broadcast → V1 view; library/accounts persist (nothing to do — they were never room state) |

Every F2–F4 step reduces to the same contract rows (F2: steps 5–6; F3: identical with `kind=audio`; F4: `isHost` recomputed live on rejoin, no transfer endpoint exists by design).
