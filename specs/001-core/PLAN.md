---
status: gate-passed
---

# PLAN.md — Together V2: Ephemeral Rooms + Local Playback

Implementation plan for the V2 refactor. Sources: `specs/001-core/SPEC.md` (§9 resolved decisions bind), `specs/001-core/PRD.md` (FR/NFR/AC ids referenced), `ARCHITECTURE.md`, `UX.md`, `DESIGN.md`, `CONVENTIONS.md`. Executes against the shipped V1 codebase — chunks name real files.

**Milestone A (chunks 1–5) is the walking skeleton:** the thinnest end-to-end slice that makes the kernel journey (UX F1) pass in the real app. Ugly is fine (V1 CSS primitives allowed until chunk 6), fake is not (real guest sessions, real local-file playback, real teardown). Chunks 6–9 deepen it.

Every chunk: builds, `go test ./...` green, `gofmt -l internal cmd` empty, one commit. Max ~300 new lines of code per chunk (shadcn-generated `components/ui/` files excluded from the budget).

---

## Chunk 1 — DB cutover + rooms move into the hub

**Files:** `internal/db/db.go`, `internal/db/db_test.go`, `internal/live/rooms.go` (new), `internal/live/rooms_test.go` (new), `internal/live/hub.go`, `internal/live/hub_test.go`, `internal/media/serve.go`, `cmd/server/main.go`; **delete** `internal/api/` (both files).

**Requirements:**
- `db.go`: after the idempotent DDL, run the idempotent V2 cutover — `DROP TABLE IF EXISTS rooms; DROP TABLE IF EXISTS messages; DROP TABLE IF EXISTS activities;` plus kind-vocabulary normalization `UPDATE media SET kind='video' WHERE kind='movie'; UPDATE media SET kind='audio' WHERE kind='music';` (ARCHITECTURE §3.1). Remove the migration-note ponytail comment (this is the migration).
- `internal/live/rooms.go`: `Room` struct per ARCHITECTURE §3.2 (`id`, `name`, `ownerID`, `mediaID`, `kind`, `joinToken`, `watch WatchState`, `clients`, `emptyTimer` — timer unused until chunk 3, chat ring until chunk 3). Room ids and join tokens from the same crypto-random generator as `internal/auth` session tokens (ids 16 hex chars, tokens ≥128-bit hex).
- HTTP handlers in `rooms.go`, mounted in `main.go` under `auth.Require` (account): `GET /api/rooms` (live rooms from hub: `{id,name,mediaId,mediaTitle,kind,participants}`), `POST /api/rooms` (`{mediaId,name?}` → 201 `{id,joinToken}`; media must be status `ready`, 404 otherwise; name ≤64 chars, defaults to media title; creates the room **and** starts its watch activity), `GET /api/rooms/{id}/token` (host-only current invite for a returning host), `DELETE /api/rooms/{id}` (host-only → 403; teardown v0: close sockets, delete from map), `POST /api/rooms/{id}/token` (host-only regenerate; old token dead for new joins).
- `hub.go`: delete `state_json` checkpointing on intents and the boot-time activity reclaim; `WatchState` now lives on the hub's `Room`. Keep the per-room mutex discipline.
- `serve.go`: kind query filter accepts `video|audio` (was `movie|music`).

**Acceptance (falsifiable):**
- Boot against a V1 database file: no error; `rooms`/`messages`/`activities` tables gone; `media.kind` values are only `video`/`audio`; second boot also clean (idempotence test).
- httptest: create room → 201 with id + joinToken; list shows it with media title; non-host DELETE → 403; host DELETE → gone from list.
- Restart the process with a room live: room gone, media/accounts/sessions intact (seed of AC-5.6).

**Do NOT:** touch guest auth, chat, presence, timers, watch.go, or any frontend file. Do not add a migration framework.

---

## Chunk 2 — Guest sessions, join surface, RequireRoom

**Files:** `internal/live/rooms.go` + test, `internal/media/serve.go` + test, `cmd/server/main.go`.

**Requirements (SPEC §9.1, §9.10, §9.11; ARCHITECTURE §4.3–4.4):**
- Hub gains `guests map[guestCookieToken]*GuestSession` (`guestID`, `roomID`, `name`); sessions die at room teardown, survive socket drops.
- `POST /api/rooms/join` (public): `{token,name}` → 200 `{roomId}` + `together_guest` cookie (HttpOnly, SameSite=Lax, Secure when TLS-terminated — same detection as `auth`). Validation: strip control chars, 1–32 chars (400); token must match a **live** room (404, same body as unknown room — no oracle); participant cap 12 (409). Name collision against currently-connected names → suffix `(2)`, `(3)`; a request carrying a live guest cookie for that room short-circuits: same identity returned, no new session, no re-suffix (FR-5).
- `GET /api/rooms/join/{token}` (public): → `{roomName}` / 404 — the S6 pre-join peek.
- `GET /api/rooms/{id}/meta` (room auth): → `{name, kind, media:{id,title,sizeBytes,duration}, subtitles:[{id,label}]}`.
- `live.RequireRoom` middleware: passes an account session **or** a guest session whose `roomID` matches the target room; for guests, media routes additionally require `{id}` == their room's `mediaID`. Rewire to it: `GET /ws/{roomId}`, `GET /media/{id}/stream`, `GET /media/{id}/subs/{sid}`; add `GET /media/{id}/download` = `http.ServeFile` + `Content-Disposition: attachment`.

**Acceptance:**
- Table tests: name strip/length/suffix/re-suffix-on-reconnect/freed-name-after-departure; cap 12 → 409; dead vs unknown token bodies byte-identical.
- httptest: guest cookie downloads its room's media (200/206 with Range); any other media id → 404; no cookie → 401. After regenerate, old token join → 404, new token → 200, previously-joined guest cookie still valid.

**Do NOT:** change any WS frame yet. No frontend. No rate limiter (unguessability is the rate limiter, SPEC §9.11).

---

## Chunk 3 — WS protocol V2: hello, presence + status, chat ring, teardown, timer, recover

**Files:** `internal/live/hub.go` + test, `internal/live/rooms.go` + test, `cmd/server/main.go`.

**Requirements (ARCHITECTURE §4.5, §5; SPEC §9.4–9.6):**
- Outbound `hello` on connect: `{you:{name,isHost,isGuest}, users:[…], activity, chat:[…], room:{name,kind,mediaId}}` — full stateless recovery, no event replay. `isHost` computed live: connection's account userID == room `ownerID`.
- Server-owned `presence` broadcast (`{users:[{name,isHost,isGuest,status}]}`) on join/leave/status change. Inbound `status` frame (`downloading|file_ready|in_sync`) updates the client's presence entry only — it must never enter the `intent`/`watch.Apply` path.
- Chat: per-room 200-entry ring buffer, drop-oldest; inbound `chat` ≤2000 chars, non-empty; outbound `{name,isGuest,body,createdAt}` with `createdAt` unix **seconds**; no DB writes anywhere. Delete V1's chat persistence.
- One teardown path (host end, timer fire, panic — under hub lock): broadcast `room_closed` → close sockets → cancel timer → delete room → drop its guest sessions → token forgotten. `DELETE /api/rooms/{id}` now routes through it.
- Empty timer: last WS close → `emptyTimer.Reset(idle)`; any join → `Stop()`; fire → teardown. A cookie'd-but-disconnected guest does not keep the room warm. `idle` from `TOGETHER_ROOM_IDLE` env (Go duration, default `30m`), read once in `main`.
- Per-room `recover()` in every goroutine that touches a room: log with room id → tear down that room → process keeps serving (NFR-7 — load-bearing now that checkpointing is gone).

**Acceptance:**
- WS-dial tests: two clients; status change on one → `presence` on both; 210 chats → fresh dial's `hello` carries exactly the latest 200; reconnect `hello` carries activity + chat.
- Timer test with `TOGETHER_ROOM_IDLE=50ms`: last close → room gone after fire; rejoin within idle → `Stop()` verified (room survives).
- Panic test: injected panic in room A's handling; room B's clients still exchange frames; process alive; room A gone.

**Do NOT:** touch `watch.go` or `web/src/lib/sync.js`. No host gating of intents (FR-16).

---

### DEMO GATE 1 — protocol-level kernel walk

Two terminals (`websocat`) + `curl --noproxy '*'` against `TOGETHER_ADDR=:18080`, walking ARCHITECTURE §10 steps 1–4 and 7–11: login → create room → guest join via cookie jar → both dial WS → observe `hello` on both → `intent:play` from one echoes `activity` to both → `chat` both ways → kill one connection, redial → `hello` restores chat + mid-scene activity → `DELETE /api/rooms/{id}` → observe `room_closed` on the survivor. **Must observe:** every listed frame, byte-for-byte field names per ARCHITECTURE §4.5.

---

## Chunk 4 — Frontend: guest join route + room shell on the new protocol

**Files:** `web/src/App.svelte`, `web/src/pages/JoinGuest.svelte` (new), `web/src/pages/Home.svelte` (renamed from `Rooms.svelte`), `web/src/pages/Room.svelte` (rewrite), `web/src/components/Chat.svelte`, `web/src/components/RoomClosed.svelte` (new), `web/src/lib/ws.js`, `web/src/lib/api.js`.

**Requirements (UX S3, S6, S4 shell, V1 view; SPEC §9.7):**
- `#/join/{token}` renders **before** the `/api/me` gate in `App.svelte` (anonymous short-circuit). JoinGuest (S6): peek `GET /api/rooms/join/{token}` for the room name; one name field, auto-focused; inline name errors; terminal states for dead link and room full (no form, no retry); a live guest cookie skips the form into the room.
- Home (S3): live rooms list (name, media title, kind, participant count; whole row ≥44px click target), Create room (plain dialog this chunk — ready items only, name optional), empty/loading/error states per UX.
- Room shell (S4 skeleton): owns the WS; renders from `hello`/`presence`/`chat`/`activity`; participant list with text status (dots in chunk 5); chat panel; reconnect banner that disables intents + chat input while down (AC-3.6); `room_closed` → RoomClosed view (Back to rooms for account users only).
- `ws.js`: reconnect with exponential backoff (1s → cap 30s), EMA clock offset kept; new frame types wired; field names hand-matched to `hub.go` in the same commit (CONVENTIONS).

**Acceptance:** two browsers — guest joins via link and appears in both participant lists ≤2s (AC-1.2); empty/33-char names rejected inline (AC-1.3); duplicate name shows `(2)` (AC-1.4); guest reload → same identity, no form, one presence entry (AC-1.5); chat both ways + history on rejoin (AC-4.1/4.2); host end → both see Room Closed ≤2s (AC-5.2); guest at `#/` or `#/admin` sees no room list/admin data (AC-1.8).

**Do NOT:** no acquisition panel or blob playback yet (Player may temporarily keep V1 streaming); no shadcn; do not add a router dep.

---

## Chunk 5 — Local-file playback: acquisition panel, blob player, status dots

**Files:** `web/src/lib/localfile.js` (new), `web/src/lib/localfile.test.js` (new), `web/src/components/AcquisitionPanel.svelte` (new), `web/src/components/Player.svelte` (rewrite), `web/src/components/Participants.svelte` (new), `web/src/pages/Room.svelte`.

**Requirements (SPEC §9.3–9.4, §9.8; UX §3.4; FR-9…FR-15):**
- `localfile.js` (pure, tested): size check `file.size === meta.media.sizeBytes` → ok/mismatch with both sizes; objectURL create/revoke helpers.
- AcquisitionPanel occupies the player region until a size-matched file is loaded: media title + human size + kind; **Download from server** (plain navigation to `/media/{id}/download` — browser owns progress/resume; page memory must not grow); **Load your copy** (file input); helper text funnels download users back to the picker; inline mismatch state (selected vs expected size, re-pick affordance, never a modal, never auto-streams); quiet **Play from server instead** last (this chunk: `window.confirm` stand-in; M4 dialog in chunk 7).
- Player: src = `blob:` objectURL (drop `crossorigin`); arm overlay until first user gesture (FR-12); intents/echo/drift loop unchanged (>1s hard seek, >0.15s rate nudge); subtitles stay server-`<track>` (guest cookie rides the fetch); streaming fallback uses `/media/{id}/stream` only after explicit confirm.
- Status reporting: client sends `status` on transitions — `downloading` (no valid file) → `file_ready` (size-matched, not armed) → `in_sync` (armed + tracking within drift bands). Participants shows the dot ladder `◌ ◐ ●` + name + HOST badge; title-attribute tooltip this chunk.

**Acceptance:** AC-2.1–2.8 pass manually in two browsers; AC-3.3 (play never gated on readiness) and AC-3.4 (mid-scene arm lands within 1s band, not 0:00); `node --test` green for `localfile.test.js`; during 60s local playback the network tab shows zero media requests (AC-2.6).

**Do NOT:** never read the media file into page memory (no Blob/ArrayBuffer buffering — NFR-4); no automatic streaming fallback on any failure (FR-13); do not change `sync.js`.

---

### DEMO GATE 2 — WALKING SKELETON: kernel journey F1 end-to-end

Two browsers (normal + incognito), real uploaded video, walking UX F1 steps 1–11 verbatim: host signs in → creates room → copies link → guest joins named "Ali" → downloads media → loads file → size check → File Ready dot on both screens → host plays → sync within bands → guest scrubs → both jump → guest connection killed and restored mid-scene via the invite link (same name, mid-scene position) → chat → host ends room → both see Room Closed; library + accounts intact. **Must observe:** every step, plus the three dot states changing on the *other* browser's participant list. Ugly V1 styling is acceptable here; any faked step fails the gate. Milestone A ends here.

---

## Chunk 6 — shadcn-svelte foundation + account surfaces

**Files:** `web/package.json`, `web/components.json` (new), `web/src/app.css`, `web/src/components/ui/*` (generated), `web/src/pages/Login.svelte`, `web/src/pages/Register.svelte` (new — split out of Login), `web/src/pages/Home.svelte`, `web/src/components/MediaPickerDialog.svelte` (new), `web/src/pages/Admin.svelte` (kind column only).

**Requirements (SPEC §6–7; DESIGN.md is the contract):**
- Install shadcn-svelte on the existing Tailwind v4 setup; `components.json` aliases generated components to `web/src/components/ui/`. Map shadcn semantic CSS vars → existing `@theme` tokens exactly per DESIGN.md §3 (no new raw hex anywhere; add the `--radius-pill` mapping and motion-duration vars DESIGN.md names).
- Adopt Button/Input/Card/Dialog/DropdownMenu/Skeleton/Alert on S1/S2/S3/M1: Login + Register split per UX; Home with skeleton loading rows, inline retry banner, empty state; MediaPickerDialog (M1) — single-select **ready** items with title/kind/human size, Create disabled until selection, room name optional defaulting to media title, empty states per UX (admin link vs "ask your admin").
- Admin: library table gains the kind column (S7); no other admin restyle this chunk.
- Remove `.btn-primary`/`.btn-ghost`/`.input` usage from the pages this chunk touches.

**Acceptance:** `./build.sh` succeeds; `grep -rn '#[0-9a-fA-F]\{3\}' web/src/components web/src/pages` (excluding `components/ui/` internals only if shadcn generates none — target: empty); focus rings are the secondary (cyan) token on every interactive element; AC-5.1 (create → appears in other account's list); M1 behaviors per UX §M1.

**Do NOT:** restyle the room interior yet; no light theme; no extra shadcn components beyond DESIGN.md §4's inventory.

---

## Chunk 7 — Room surfaces on shadcn: strip, menu, dialogs, side panel

**Files:** `web/src/components/RoomStrip.svelte` (new), `RoomMenu.svelte` (new), `EndRoomDialog.svelte` (new, M2), `RegenerateLinkDialog.svelte` (new, M3), `PlayFromServerDialog.svelte` (new, M4), `SidePanel.svelte` (new), `Participants.svelte`, `Chat.svelte`, `AcquisitionPanel.svelte`, `web/src/pages/Room.svelte`, `web/src/app.css`.

**Requirements (UX S4 §1–4, §5 density; DESIGN.md):**
- Room strip: Leave, room name + media title, HOST badge; host-only **Room** disclosure menu (DropdownMenu): copy invite link (built from joinToken → `#/join/{token}`, "Link copied" confirm), regenerate (→ M3 AlertDialog), end room (→ M2 AlertDialog). Strip auto-hides during playback.
- M4 replaces the chunk-5 `window.confirm`; its copy per UX §M4; confirm switches the player to `/media/{id}/stream`.
- Side panel: collapsible, remembers state (localStorage), Participants block fixed on top, Chat fills; dot Tooltip spells out the state name; panel toggle in transport.
- Transport bar per DESIGN.md: play/pause, scrub (Slider wired to seek intents), position/duration in mono, CC, fullscreen, panel toggle — all echo-driven.
- Delete `.btn-primary`, `.btn-ghost`, `.input` classes from `app.css` — V1 primitives fully retired.

**Acceptance:** AC-1.6/1.7 via M3 (old link terminal-invalid, joined guest survives, new link works); M2 end flow = AC-5.2; M4 flow = AC-2.7; panel collapse survives reload; `grep -rn 'btn-primary\|btn-ghost\|\.input' web/src` empty; dot tooltip shows on hover and focus.

**Do NOT:** no audio UI; no kick/co-host affordances (backlog); host controls must not sit next to play/pause (UX §5).

---

### DEMO GATE 3 — styled kernel + host powers

Re-walk F1 fully styled, plus F4: kill the host tab mid-playback → guest playback continues, guest can pause/seek/chat (AC-5.5) → host reopens from Home → HOST badge + Room menu restored. Then AC-1.6/1.7 regenerate walk. **Must observe:** theater layout (player dominates, strip auto-hides), dot tooltips, reconnect banner disabling inputs, all three dialogs, "Link copied" confirm.

---

## Chunk 8 — Audio: pipeline branch + S5 now-playing

**Files:** `internal/media/pipeline.go` + test, `internal/media/upload.go` + test, `web/src/components/AudioPlayer.svelte` (new), `web/src/pages/Room.svelte`, `web/src/components/MediaPickerDialog.svelte`, `web/src/pages/Admin.svelte`.

**Requirements (SPEC §9.9; ARCHITECTURE §4.2; UX S5; FR-35…37, FR-40):**
- Pipeline: probe result with **no video stream** → audio branch: aac/m4a/mp3 → move as-is; anything else → `-c:a aac` into `.m4a`; still one job at a time under `nice -n 19`; **no libx264 path for audio, ever**. `media.kind` (`video|audio`) set from the probe at ingest; `upload.go` stops accepting a client-supplied kind.
- Room switches player component on `kind`: AudioPlayer (S5) = now-playing anchor (title + duration, mono numerics), transport minus CC/fullscreen, side panel **open by default**; acquisition panel and status ladder identical to video.
- M1 and S7 label every item `video`/`audio`.

**Acceptance:** pipeline tests with lavfi-synthesized fixtures (sine-wave mp3/opus; skip pattern if ffmpeg missing): mp3 moved not transcoded, opus → `.m4a`, kind set correctly from probe; AC-6.1–6.4 manually; `go test ./internal/media/` green.

**Do NOT:** no waveform/artwork; no multiple audio tracks; do not touch the video pipeline tree.

---

## Chunk 9 — Hardening sweep + docs

**Files:** `internal/live/*_test.go`, `web/src/lib/ws.js`, `README.md`, `CLAUDE.md`, `.superpowers/sdd/progress.md`.

**Requirements:**
- Backfill edge tests: name freed after departure vs kept on reconnect (PRD §9), two host tabs coexist, backward-clock clamp still covered on both `watch_test.go` and `sync.test.js`, room-full terminal state, dead-vs-unknown-token no-oracle.
- `go test ./... -race` green; `gofmt -l internal cmd` empty; `cd web && node --test src/lib/*.test.js` green.
- Walk the full PRD §6 AC checklist in two browsers; log outcomes per-AC in `.superpowers/sdd/progress.md`.
- Docs: README env table gains `TOGETHER_ROOM_IDLE`; CLAUDE.md updated (routes, files, doc map — it still references deleted `docs/debt.md` and the V1 plan); verify restic backup units untouched; `git restore cmd/server/webdist/index.html` before the commit.

**Acceptance:** every AC in PRD §6 has a logged pass (or a logged, user-acknowledged deviation); `-race` clean.

**Do NOT:** no new features; no backlog items sneaking in.

---

### DEMO GATE 4 — v1 exit bar

US-6 (music room end-to-end from admin upload of an audio file) + US-7 (invite-code register incl. failed-attempt-keeps-code, resumable upload with mid-upload reload, processing→ready→failed states, delete) + the crash drill: restart the server with a live room → sign-in works, library and invite codes intact, room and chat gone (AC-5.6). **Must observe:** all three, on the production build (`./build.sh` binary, not the dev server).
