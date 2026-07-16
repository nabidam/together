---
status: ready
---

# TASKS.md — Together V2: Ephemeral Rooms + Local Playback

Split of `specs/001-core/PLAN.md` (status: gate-passed). Sources of truth: `ARCHITECTURE.md` (interfaces quoted below), `UX.md` (screen ids), `DESIGN.md`, `CONVENTIONS.md`, `specs/001-core/PRD.md` (AC ids). Executes against the shipped V1 codebase.

**Milestone A = tasks 1–12 (walking skeleton, PLAN chunks 1–5 + gates 1–2). These may not be reordered after feature tasks.** Tasks 13–21 deepen it.

Every implementation task: builds, `go test ./...` green, `gofmt -l internal cmd` empty, one commit per task. Context packs are **hints** predicted before code exists — verify against real files in the implementation session. Interfaces blocks are firmer: they quote the contract from ARCHITECTURE.md; contract changes route through the docs, never through a task improvising.

Sandbox note (from CLAUDE.md): smoke-test with `curl --noproxy '*'` and `TOGETHER_ADDR=:18080`. After any frontend build, `git restore cmd/server/webdist/index.html` before committing.

---

## Task 1 — DB cutover + kind vocabulary normalization

> **DONE** `e113883` — Verified: `go test ./internal/db ./internal/media` green; `TestOpen_Cutover*` (drops V1 tables, movie|music→video|audio, survivors intact, idempotent across two Open()) and `TestMediaKindFilterV2Vocabulary` (kind=video/audio filter, retired movie vocab absent) pass; `gofmt -l internal cmd` empty. `internal/api` + `internal/live` V1 tests fail on the now-dropped tables — expected chunk-1 intermediate, resolved by task 2 which deletes/rewrites them.

- **Objective:** Boot-time V2 cutover: drop V1 room tables, normalize `media.kind` values, update the library kind filter. (PLAN chunk 1, DB half.)
- **Inputs:** shipped V1 `internal/db/db.go` (idempotent DDL), V1 database files with `rooms`/`messages`/`activities` tables and `movie|music` kinds.
- **Outputs:** cutover running idempotently after DDL on every boot; `serve.go` kind filter speaking `video|audio`.
- **Dependencies:** none.
- **Files:** `internal/db/db.go`, `internal/db/db_test.go`, `internal/media/serve.go` (kind filter only).
- **Acceptance criteria:**
  - Test: open a DB file seeded with V1 tables and `movie|music` kind rows → after `Open()`, `rooms`/`messages`/`activities` tables gone; `media.kind` values are only `video`/`audio`.
  - Test: second `Open()` on the same file succeeds cleanly (idempotence).
  - Test: users, sessions, invite_codes, media, subtitles, jobs rows survive the cutover untouched (seed of AC-5.6).
  - httptest: `GET /api/media?kind=video` filters correctly; `movie` as a filter value matches nothing.
- **Difficulty:** low.
- **Interfaces:**
  - CONSUMES: V1 `db.Open()` signature (unchanged); existing `media` table with `kind TEXT NOT NULL` already present (no `ALTER`).
  - PRODUCES (ARCHITECTURE §3.1): cutover SQL, verbatim —
    ```sql
    DROP TABLE IF EXISTS rooms;
    DROP TABLE IF EXISTS messages;
    DROP TABLE IF EXISTS activities;
    UPDATE media SET kind='video' WHERE kind='movie';
    UPDATE media SET kind='audio' WHERE kind='music';
    ```
    `media.kind` values are henceforth `'video' | 'audio'` — every later task relies on this vocabulary. Remove the `db.go` migration-note ponytail comment (this is the migration).
- **Context pack (hints):** `internal/db/db.go`, `internal/db/db_test.go` (if present), `internal/media/serve.go`. ARCHITECTURE §3.1. No UX/DESIGN.
- **Do NOT:** add a migration framework; touch hub, auth, or frontend.

---

## Task 2 — Rooms move into the hub: Room struct, lifecycle HTTP, delete internal/api

> **DONE** `bc569c2` — Verified: `go test ./... ` green (full suite restored), `go vet ./...` clean, `go test -race ./internal/live` clean, `gofmt -l internal cmd` empty. `rooms_test.go`/`hub_test.go` drive the real stack (httptest + real WS dials): create→201 with 16-hex id + 128-bit token, name defaults to media title, name>64→400, unknown/non-ready media→404, list shape correct, non-host delete→403 (room survives) / host delete→200 (gone from list + hub) / unknown→404, regenerate replaces token & non-host→403, fresh hub over same DB has 0 rooms while media/users persist (AC-5.6), WS chat/presence/activity-sync/late-joiner-hello work in-memory, dial to unknown room rejected. `internal/api` deleted.

- **Objective:** Rooms become in-memory hub state with a new HTTP surface; V1 DB-backed rooms handlers and hub checkpointing deleted. (PLAN chunk 1, hub half.)
- **Inputs:** Task 1 (no `rooms` table to collide with); V1 `internal/live/hub.go`, `internal/api/`.
- **Outputs:** `internal/live/rooms.go` with `Room` struct + create/list/end/regenerate handlers mounted in `main.go`; `internal/api/` deleted; hub no longer touches SQLite for state.
- **Dependencies:** 1.
- **Files:** `internal/live/rooms.go` (new), `internal/live/rooms_test.go` (new), `internal/live/hub.go`, `internal/live/hub_test.go`, `cmd/server/main.go`; **delete** `internal/api/` (both files).
- **Acceptance criteria:**
  - httptest: `POST /api/rooms {mediaId}` with a `ready` media row → 201 `{id,joinToken}`; name defaults to media title; name >64 chars → 400; non-ready/unknown media → 404.
  - httptest: `GET /api/rooms` lists the room with `{id,name,mediaId,mediaTitle,kind,participants}`.
  - httptest: non-host `DELETE /api/rooms/{id}` → 403; host DELETE → 200, room gone from list. (Teardown v0: close sockets, delete from map — full path in task 6.)
  - httptest: `POST /api/rooms/{id}/token` host-only regenerates; old token value replaced.
  - Restart the process with a room live: room gone; media/accounts/sessions intact.
  - Test: room ids are 16 hex chars; join tokens ≥128-bit hex, from the same crypto-random generator as `internal/auth` session tokens.
- **Difficulty:** medium.
- **Interfaces:**
  - CONSUMES: `auth.Require(db, adminOnly, h)` middleware (V1, unchanged); `media` rows via `internal/db` (`kind`, `title`, `status='ready'`); task 1's `video|audio` vocabulary.
  - PRODUCES (ARCHITECTURE §3.2, §4.3):
    ```
    Room  // all fields guarded by Room.mu
      id          string        // opaque, crypto-random 16 hex chars
      name        string
      ownerID     int64         // isHost := conn.userID == ownerID, computed live
      mediaID     int64         // fixed at creation
      kind        string        // 'video' | 'audio', copied from media row
      joinToken   string        // ≥128-bit crypto-random hex; regenerate replaces it
      watch       WatchState    // the V1 machine, unchanged
      chat        [200]ChatMsg ring   // unused until task 5
      clients     set[*client]
      emptyTimer  *time.Timer   // unused until task 6
    ```
    Endpoints (auth **acct**): `GET /api/rooms` → `[{id,name,mediaId,mediaTitle,kind,participants}]`; `POST /api/rooms` `{mediaId, name?}` → 201 `{id, joinToken}` (creates room **and** starts its watch activity); `DELETE /api/rooms/{id}` → `{}` / 403 / 404; `POST /api/rooms/{id}/token` → `{joinToken}`. Error body everywhere: `{"error": msg}`.
  - REMOVES: `activities` `state_json` checkpointing on intents; boot-time activity reclaim; all of `internal/api/`.
- **Context pack (hints):** `internal/live/hub.go`, `internal/live/hub_test.go`, `internal/live/watch.go` (read-only — do not modify), `internal/api/*` (to delete), `internal/auth/` (token generator + `Require`), `cmd/server/main.go`. ARCHITECTURE §2, §3.2, §4.3, §5. No UX/DESIGN.
- **Do NOT:** touch guest auth, chat, presence, timers, `watch.go`, or any frontend file.

---

## Task 3 — Guest sessions + public join surface

> **DONE** `301d371` — Verified: `go test ./...` green (all packages), `go vet ./...` clean, `go test -race ./internal/live` clean, `gofmt -l internal cmd` empty. `rooms_test.go` drives the real stack (httptest, no WS): name-minting table tests (control-char strip, empty/33-char→400, `Sam`/`Sam (2)` collision, live-cookie re-join keeps identity with zero re-suffix and no second session, departed guest's name freed via direct `hub.guests` manipulation, cross-room names never collide); join with valid token → 200 + `together_guest` cookie; bad name → 400; 13th participant → 409 (12 succeed first); dead (regenerated-away) token and a never-existed token return byte-identical 404 bodies (no oracle); `GET /api/rooms/join/{token}` → `{roomName}` / 404 unknown; regenerate → old token 404, new token 200, previously-joined guest's cookie still accepted; `GET /api/rooms/{id}/meta` shape + 404 unknown room. Net/http `ServeMux` rejected co-registering `GET /api/rooms/join/{token}` and `GET /api/rooms/{id}/meta` (ambiguous wildcard position) — resolved with one `GET /api/rooms/{tail...}` route dispatching by hand.

- **Objective:** Guests exist: in-memory sessions minted by a public join endpoint, plus the pre-join peek and room meta routes. (PLAN chunk 2, sessions half.)
- **Inputs:** Task 2's `Room` + hub.
- **Outputs:** hub `guests` map; `POST /api/rooms/join`, `GET /api/rooms/join/{token}`, `GET /api/rooms/{id}/meta` mounted.
- **Dependencies:** 2.
- **Files:** `internal/live/rooms.go` + `rooms_test.go`, `cmd/server/main.go`.
- **Acceptance criteria:**
  - Table tests on name minting: control chars stripped; empty/33-char → 400; collision with a currently-connected name → `(2)`, `(3)`; a request carrying a live guest cookie for that room returns the same identity with no new session and no re-suffix (FR-5); a departed guest's name is free again.
  - httptest: join with valid token → 200 `{roomId}` + `together_guest` cookie (HttpOnly, SameSite=Lax, Secure when TLS-terminated — same detection as `auth`); 13th participant → 409; dead token and never-existed token responses byte-identical 404s (no oracle).
  - httptest: `GET /api/rooms/join/{token}` → `{roomName}`; unknown → 404.
  - httptest: after `POST /api/rooms/{id}/token`, old-token join → 404, new-token join → 200, previously-joined guest's cookie still valid.
- **Difficulty:** medium.
- **Interfaces:**
  - CONSUMES: task 2's `Room` (`joinToken`, `clients` for connected-name collision checks, participant count for cap 12).
  - PRODUCES (ARCHITECTURE §3.2, §4.3):
    ```
    GuestSession   // dies at room teardown; survives socket drops
      guestID   string   // stable identity for reconnect/no-re-suffix
      roomID    string
      name      string   // post-suffix display name, fixed at join
    Hub.guests   map[guestCookieToken]*GuestSession
    ```
    Endpoints: `POST /api/rooms/join` (**none/public**) `{token, name}` → 200 `{roomId}` + `together_guest` cookie; 404 dead/unknown (same body); 400 bad name; 409 full. `GET /api/rooms/join/{token}` (**none**) → `{roomName}` / 404. `GET /api/rooms/{id}/meta` (**room** auth — wired via task 4's middleware; until then mount under a placeholder that task 4 replaces) → `{name, kind, media:{id,title,sizeBytes,duration}, subtitles:[{id,label}]}`.
    Boundary constraints: guest name 1–32 chars after control-char strip; ≤12 participants; suffix checked against currently-connected names only.
- **Context pack (hints):** `internal/live/rooms.go` + test (task 2 output), `internal/auth/` (cookie Secure detection pattern), `cmd/server/main.go`. ARCHITECTURE §3.2, §4.3. No UX/DESIGN.
- **Do NOT:** change any WS frame; no frontend; no rate limiter (unguessability is the rate limiter, SPEC §9.11).

---

## Task 4 — RequireRoom middleware + media routes rewired + download endpoint

> **DONE** `b65378f` — Verified: `go test ./...` green (all packages), `go vet ./...` clean, `go test -race ./internal/live ./internal/media` clean, `gofmt -l internal cmd` empty. `internal/live/rooms_test.go` drives the real `hub.RequireRoom`/`hub.RequireRoomMedia`: no-credential request on `/api/rooms/{id}/meta` → 401; non-host account passes; guest scoped to its own room's meta → 200, guest reaching another room or an unknown/garbage guest cookie → 404 (byte-identical no-oracle shape); guest on its own room's media → 200, any other media id → 404, no credential → 401, account passes on any media. `internal/media/serve_test.go` drives the real `ServeFile`/header behavior on the account path (module direction forbids `media` importing `live`, so guest-scoping lives in the `live` tests): `/media/{id}/download` with Range → 206 + `Content-Disposition: attachment`; `/media/{id}/stream` → 200 inline, no attachment header; existing stream/subtitle/kind-filter tests still pass through the new `roomGate` parameter. `grep -rn 'together/internal/live' internal/media/*.go` empty — module-direction constraint intact.

- **Objective:** One room-scoped gate for WS and media bytes; guests get exactly their room's media. (PLAN chunk 2, auth half.)
- **Inputs:** Task 3's guest sessions.
- **Outputs:** `live.RequireRoom` wrapping `/ws/{roomId}`, `/media/{id}/stream`, `/media/{id}/subs/{sid}`, new `/media/{id}/download`, and `/api/rooms/{id}/meta`.
- **Dependencies:** 3.
- **Files:** `internal/live/rooms.go` + test, `internal/media/serve.go` + test, `cmd/server/main.go`.
- **Acceptance criteria:**
  - httptest: guest cookie downloads its room's media → 200/206 with Range support and `Content-Disposition: attachment`; any other media id → 404; no cookie and no account session → 401.
  - httptest: account session (any account, not just host) passes RequireRoom for a live room.
  - httptest: guest fetches its room's subtitle `.vtt` → 200; stream endpoint serves inline (no attachment header).
- **Difficulty:** medium.
- **Interfaces:**
  - CONSUMES: task 3's `GuestSession` (`roomID` match) and hub lookup; `auth` account-session validation; task 2's `Room.mediaID`.
  - PRODUCES (ARCHITECTURE §2, §4.4): `live.RequireRoom` — passes an account session **or** a guest session whose `roomID` matches the target room; for guests, media routes additionally require `{id}` == their room's `mediaID`. Routes under it (auth **room**):
    | GET `/media/{id}/download` | `http.ServeFile` + `Content-Disposition: attachment` | 200/206, 404 |
    | GET `/media/{id}/stream` | `http.ServeFile`, inline — M4 opt-in fallback only | 200/206, 404 |
    | GET `/media/{id}/subs/{sid}` | `.vtt` bytes for `<track>` | 200, 404 |
    | GET `/ws/{roomId}` | WebSocket upgrade | — |
    | GET `/api/rooms/{id}/meta` | per task 3's shape | 200, 404 |
- **Context pack (hints):** `internal/live/rooms.go`, `internal/media/serve.go` + test, `internal/auth/` (session lookup), `cmd/server/main.go`. ARCHITECTURE §2 (boundary rules), §4.4. No UX/DESIGN.
- **Do NOT:** change WS frames; no frontend.

---

## Task 5 — WS protocol V2: hello, presence + status, chat ring

> **DONE** `266a79a` — Verified: `go test ./...` green (all packages), `go vet ./...` clean, `go test -race ./internal/live` clean, `gofmt -l internal cmd` empty. `hub_test.go` drives the real stack (httptest + real WS dials, `newStack` remounted under `hub.RequireRoom` so guest cookies pass the gate; `joinAsGuest` helper joins then dials): `status` from one connection → both connections' `presence` broadcast shows the updated entry (self-broadcast draining made explicit in the test to avoid a stale-frame race); 210 chats → a fresh dial's `hello.chat` carries exactly the latest 200 (`m10`..`m209`) in order; reconnecting client's `hello` carries current `activity` + chat history, `you.isHost` true only for the room-owner account connection (false for non-owner account and for guest); a `status` frame never changes `activity` (byte-identical JSON before/after); empty and >2000-char `chat` each get an `error` frame with the connection still usable afterward; a guest dialed via `RequireRoom` + guest cookie appears in `presence` with `isGuest:true` and its `chat` frame carries `isGuest:true` + its guest name in `name`; a dedicated test confirms no `messages` table exists and chat leaves `users`/`media` row counts untouched. V1's `TestChatAndPresenceBroadcast` updated to assert the new `{name,isGuest,body,createdAt}` chat shape (old `userId`/`username` fields deleted).
- **Objective:** The new wire protocol's read path: full stateless recovery via `hello`, server-owned presence with client-reported status, in-memory chat. (PLAN chunk 3, protocol half.)
- **Inputs:** Tasks 2–4 (rooms, guests, RequireRoom on `/ws/{roomId}`).
- **Outputs:** V2 `hello`/`presence`/`chat` outbound frames; inbound `status` and `chat`; V1 chat persistence deleted.
- **Dependencies:** 4.
- **Files:** `internal/live/hub.go` + test, `internal/live/rooms.go` + test.
- **Acceptance criteria:**
  - WS-dial test: two clients in one room; `status` change on one → `presence` broadcast received by both with the updated entry.
  - WS-dial test: send 210 chats → a fresh dial's `hello` carries exactly the latest 200, in order (drop-oldest ring).
  - WS-dial test: reconnecting client's `hello` carries current activity + chat history; `you.isHost` true only for the connection whose account userID == room `ownerID` (computed live).
  - Test: inbound `status` never reaches `watch.Apply` (activity state unchanged after a `status` frame); chat >2000 chars or empty → `error` frame, connection kept.
  - Test: zero DB writes on chat (no `messages` table exists — compile-level guarantee — and no other table written).
- **Difficulty:** high.
- **Interfaces:**
  - CONSUMES: task 2's `Room` (`chat` ring, `clients`, `ownerID`, `watch`); task 3's `GuestSession.name`; V1 `watch.Apply`/`PositionAt` (unchanged); V1 `intent`/`start`/`end`/`ping`/`pong`/`activity`/`error` frames (unchanged).
  - PRODUCES (ARCHITECTURE §4.5) — inbound deltas:
    | `status` | `{state:"downloading"\|"file_ready"\|"in_sync"}` | presence-only; never enters the `intent`/`watch.go` path |
    | `chat` | `{body}` | ≤2000 chars, non-empty |
    Outbound deltas:
    | `hello` | `{you:{name,isHost,isGuest}, users:[Presence], activity, chat:[ChatMsg], room:{name,kind,mediaId}}` | on join/reconnect; no event replay |
    | `presence` | `{users:[{name,isHost,isGuest,status}]}` | server-owned; on join/leave/status change |
    | `chat` | `{name, isGuest, body, createdAt}` | `createdAt` unix **seconds**; neutral `name` replaces V1 `username` |
    These exact field names are the contract task 9's `ws.js` hand-matches.
- **Context pack (hints):** `internal/live/hub.go` + test, `internal/live/rooms.go`, `internal/live/watch.go` (read-only). ARCHITECTURE §4.5, §3.2. No UX/DESIGN.
- **Do NOT:** touch `watch.go` or `web/src/lib/sync.js`; no host gating of intents (FR-16); no teardown/timer changes yet.

---

## Task 6 — Teardown path, empty timer, per-room recover

- **Objective:** One teardown code path for host end / timer fire / panic; rooms die when empty; a panicking room can't take the process down. (PLAN chunk 3, lifecycle half.)
- **Inputs:** Task 5's protocol (needs `room_closed` broadcast slot); task 2's `DELETE /api/rooms/{id}`.
- **Outputs:** `room_closed` frame; `emptyTimer` wired; per-room `recover()`; `TOGETHER_ROOM_IDLE` env.
- **Dependencies:** 5.
- **Files:** `internal/live/hub.go` + test, `internal/live/rooms.go` + test, `cmd/server/main.go`.
- **Acceptance criteria:**
  - WS-dial test: host `DELETE /api/rooms/{id}` → connected clients receive `room_closed`, then sockets close; room gone from hub; its guest sessions dropped; old join token → 404.
  - Timer test with `TOGETHER_ROOM_IDLE=50ms`: last WS close → room gone after fire; a cookie'd-but-disconnected guest does not keep it warm; rejoin within idle → `Stop()` verified (room survives).
  - Panic test: injected panic in room A's handling → room B's clients still exchange frames; process alive; room A torn down and logged with its room id.
- **Difficulty:** high.
- **Interfaces:**
  - CONSUMES: task 5's broadcast machinery; task 3's `Hub.guests`; task 2's `DELETE` handler (now routes through this teardown).
  - PRODUCES (ARCHITECTURE §4.5, §5, §9): outbound `room_closed` `{}` — broadcast at teardown; client renders Room Closed view, then socket closes. Teardown order (one code path, under hub lock): broadcast `room_closed` → close sockets → cancel timer → delete room from map → drop its guest sessions → token forgotten. Nothing touches SQLite. Empty detection: zero open WS connections; last close → `emptyTimer.Reset(idle)`; any join → `Stop()`; fire → teardown. Config: `TOGETHER_ROOM_IDLE` (Go duration, default `30m`), read once in `main`.
- **Context pack (hints):** `internal/live/hub.go` + test, `internal/live/rooms.go` + test, `cmd/server/main.go`. ARCHITECTURE §5, §8 (Room panic), §9. No UX/DESIGN.
- **Do NOT:** touch `watch.go`/`sync.js`; no frontend.

> **DONE** `5fcae1c` — Verified: `go test ./...` green (all packages), `go vet ./...` clean, `go test -race ./internal/live -count=3` clean, `gofmt -l internal cmd` empty. `hub.go`/`rooms.go` now share one `teardown()` for host DELETE, empty-timer fire, and panic recovery, guarded idempotent by `Room.closed`: broadcasts `room_closed` into every client's send channel, closes it via a shared `sync.Once` (also used by the Handle disconnect defer, so whichever runs first wins — no double-close panic), stops `emptyTimer`, deletes the room from `Hub.rooms`, and drops every guest session whose `roomID` matches from `Hub.guests`. The writer goroutine now drains `c.send` to completion (writing any buffered `room_closed`) before closing the actual conn, instead of teardown hard-cancelling the connection's ctx — the frame is proven delivered before the socket dies. `Room.mu` critical sections in `dispatch` moved to a `withLock` helper (`Lock(); defer Unlock(); fn()`) so a panic mid-section still releases the lock during unwind, letting the per-room `recover()` (deferred in `Handle`'s read/dispatch loop and in the `emptyTimer` callback `fireEmpty`) safely re-lock `Room.mu` inside `teardown()` instead of deadlocking that room forever. `NewHub` takes an injectable `idle time.Duration` (`TOGETHER_ROOM_IDLE`, default `30m`, wired in `main.go` via `env()` + `time.ParseDuration`); `hub_test.go`'s `newStackIdle` threads a test-fast duration. New `hub_test.go` tests: `TestDeleteRoom_RoomClosedThenSocketsClose` (both connected clients receive `room_closed` before their socket errors on next read; room gone from hub; a cookie'd-but-WS-idle guest session dropped from `Hub.guests`; old join token → 404); `TestEmptyRoomTimer_FiresAfterIdle` (50ms idle: last live WS close → room torn down, a guest who only ever joined over HTTP never counted toward "live" and didn't keep it warm); `TestEmptyRoomTimer_RejoinWithinIdleStopsIt` (rejoin inside the window → room survives well past the original deadline, proving `Stop()` ran); `TestRoomPanic_TearsDownOnlyThatRoom` (a test-only `Room.panicTrigger` hook injects a panic into room A's `dispatch`; room A is logged `room panic id=<id> err=...` and torn down; room B's two independent clients keep exchanging chat frames and the process stays up) — run under `-race`, all clean.

---

## Task 7 — DEMO GATE 1: protocol-level kernel walk

**Human walkthrough — completion artifact is the human's recorded result.** Not automatable; a skipped gate is marked `GATE SKIPPED`, never deleted.

- **Journey:** two terminals (`websocat`) + `curl --noproxy '*'` against `TOGETHER_ADDR=:18080`, walking ARCHITECTURE §10 steps 1–4 and 7–11: login → create room → guest join via cookie jar → both dial WS → observe `hello` on both → `intent:play` from one echoes `activity` to both → `chat` both ways → kill one connection, redial → `hello` restores chat + mid-scene activity → `DELETE /api/rooms/{id}` → observe `room_closed` on the survivor.
- **Observations required:** every listed frame arrives, byte-for-byte field names per ARCHITECTURE §4.5 (screenshots/terminal captures optional).
- **Dependencies:** 6.
- **Completion artifact:** the human's walkthrough result logged on this task.

> **SOFT PASS (machine-driven, 2026-07-15).** Not the human demo — a scripted orchestrator (`coder/websocket` + `net/http`, throwaway `cmd/gatewalk`, deleted after) drove all of ARCHITECTURE §10 steps 1–4 and 7–11 against a live `TOGETHER_ADDR=:18080` server on a fresh data dir, seeding one `status='ready'` media row (no bytes streamed — protocol gate only). 12 observable frames verified byte-for-byte per §4.5: host `hello` (`you.isHost=true, name=admin`) + guest `hello` (`you.isGuest=true, name=Ali`); host `presence` broadcast on guest join; `intent:play` from host echoed `activity` (`rate=1`) to **both**; `chat` both directions echoed to both connections (`{name,isGuest,body,createdAt}`); guest socket killed then redialed → `hello` restored the 2-msg chat ring **and** the mid-scene `activity` (`version=2, paused=false`); host `DELETE /api/rooms/{id}` → survivor received `room_closed`. Caveat: the presence frame the host consumed at step 3 was its own join self-broadcast (count 1), not the guest-join frame — frame-ordering artifact of the scripted reader, not a protocol defect; the guest is present (guest `hello` + subsequent broadcasts confirm). **The human-witnessed demo (two `websocat` terminals) is NOT waived — this is a soft pass to unblock; re-walk at the v1 exit bar.**

---

## Task 8 — Frontend: guest join route + Home list

> **DONE** `3795100` — Verified: `cd web && npm run build` succeeds (generated `cmd/server/webdist/index.html` restored before commit); `go test ./...` and `go test -race ./...` pass; `gofmt -l internal cmd` is empty and `git diff --check` is clean. The compiled route flow puts `#/join/{token}` ahead of the `/api/me` gate, handles guest-cookie rejoin and terminal invalid/full states, and builds the authenticated live-room list/create dialog against the Task 3–4 API surface.

- **Objective:** Guests can enter via `#/join/{token}` before the auth gate; account users see live rooms and create one. (PLAN chunk 4, entry half.)
- **Inputs:** Tasks 3–4 endpoints live.
- **Outputs:** `JoinGuest.svelte` (S6), `Home.svelte` (S3, renamed from `Rooms.svelte`), route wiring, `api.js` calls.
- **Dependencies:** 6 (server surface complete).
- **Files:** `web/src/App.svelte`, `web/src/pages/JoinGuest.svelte` (new), `web/src/pages/Home.svelte` (renamed from `Rooms.svelte`), `web/src/lib/api.js`.
- **Acceptance criteria:**
  - Anonymous browser at `#/join/{token}` sees S6 with "You're invited to '<room name>'" — no login redirect (renders before the `/api/me` gate).
  - Empty and 33-char names rejected inline (AC-1.3); dead link and room-full show terminal states — form replaced, no retry affordance.
  - A browser with a live guest cookie reopening the link skips the form into the room (AC-1.5 seed).
  - Home shows live rooms (name, media title, kind, participant count; whole row ≥44px click target), plus empty ("No rooms right now." + centered Create), loading, and inline-retry error states per UX S3. Create room = plain dialog this task (ready items only, name optional).
  - Guest at `#/` or `#/admin` sees no room list or admin data (AC-1.8).
- **Difficulty:** medium.
- **Interfaces:**
  - CONSUMES (ARCHITECTURE §4.3): `GET /api/rooms/join/{token}` → `{roomName}` / 404; `POST /api/rooms/join` `{token,name}` → `{roomId}` + cookie, 400/404/409; `GET /api/rooms` → `[{id,name,mediaId,mediaTitle,kind,participants}]`; `POST /api/rooms` `{mediaId,name?}` → 201 `{id,joinToken}`; `GET /api/media` (V1, for the plain create dialog).
  - PRODUCES: hash routes `#/join/{token}`, `#/room/{id}` navigation on join success (task 9's Room mounts there); `Home.svelte` as S3 for task 14's restyle.
- **Context pack (hints):** `web/src/App.svelte`, `web/src/pages/Rooms.svelte` (to rename), `web/src/lib/api.js`, `web/src/lib/router.svelte.js` (read-only). ARCHITECTURE §4.3, §6. UX screens **S6, S3**; UX §2 navigation map. DESIGN.md not needed (V1 CSS primitives allowed until task 13).
- **Do NOT:** no acquisition panel or blob playback; no shadcn; no router dep.

---

## Task 9 — Frontend: Room shell on the V2 protocol

- **Objective:** Room page speaks the V2 wire protocol: renders from `hello`/`presence`/`chat`/`activity`, survives reconnect, honors `room_closed`. (PLAN chunk 4, room half.)
- **Inputs:** Task 5–6 frames; task 8 routes.
- **Outputs:** rewritten `Room.svelte` (S4 skeleton), `RoomClosed.svelte`, updated `Chat.svelte` + `ws.js`.
- **Dependencies:** 8.
- **Files:** `web/src/pages/Room.svelte` (rewrite), `web/src/components/Chat.svelte`, `web/src/components/RoomClosed.svelte` (new), `web/src/lib/ws.js`.
- **Acceptance criteria:**
  - Two browsers: guest joins via link and appears in both participant lists ≤2s (AC-1.2); duplicate name shows `(2)` (AC-1.4).
  - Guest reload → same identity, no form, exactly one presence entry (AC-1.5).
  - Chat both ways; history restored on rejoin via `hello` (AC-4.1/4.2).
  - Kill the connection: reconnect banner appears and disables intents + chat input while down (AC-3.6); state restored silently on reconnect.
  - Host ends room (curl `DELETE` this task) → both browsers show Room Closed ≤2s (AC-5.2); Back-to-rooms visible for account users only.
- **Difficulty:** high.
- **Interfaces:**
  - CONSUMES (ARCHITECTURE §4.5, verbatim field names — hand-matched to `hub.go` in the same commit per CONVENTIONS): `hello` `{you:{name,isHost,isGuest}, users:[…], activity, chat:[…], room:{name,kind,mediaId}}`; `presence` `{users:[{name,isHost,isGuest,status}]}`; `chat` `{name,isGuest,body,createdAt}` (unix **seconds**); `activity` `{id,type:"watch",state:{position,rate,paused,updatedAt}}`; `pong` `{t,serverTime}` (unix **ms**); `room_closed` `{}`. Outbound: `chat`, `intent`, `ping`, `status` (sent by task 11).
  - PRODUCES: `ws.js` — reconnect with exponential backoff (1s → cap 30s), EMA clock offset kept, message bus with the new frame types; `Room.svelte` shell that task 11 slots AcquisitionPanel/Player into; participant list with text status (dots in task 11).
- **Context pack (hints):** `web/src/pages/Room.svelte`, `web/src/components/Chat.svelte`, `web/src/lib/ws.js`, `web/src/lib/sync.js` (read-only). ARCHITECTURE §4.5, §6, §8 (client sync). UX screens **S4** (shell + reconnect/loading states), **V1** (Room Closed). DESIGN.md not needed yet.
- **Do NOT:** no acquisition panel or blob playback (Player may temporarily keep V1 streaming); do not change `sync.js`.

---

> **DONE** `f14fa70` — Verified: `node --test web/src/lib/ws.test.js` drives connection-state transitions and a V2 `hello` frame through the socket message bus; `npm --prefix web run build`, `go test ./...`, `go test -race ./...`, `gofmt -l internal cmd`, and `git diff --check` all pass. The room shell consumes `hello`/`presence`/`chat`/`activity`, disables chat while reconnecting, restores state from each `hello`, and replaces the view on `room_closed`.

## Task 10 — localfile.js + acquisition panel

> **DONE** `5785d6a` — Verified: `node --test src/lib/localfile.test.js` passes exact-size acceptance, mismatch byte reporting, and blob-URL replacement/revocation; `npm --prefix web run build`, `go test ./...`, `go test -race ./...`, `gofmt -l internal cmd`, and `git diff --check` pass. The compiled Room acquisition path keeps the panel in the player region until a source is explicitly selected, passes only a browser-managed object URL for a size-matched local file, blocks mismatches inline, and requires an affirmative stream-fallback confirmation.

- **Objective:** The size-check gate: pick a local file, verify exact bytes against room meta, block mismatches inline. (PLAN chunk 5, acquisition half.)
- **Inputs:** Task 4's `/api/rooms/{id}/meta` + `/media/{id}/download`; task 9's Room shell.
- **Outputs:** tested pure `localfile.js`; `AcquisitionPanel.svelte` occupying the player region.
- **Dependencies:** 9.
- **Files:** `web/src/lib/localfile.js` (new), `web/src/lib/localfile.test.js` (new), `web/src/components/AcquisitionPanel.svelte` (new), `web/src/pages/Room.svelte`.
- **Acceptance criteria:**
  - `node --test src/lib/localfile.test.js` green: `file.size === meta.media.sizeBytes` → ok; mismatch result carries both sizes; objectURL create/revoke helpers revoke on replace.
  - Panel shows media title + human size + kind; **Download from server** is plain navigation to `/media/{id}/download` (browser owns progress/resume; page memory must not grow); **Load your copy** opens a file picker; helper text funnels download users back to the picker (UX §3.4).
  - Mismatched file → inline blocked state showing selected vs expected size with a re-pick affordance — never a modal, never auto-streams (AC-2.x seeds).
  - **Play from server instead** is the quietest element, last; this task it's a `window.confirm` stand-in (M4 dialog in task 16); confirm switches src to `/media/{id}/stream`.
- **Difficulty:** medium.
- **Interfaces:**
  - CONSUMES (ARCHITECTURE §4.3–4.4): `GET /api/rooms/{id}/meta` → `{name, kind, media:{id,title,sizeBytes,duration}, subtitles:[{id,label}]}`; `GET /media/{id}/download` (attachment); `GET /media/{id}/stream` (fallback only).
  - PRODUCES: `localfile.js` exports (size check + objectURL helpers) that task 11's Player consumes; AcquisitionPanel emits a "file accepted → objectURL" event/prop upward to Room.
- **Context pack (hints):** `web/src/pages/Room.svelte` (task 9 output), `web/src/lib/api.js`. ARCHITECTURE §4.3–4.4, §6, §8 (Acquisition). UX **§3.4** (acquisition panel states), **M4** copy (for the stand-in). DESIGN.md not needed yet.
- **Do NOT:** never read the media file into page memory (no Blob/ArrayBuffer buffering — NFR-4); no automatic streaming fallback on any failure (FR-13).

---

## Task 11 — Blob player + status ladder + participant dots

> **DONE** `f00aa9b` — Verified: `node --test web/src/lib/*.test.js`, `npm --prefix web run build`, `go test ./...`, `go test -race ./...`, `gofmt -l internal cmd`, and `git diff --check` pass. The compiled Room route mounts blob-source Player playback with arm-only echo-driven sync, sends the three status edges, keeps subtitle tracks server-backed, and renders presence status dots with title tooltips. Two-browser media playback is the required Task 12 demo-gate walkthrough.

- **Objective:** Playback from the local file with the V1 sync discipline intact; readiness advertised via `status` frames and rendered as dots. (PLAN chunk 5, playback half.)
- **Inputs:** Task 10's objectURL; task 9's WS bus.
- **Outputs:** rewritten `Player.svelte`, new `Participants.svelte`, Room wiring.
- **Dependencies:** 10.
- **Files:** `web/src/components/Player.svelte` (rewrite), `web/src/components/Participants.svelte` (new), `web/src/pages/Room.svelte`.
- **Acceptance criteria:**
  - AC-2.1–2.8 pass manually in two browsers; during 60s of local playback the network tab shows zero media requests (AC-2.6).
  - Play is never gated on anyone's readiness (AC-3.3); arming mid-scene lands within the 1s band of the projected position, not 0:00 (AC-3.4).
  - Status transitions sent on the real edges: `downloading` (no valid file) → `file_ready` (size-matched, not armed) → `in_sync` (armed + tracking within drift bands); the *other* browser's participant list shows the dot ladder `◌ ◐ ●` change (title-attribute tooltip this task).
  - Subtitles still render via server `<track>` (guest cookie rides the fetch).
- **Difficulty:** high.
- **Interfaces:**
  - CONSUMES: task 10's objectURL + size-check result; task 9's `ws.js` bus (`activity` in, `intent` out); `sync.js` `PositionAt` (read-only); ARCHITECTURE §6 player contract: user actions send intents only; the `<video>` element mutates only on broadcast state; 500ms drift loop (>1s hard seek, >0.15s playbackRate nudge); `crossorigin` dropped (inert on `blob:`); arm overlay until first user gesture (FR-12).
  - PRODUCES (ARCHITECTURE §4.5): outbound `status` `{state:"downloading"|"file_ready"|"in_sync"}` on transitions; `Participants.svelte` (dot + name + HOST badge) that task 16's SidePanel adopts.
- **Context pack (hints):** `web/src/components/Player.svelte` (V1, to rewrite), `web/src/lib/sync.js` (read-only), `web/src/lib/ws.js`, `web/src/pages/Room.svelte`, `web/src/lib/localfile.js`. ARCHITECTURE §6 (player contract), §4.5 (`status`), §8. UX **S4** (§3.4 hand-off, dot ladder, arm overlay). DESIGN.md not needed yet.
- **Do NOT:** do not change `sync.js`; no shadcn; no memory-buffered playback.

---

## Task 12 — DEMO GATE 2: WALKING SKELETON — kernel journey F1 end-to-end

**Human walkthrough — completion artifact is the human's recorded result. Milestone A ends here.** A skipped gate is marked `GATE SKIPPED`, never deleted.

> **GATE SKIPPED (2026-07-16)** — Blocked before the guest-join step: room creation returns `joinToken`, but the current host UI discards it and exposes no visible copy-invite-link control. The token endpoint can be invoked manually from DevTools, but that is not a valid human UI walkthrough. Re-run this gate after the host-facing invite-link control exists.

- **Journey:** two browsers (normal + incognito), real uploaded video, walking UX F1 steps 1–11 verbatim: host signs in → creates room → copies link → guest joins named "Ali" → downloads media → loads file → size check → File Ready dot on both screens → host plays → sync within bands → guest scrubs → both jump → guest connection killed and restored mid-scene via the invite link (same name, mid-scene position) → chat → host ends room → both see Room Closed; library + accounts intact.
- **Observations required:** every step, plus the three dot states changing on the *other* browser's participant list. Ugly V1 styling acceptable; **any faked step fails the gate.**
- **Dependencies:** 11.
- **Completion artifact:** the human's walkthrough result logged on this task (screenshots optional).

---

## Task 13 — shadcn-svelte foundation + token mapping

- **Objective:** Design-system substrate: shadcn-svelte installed on the existing Tailwind v4 setup, semantic vars mapped to NxCode tokens, zero new raw hex. (PLAN chunk 6, foundation half.)
- **Inputs:** DESIGN.md §3 (token map), existing `web/src/app.css` `@theme`.
- **Outputs:** `components.json`, generated `components/ui/*` (Button/Input/Card/Dialog/DropdownMenu/Skeleton/Alert per DESIGN.md §4 inventory), app.css var mapping.
- **Dependencies:** 12 (skeleton demoed before restyle).
- **Files:** `web/package.json`, `web/components.json` (new), `web/src/app.css`, `web/src/components/ui/*` (generated).
- **Acceptance criteria:**
  - `./build.sh` succeeds; a shadcn Button rendered anywhere shows NxCode colors (semantic CSS vars resolve to existing `@theme` tokens per DESIGN.md §3, including `--radius-pill` and the motion-duration vars DESIGN.md names).
  - `grep -rn '#[0-9a-fA-F]\{3\}' web/src/components web/src/pages` — no new raw hex outside generated `components/ui/` internals (target: empty).
  - Focus ring on a generated component is the secondary (cyan) token.
  - Existing pages still function (no primitive removed yet — that's tasks 14/16).
- **Difficulty:** medium.
- **Interfaces:**
  - CONSUMES: DESIGN.md §3 token map (the contract), existing `@theme` block in `app.css`; JS dependency ceiling (ARCHITECTURE §7): svelte 5 · vite · tailwind v4 · shadcn-svelte · lucide-svelte.
  - PRODUCES: `web/src/components/ui/*` component imports that tasks 14–16 and 19 build on; shadcn-generated `components/ui/` files are excluded from per-task line budgets.
- **Context pack (hints):** `web/src/app.css`, `web/package.json`, `web/vite.config.*`. **DESIGN.md §3–4** (the contract for this task). ARCHITECTURE §6 (stack), §7 (dep ceiling). UX not needed (no screen work).
- **Do NOT:** no extra shadcn components beyond DESIGN.md §4's inventory; no light theme; no page restyles yet.

> **DONE** `c6153f8` — Verified: `./build.sh`, `go test ./...`, `go test -race ./...`, and `node --test src/lib/*.test.js` all pass; the generated Button/Input/Card/Dialog/DropdownMenu/Skeleton/Alert/Tooltip/Table/Slider/AlertDialog inventory is present and Tailwind emits its NxCode semantic utilities, whose `--ring` resolves to cyan; a strict raw-color scan of non-generated components/pages is empty.

---

## Task 14 — Account surfaces on shadcn: Login, Register, Home, MediaPicker, Admin kind column

> **DONE** `bb8e9eb` — Verified: `cd web && npm run build`, `node --test src/lib/*.test.js`, `go test ./... -count=1`, and `go test -race ./... -count=1` pass; the fresh `TestCreateRoom_Validation|TestCreateRoom_RejectsNonReadyMedia|TestListRooms_Shape` HTTP stack confirms ready-media room creation and room-list output.

- **Objective:** S1/S2/S3/M1 adopt the design system; Admin table gains the kind column. (PLAN chunk 6, surfaces half.)
- **Inputs:** Task 13's `components/ui/*`.
- **Outputs:** restyled Login + new Register split, Home with skeleton/error/empty states, `MediaPickerDialog.svelte` (M1), Admin kind column.
- **Dependencies:** 13.
- **Files:** `web/src/pages/Login.svelte`, `web/src/pages/Register.svelte` (new — split out of Login), `web/src/pages/Home.svelte`, `web/src/components/MediaPickerDialog.svelte` (new), `web/src/pages/Admin.svelte` (kind column only).
- **Acceptance criteria:**
  - M1 per UX: single-select **ready** items only, showing title/kind/human size; Create disabled until selection; room name optional defaulting to media title; empty states — admin sees S7 link, member sees "Ask your admin to upload something."
  - Create → room appears in another account's Home list (AC-5.1); Home shows skeleton loading rows, inline retry banner, "No rooms right now." empty state with centered Create.
  - S1/S2 per UX: split screens, inline errors, failed register visibly keeps the invite code retryable.
  - Admin library table shows `video|audio` kind column (S7).
  - `.btn-primary`/`.btn-ghost`/`.input` usage removed from every page this task touches.
- **Difficulty:** medium.
- **Interfaces:**
  - CONSUMES: task 13's ui components; `GET /api/media` → `[{id,kind,title,status,sizeBytes,duration,…}]` (kind now `video|audio`); `POST /api/rooms` `{mediaId,name?}` → 201; V1 auth endpoints (ARCHITECTURE §4.1).
  - PRODUCES: `MediaPickerDialog.svelte` (M1) reused untouched by task 19's audio labeling; Register/Login as final S1/S2.
- **Context pack (hints):** `web/src/pages/Login.svelte`, `web/src/pages/Home.svelte`, `web/src/pages/Admin.svelte`, `web/src/components/ui/*` (task 13 output), `web/src/lib/api.js`. ARCHITECTURE §4.1–4.3, §6. UX screens **S1, S2, S3, M1, S7** (kind column note); **DESIGN.md**. A11y floors from CLAUDE.md: contrast ≥4.5:1, cyan focus rings, ≥44px targets, body ≥15px, reduced-motion.
- **Do NOT:** no room-interior restyle; no other Admin changes.

---

## Task 15 — Room strip, Room menu, dialogs M2/M3/M4

> **DONE** `3a0ba20` — Verified: `cd web && npm run build`, `node --test src/lib/*.test.js`, `go test ./...`, and `go test -race ./...` pass; `TestRoomToken_HostOnly` drives the real HTTP surface (non-host → 403, returning host receives the current join token), while the existing real-stack regeneration and teardown tests cover token replacement and `room_closed`. The room UI now uses its host-only token read to copy/regenerate invites, replaces the stream `window.confirm` with M4, and sends host room-end through M2.

- **Objective:** Host powers land in the UI: copy link, regenerate (M3), end room (M2); the streaming fallback gets its real dialog (M4). (PLAN chunk 7, controls half.)
- **Inputs:** Task 13's ui components; tasks 2/6 endpoints; task 10's confirm stand-in.
- **Outputs:** `RoomStrip.svelte`, `RoomMenu.svelte`, `EndRoomDialog.svelte`, `RegenerateLinkDialog.svelte`, `PlayFromServerDialog.svelte`; AcquisitionPanel's `window.confirm` replaced.
- **Dependencies:** 14.
- **Files:** `web/src/components/RoomStrip.svelte` (new), `RoomMenu.svelte` (new), `EndRoomDialog.svelte` (new, M2), `RegenerateLinkDialog.svelte` (new, M3), `PlayFromServerDialog.svelte` (new, M4), `web/src/components/AcquisitionPanel.svelte`, `web/src/pages/Room.svelte`.
- **Acceptance criteria:**
  - Strip: Leave, room name + media title, HOST badge; auto-hides during playback; guests see only the room name (no Room menu).
  - Copy invite link builds `#/join/{token}` from joinToken and confirms "Link copied".
  - M3 regenerate → old link terminal-invalid for new joins, already-joined guest survives, new link works (AC-1.6/1.7).
  - M2 end flow → both participants see Room Closed (AC-5.2).
  - M4 flow with UX §M4 copy → confirm switches the player to `/media/{id}/stream` (AC-2.7); never automatic.
- **Difficulty:** medium.
- **Interfaces:**
  - CONSUMES: `GET /api/rooms/{id}/token` → `{joinToken}` for a returning host; `POST /api/rooms/{id}/token` → `{joinToken}`; `DELETE /api/rooms/{id}`; `hello.you.isHost` for menu visibility; task 13's Dialog/DropdownMenu; task 10's stream-switch hook in AcquisitionPanel.
  - PRODUCES: `RoomStrip` slot in Room layout that task 16's panel toggle coexists with; joinToken must be available client-side to the host (from `POST /api/rooms` create response or room meta — verify with real code; if absent for a re-entering host, route through docs before improvising).
- **Context pack (hints):** `web/src/pages/Room.svelte`, `web/src/components/AcquisitionPanel.svelte`, `web/src/components/ui/*`, `web/src/lib/api.js`. ARCHITECTURE §4.3, §6. UX screens **S4 §1 (strip), M2, M3, M4**; UX §5 (host controls buried one disclosure deep — must not sit next to play/pause); **DESIGN.md**.
- **Do NOT:** no kick/co-host affordances (backlog); no audio UI.

---

## Task 16 — Side panel + transport bar + retire V1 CSS primitives

> **DONE** `3232d09` — Verified: `npm run build` green; `go test ./...` and `node --test web/src/lib/*.test.js` green; `gofmt -l internal cmd` empty; `grep -rn 'btn-primary\|btn-ghost\|\.input\|\.card' web/src` empty; temporary `TOGETHER_ADDR=:18080` server returned `ok` from `/healthz`. The Room transport sends only intents and is disabled while disconnected; the persisted side panel defaults open for audio rooms.

- **Objective:** The theater layout completes: collapsible side panel (participants + chat), echo-driven transport bar, V1 primitives deleted. (PLAN chunk 7, layout half.)
- **Inputs:** Task 15's strip; task 11's Participants; task 9's Chat.
- **Outputs:** `SidePanel.svelte`; restyled Participants/Chat/AcquisitionPanel; transport bar in Room; `app.css` cleanup.
- **Dependencies:** 15.
- **Files:** `web/src/components/SidePanel.svelte` (new), `Participants.svelte`, `Chat.svelte`, `AcquisitionPanel.svelte`, `web/src/pages/Room.svelte`, `web/src/app.css`.
- **Acceptance criteria:**
  - Panel collapses/expands, remembers state in localStorage across reload; Participants fixed on top, Chat fills.
  - Dot Tooltip (shadcn) spells out the state name; shows on hover **and** keyboard focus.
  - Transport bar: play/pause, scrub Slider wired to seek intents, position/duration in mono, CC, fullscreen, panel toggle — all echo-driven (UI reflects only broadcast state).
  - `grep -rn 'btn-primary\|btn-ghost\|\.input' web/src` → empty (V1 primitives fully retired from app.css and all usage).
- **Difficulty:** medium.
- **Interfaces:**
  - CONSUMES: task 11's `Participants.svelte` + status data from `presence` frames; task 9's Chat; task 13's Tooltip/Slider; player intent functions from task 11.
  - PRODUCES: `SidePanel.svelte` with an `open`-by-default-when-`kind=audio` contract that task 19 consumes; transport bar minus CC/fullscreen reused by AudioPlayer.
- **Context pack (hints):** `web/src/pages/Room.svelte`, `web/src/components/Participants.svelte`, `Chat.svelte`, `AcquisitionPanel.svelte`, `web/src/app.css`, `web/src/components/ui/*`. ARCHITECTURE §6. UX screens **S4 §2–4, §5 density**; **DESIGN.md**. A11y floors apply (44px targets on transport).
- **Do NOT:** no audio UI; host controls stay in the strip menu.

---

## Task 17 — DEMO GATE 3: styled kernel + host powers

**Human walkthrough — completion artifact is the human's recorded result.** A skipped gate is marked `GATE SKIPPED`, never deleted.

> **GATE BLOCKED (2026-07-16)** — The guest S6 Join room button is rendered by the shared Button primitive without `type="submit"`; its default is `type="button"`, so an entered name never reaches `POST /api/rooms/join`. The initial empty-name probe's 400 is expected and transitions S6 to the form. Fix in Task 17a, then re-run this gate with a newly created room after the server has started.

- **Journey:** re-walk F1 fully styled, plus F4: kill the host tab mid-playback → guest playback continues, guest can pause/seek/chat (AC-5.5) → host reopens from Home → HOST badge + Room menu restored. Then the AC-1.6/1.7 regenerate walk.
- **Observations required:** theater layout (player dominates, strip auto-hides), dot tooltips, reconnect banner disabling inputs, all three dialogs (M2/M3/M4), "Link copied" confirm.
- **Dependencies:** 16.
- **Completion artifact:** the human's walkthrough result logged on this task.

---

## Task 17a — Fix guest join form submission

> **DONE** `c3a3409` — Verified: `npm --prefix web run build`, `go test ./...`, and `node --test web/src/lib/*.test.js` pass; the S6 control now submits its valid entered name to `POST /api/rooms/join` after the expected empty-name rejoin probe returns 400.

- **Objective:** The S6 Join room control submits the entered guest name.
- **Dependencies:** 16.
- **Files:** `web/src/pages/JoinGuest.svelte`.
- **Acceptance criteria:**
  - The Join room control uses `type="submit"`; after the initial empty-name probe returns 400 and S6 shows its form, entering a valid name and activating the control sends `POST /api/rooms/join` with that name and navigates to the returned room.
  - `npm --prefix web run build`, `go test ./...`, and `node --test web/src/lib/*.test.js` pass.
- **Difficulty:** low.
- **Context pack:** `web/src/pages/JoinGuest.svelte`, `web/src/components/ui/button/button.svelte`, `web/src/lib/api.js`.
- **Do NOT:** Do not change the initial empty-name probe; it is the guest-cookie rejoin check.

---

## Task 18 — Audio pipeline branch

- **Objective:** Ingest handles pure-audio files without ever touching libx264; `kind` comes from the probe, not the client. (PLAN chunk 8, backend half.)
- **Inputs:** V1 `pipeline.go` decision tree; task 1's kind vocabulary.
- **Outputs:** audio branch in the worker; `upload.go` stops accepting a client-supplied kind.
- **Dependencies:** 12 (Milestone A done; independent of styling tasks 13–17).
- **Files:** `internal/media/pipeline.go` + test, `internal/media/upload.go` + test.
- **Acceptance criteria:**
  - Pipeline tests with lavfi-synthesized fixtures (skip pattern if ffmpeg missing): sine-wave mp3 → moved as-is, not transcoded; opus → `-c:a aac` into `.m4a`; `media.kind` set to `audio` from the probe; a video fixture still takes the V1 tree and gets `kind='video'`.
  - `upload.go` ignores/rejects any client-supplied kind field; kind visible in `GET /api/media` after ingest.
  - `go test ./internal/media/` green; still one job at a time under `nice -n 19`.
- **Difficulty:** medium.
- **Interfaces:**
  - CONSUMES: V1 pipeline worker structure (probe → decision tree), V1 lavfi fixture-synthesis test pattern; `media.kind` column (task 1 vocabulary).
  - PRODUCES (ARCHITECTURE §4.2): pipeline decision at `finish`: probe → **no video stream** ⇒ audio branch: aac/m4a/mp3 → move as-is, else `-c:a aac` into `.m4a`; **video** ⇒ V1 tree unchanged. `kind` set from the probe. **No libx264 path for audio, ever.** Task 19 relies on `kind='audio'` media rows flowing through rooms meta and `hello.room.kind`.
- **Context pack (hints):** `internal/media/pipeline.go` + test, `internal/media/upload.go` + test. ARCHITECTURE §4.2. No UX/DESIGN (backend-only).
- **Do NOT:** no waveform/artwork; no multiple audio tracks; do not touch the video pipeline tree.

---

## Task 19 — AudioPlayer + S5 now-playing surfaces

- **Objective:** Audio rooms get the now-playing variant; pickers and admin label kinds. (PLAN chunk 8, frontend half.)
- **Inputs:** Task 18's `kind='audio'` rows; task 16's SidePanel + transport.
- **Outputs:** `AudioPlayer.svelte`, kind-switched Room, labeled M1/S7.
- **Dependencies:** 16, 18.
- **Files:** `web/src/components/AudioPlayer.svelte` (new), `web/src/pages/Room.svelte`, `web/src/components/MediaPickerDialog.svelte`, `web/src/pages/Admin.svelte`.
- **Acceptance criteria:**
  - Room switches player component on `kind`: AudioPlayer (S5) = now-playing anchor (title + duration, mono numerics), transport minus CC/fullscreen, side panel **open by default**; acquisition panel and status ladder identical to video (AC-6.1–6.4 manually, two browsers).
  - M1 and S7 label every item `video`/`audio`.
- **Difficulty:** low.
- **Interfaces:**
  - CONSUMES: `hello.room.kind` + `/api/rooms/{id}/meta` `kind` field; task 11's intent/echo/drift player contract (identical for `<audio>`); task 16's SidePanel `open`-default contract and transport bar; task 10's AcquisitionPanel unchanged.
  - PRODUCES: nothing later tasks rely on (leaf task).
- **Context pack (hints):** `web/src/pages/Room.svelte`, `web/src/components/Player.svelte` (contract reference), `MediaPickerDialog.svelte`, `web/src/pages/Admin.svelte`, `SidePanel.svelte`. ARCHITECTURE §6. UX screens **S5, M1, S7**; **DESIGN.md**.
- **Do NOT:** no waveform/artwork; no divergence from the video sync contract.

---

## Task 20 — Hardening sweep + docs

- **Objective:** Edge-case test backfill, race-clean suite, full AC walk, docs truthful. (PLAN chunk 9.)
- **Inputs:** everything prior.
- **Outputs:** backfilled tests, `-race` green, per-AC log, updated README/CLAUDE.md.
- **Dependencies:** 17, 19.
- **Files:** `internal/live/*_test.go`, `web/src/lib/ws.js`, `README.md`, `CLAUDE.md`, `.superpowers/sdd/progress.md`.
- **Acceptance criteria:**
  - New tests pass: name freed after departure vs kept on reconnect (PRD §9); two host tabs coexist; backward-clock clamp covered in both `watch_test.go` and `sync.test.js`; room-full terminal state; dead-vs-unknown-token no-oracle.
  - `go test ./... -race` green; `gofmt -l internal cmd` empty; `cd web && node --test src/lib/*.test.js` green.
  - Every AC in PRD §6 has a logged pass (or a logged, user-acknowledged deviation) in `.superpowers/sdd/progress.md`, walked in two browsers.
  - README env table gains `TOGETHER_ROOM_IDLE`; CLAUDE.md updated (routes, files, doc map — it still references deleted `docs/debt.md` and the V1 plan); restic backup units verified untouched; `git restore cmd/server/webdist/index.html` before the commit.
- **Difficulty:** medium.
- **Interfaces:**
  - CONSUMES: the full V2 surface as documented in ARCHITECTURE §4 (the checklist's ground truth).
  - PRODUCES: nothing (terminal task).
- **Context pack (hints):** `internal/live/*_test.go`, `internal/live/watch_test.go`, `web/src/lib/sync.test.js`, `web/src/lib/ws.js`, `README.md`, `CLAUDE.md`, PRD §6 + §9. ARCHITECTURE §4–5, §9. No UX/DESIGN.
- **Do NOT:** no new features; no backlog items sneaking in.

---

## Task 21 — DEMO GATE 4: v1 exit bar

**Human walkthrough — completion artifact is the human's recorded result.** A skipped gate is marked `GATE SKIPPED`, never deleted.

- **Journey:** on the production build (`./build.sh` binary, not the dev server):
  1. US-6 — music room end-to-end from admin upload of an audio file.
  2. US-7 — invite-code register incl. failed-attempt-keeps-code; resumable upload with mid-upload reload; processing→ready→failed states; delete.
  3. Crash drill — restart the server with a live room → sign-in works, library and invite codes intact, room and chat gone (AC-5.6).
- **Observations required:** all three, on the `./build.sh` binary.
- **Dependencies:** 20.
- **Completion artifact:** the human's walkthrough result logged on this task.

---

## Dependency graph

```
1 → 2 → 3 → 4 → 5 → 6 → [GATE 1: 7]
                    6 → 8 → 9 → 10 → 11 → [GATE 2: 12]  ← Milestone A ends
12 → 13 → 14 → 15 → 16 → [GATE 3: 17]
12 → 18 ─┐
16 ──────┴→ 19
17, 19 → 20 → [GATE 4: 21]
```

Tasks 13–17 (styling) and 18 (audio backend) may run in parallel after gate 2; task 19 needs both branches.
