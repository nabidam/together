---
status: gate-passed
---

# SPEC.md — Together V2: Ephemeral Rooms + Local Playback

Refactor of the shipped V1 (see `docs/superpowers/specs/2026-07-03-together-app-design.md`). Reuses the server-authoritative sync engine, auth, and media pipeline; replaces persistent rooms with ephemeral ones and adds local-file playback.

## 1. Core promise

Watch or listen together in perfect sync, on a tiny server that never has to stream the media in real time.

## 2. Kernel

1. **Guest room access** — a guest opens the room's invite link, picks a display name, and is in — no account. Hosts are existing account users. One reusable link per room, valid while the room is open; host can regenerate it to revoke access. Name collisions get a suffix.
2. **Local-file playback** — user downloads the room's media from the server (one-time authed GET, **saved to disk then re-selected** — never buffered in-browser, so multi-GB files don't OOM the tab and the download stays resumable) *or* loads a copy they already have. Both paths converge on a file picker → `blob:` objectURL. Exact byte-size match check on file select (size-only; content-hash is backlog); mismatch warns and blocks readiness. Player arms only after a user gesture (autoplay policy). Participant list shows status dots: Downloading / File Ready / In Sync. Readiness is **advisory** — the host may play regardless; laggards sync when their file is ready.
3. **Server-authoritative sync** — unchanged `watch.go` state machine: clients send play/pause/seek intents over WS, server broadcasts absolute state, clients drift-correct (>1s hard seek, >0.15s rate nudge). During playback the server sends **state only — zero media bytes**. Streaming playback (existing Range-request path) stays as a fallback source.
4. **Room-lifetime chat** — text + emoji side panel, held in server memory, dies with the room.
5. **Room lifecycle** — host creates a room, picks media from the persistent library, ends it explicitly; rooms auto-close after 30 min empty. Room state (chat, presence, playback) is in-memory only.

### Kernel journey

Host signs in → creates room → picks a movie from the library → copies the invite link → sends it to partner. Partner opens the link, types a name, joins without an account → clicks Download Media (or loads their own copy) → size check passes → status flips to File Ready. Host presses play → both play in sync. Partner scrubs → both jump. Partner's connection drops → they reopen the link → `hello` restores position mid-scene. They chat about the ending. Host ends the room → chat and room are gone; the media library and the host's account remain.

## 3. v1 / Backlog

**v1:** kernel + music playback (same sync machinery, audio-only UI — needs a pipeline **audio-only branch** and a `kind` column, not just a UI skin) + admin upload pipeline otherwise unchanged.

**Backlog (ranked):** 1. Collaborative canvas (separate room view, stroke replay for late joiners, 50ms client-side batching, PNG export, host clear). 2. Kick user. 3. Co-host promotion. 4. Emoji picker. 5. Content-hash file matching. 6. Guest link expiry options.

## 4. Edge cases

- **Wrong local file:** size mismatch → warn, block File Ready.
- **Host disconnects:** room and playback continue (sync is server-owned); host reclaims powers on rejoin. No host auto-transfer.
- **Server crash:** ephemeral rooms lost — accepted; library and accounts persist.
- **Late join / reconnect:** stateless recovery — `hello` carries full room state; player seeks to projected position.
- **Guest downloads mid-session:** allowed; plain authed GET, no special casing.

## 5. Non-functional + tech constraints

2 vCPU / 2 GB VPS; ≤10 concurrent users. Exactly three Go deps (`coder/websocket`, `modernc.org/sqlite`, `x/crypto`); stdlib routing/config/logging — no Gin/Viper/Zap. Never live-transcode. WS carries state only during playback. A11y floors per `design.md`: contrast ≥4.5:1, ≥44px touch targets, body ≥15px, reduced-motion respected.

## 6. Tech stack

Backend unchanged: Go monolith, SQLite (WAL), embedded SPA, one static binary behind Caddy. Frontend: Svelte 5 SPA (Vite, embedded — not SvelteKit), **full shadcn-svelte adoption** on the existing Tailwind v4 setup — V1's `.btn-primary`/`.input`/`.card` primitives are retired in favor of shadcn components, all re-skinned to NxCode tokens (accepted dep/restyle ceiling); lucide icons.

## 7. Design direction

Design system: **NxCode (`design.md`) stays the single token source** — dark only, Inter + JetBrains Mono, cyan focus rings. shadcn-svelte components are skinned to NxCode tokens; no raw hex, no emoji in UI chrome. Personality: intimate, focused, theater-first — player dominates the viewport, chat/participants in a collapsible side panel.

## 8. Out of scope

Real-time media streaming as the primary path, live transcoding, host auto-transfer, guest accounts, canvas (v1), E2E encryption, mobile apps, >10 users.

## 9. Resolved design decisions

Settled via design grilling; these bind implementation. Numbered by concern.

1. **Guest auth / identity.** Room token = gate-only, ≥128-bit crypto-random (same generator as session tokens); regenerate swaps it, old links 404. Name submit (`POST /api/rooms/join {token, name}`) mints a separate **in-memory guest session** — HttpOnly cookie carrying `guestId` + `roomId`, dies with the room. New `RequireRoom` middleware accepts **either** an account session **or** a guest session scoped to *that* room; it wraps only `/ws/{id}` and the media/download/subs routes. Account routes (`/api/rooms`, admin, upload) stay account-only. The guest cookie survives a socket drop → reconnect restores the **same** participant (no new presence entry, no re-suffix); `hello` replays state.

2. **Room state.** Pure in-memory `map[roomID]*Room` under the existing per-room mutex. The V1 `activities` `state_json` checkpoint **and** boot-time reclaim are deleted. SQLite keeps only library / accounts / sessions / invites. Server crash loses rooms + chat (accepted, §4); library + accounts persist. Because nothing checkpoints, a per-room `recover()` in the hub goroutine is load-bearing — a single room's panic must not take the process down.

3. **Local-file acquisition.** Load-own-copy is primary. Download Media = plain authed GET with `Content-Disposition: attachment` → browser saves to disk → user re-selects via the same file input (Range/resume free from `http.ServeFile`). No in-browser Blob buffering. Both paths end at `createObjectURL` → `<video>`/`<audio>.src`. Size check: `GET /api/rooms/{id}/meta` (shape in `ARCHITECTURE.md` §4.3) supplies `sizeBytes`; exact `file.size` match → File Ready, mismatch → warn + block. Size-only (content-hash = backlog).

4. **Readiness.** Advisory, never gated. Server never blocks `play` on readiness (`watch.go` unchanged). Per-participant dot ladder, client-reported: **Downloading** (acquiring / no file) → **File Ready** (size-matched file loaded, player not yet armed) → **In Sync** (armed + `<video>` tracking projected position within the >1s / >0.15s drift bands). Mid-movie joiner un-special-cased: armed-empty player + own Downloading dot while others watch.

5. **Room lifecycle.** Empty = zero open WS connections (a dropped-but-cookie'd guest does **not** keep it warm). Per-room `time.Timer` on the struct (mutex-guarded): last connection closes → `Reset(30m)`, any join → `Stop()`, fire → teardown. Teardown (under hub lock): broadcast `room_closed`, close sockets, cancel timer, delete from map, drop chat/presence/guest sessions, forget token. Host "end" = immediate teardown regardless of who's connected. Host-only-then-leaves arms the timer (host owns *reclaim*, not *keep-alive*).

6. **Chat + wire protocol.** Chat = per-room 200-message ring buffer (drop-oldest), no DB; `GET /api/rooms/{id}/messages` and its table are deleted; `hello` carries `chat[]`. Deltas from V1 wire: **+1 inbound** `{type:"status", state:"downloading|file_ready|in_sync"}` — kept out of the `intent`/`watch.go` path. `presence` entries gain server-owned `{name, isHost, isGuest, status}`. Chat frame gains neutral `name` (+ `isGuest`) instead of overloading `username`. `createdAt` stays unix **seconds**. `start`/`end`/`ping`/`pong`/`activity`/`error` unchanged.

7. **Frontend.** Full shadcn-svelte adoption (Button/Input/Card/Dialog/DropdownMenu/Tooltip…), V1 CSS primitives retired, everything re-skinned to `design.md` tokens — no raw hex, NxCode stays the single token source; dep + restyle cost is an accepted ceiling. Join route `#/join/{token}` renders **before** the `/api/me` gate in `App.svelte` (anonymous visitor short-circuits ahead of the auth check); hash-router ethos unchanged, still no router dep.

8. **Player.** Video src = local `blob:` objectURL (drop `crossorigin`, inert on `blob:`); drift-correct loop, intents, `PositionAt` mirror all unchanged. Subtitles stay server-loaded via `<track src="/media/{id}/subs/{sid}">` — same-origin, guest cookie rides the fetch under `RequireRoom`. Streaming fallback (existing Range path) is an **explicit** "play from server" opt-in, never automatic (automatic would silently make the VPS stream real-time — the thing §1 forbids). Arm gesture = play overlay on the player until first user gesture.

9. **Music.** ffprobe **audio-only branch** (no video stream): web-friendly audio (aac/m4a, mp3) → move as-is; else transcode to aac in `.m4a` (`-c:a aac`, one job at a time, `nice -n 19`, never live). No libx264 path for audio. `media.kind` already ships in the V1 schema, but with values `movie|music`; V2 renames the vocabulary to `video|audio` (set from ffprobe at ingest, no longer client-supplied) via an idempotent boot-time `UPDATE` in the cutover — the schema's first post-release change (retires the `db.go` migration-note ponytail; no framework yet). UI = now-playing panel (title, transport, dots, chat), player switched on `kind`. Local-file flow identical to video.

10. **Host powers.** Regenerate link = revoke-**future**-only: old link → 404 at the join gate, already-connected guests persist (hard-kick = backlog #2, not v1). `isHost` computed live per connection (account session userID == room creator); two host tabs may coexist, harmless. Name collision checked against **currently-connected** names → suffix `(2)`/`(3)`; a reconnecting guest keeps their guestId-bound name (no re-suffix); a departed name frees up.

11. **Public attack surface.** `/api/rooms/join` and its pre-join peek `GET /api/rooms/join/{token}` are the only unauthenticated endpoints beyond login/register. Guards proportional to scale: crypto-random token (unguessability replaces a rate-limiter); name bound ≤32 chars + strip control chars + reject empty; per-room participant cap (12). Join only succeeds against a live room whose token matches (dead room → 404). Clean cutover: idempotent `DROP TABLE IF EXISTS` for `rooms` / `activities` / DB-chat on boot (alongside the `kind` value normalization, §9.9); library/accounts/sessions/invites untouched. No migration of V1 persistent rooms.
