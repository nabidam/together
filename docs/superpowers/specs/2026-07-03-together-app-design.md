# "Together" — Long-Distance Relationship App — Design

**Date:** 2026-07-03 (as-built addendum §12: 2026-07-04)
**Status:** V1 IMPLEMENTED and merged to main (head `9e45542`). Assumptions A1–A5 confirmed by user during review. See §12 for deviations from this design as written.
**Scale target:** ≤10 users, single VPS (2 vCPU / 2 GB RAM)

## 1. Purpose

A private, self-hosted space for a long-distance couple (and close friends) to spend time together online: watch movies, listen to music, draw, and play — with playback synced in real time for everyone in the activity.

## 2. Assumptions (made while user was away — confirm or change)

| # | Assumption | Rationale |
|---|-----------|-----------|
| A1 | Fully custom app (not a Jellyfin/OpenTogetherTube fork) | User said "I want to build an app"; existing tools don't cover rooms + activities + couple features |
| A2 | Web app (responsive, installable PWA) | One codebase, works on laptop + phone; no app-store friction |
| A3 | Private instance: registration by invite code only | ≤10 known users; avoids abuse handling |
| A4 | Text chat in v1; voice/video via WebRTC in a later version | WebRTC is peer-to-peer → near-zero server load, but adds complexity; not needed for MVP |
| A5 | No live transcoding, ever | 2-core VPS cannot live-transcode; media is prepared once at upload time |

## 3. Language-agnostic architecture

### 3.1 Shape: single monolith

One application process + one reverse proxy + one database file + the filesystem. No microservices, no message broker, no Redis, no container orchestration. At 10 users, every added moving part costs RAM and operational effort and buys nothing.

```
Browser (SPA/PWA)
   │  HTTPS (REST for CRUD, WebSocket for realtime)
   ▼
Reverse proxy (Caddy or nginx)  ── serves /media/* directly from disk
   │                               (HTTP Range requests, sendfile, TLS)
   ▼
App server (monolith)
   ├─ Auth & roles (session cookies)
   ├─ Rooms & membership
   ├─ Activity engine (server-authoritative state per room)
   ├─ Media catalog (movies, music, subtitles)
   ├─ Chunked upload endpoint (resumable)
   └─ Job queue (in-process) → ffmpeg worker (max 1 concurrent, nice/ionice)
   │
   ▼
SQLite (WAL mode) + filesystem (/data/media, /data/uploads)
```

Why these choices on 2 GB RAM:

- **SQLite over Postgres:** zero extra process, zero RAM overhead, trivially backed up (one file). At 10 users, write contention is nonexistent. WAL mode allows concurrent reads during writes.
- **Reverse proxy serves media, not the app:** video streaming = the proxy doing `sendfile()` on range requests. App process never touches video bytes after upload. CPU cost of 10 simultaneous viewers ≈ zero.
- **In-process job queue:** transcode jobs survive restarts by being persisted in a SQLite table; a single worker goroutine/thread picks them up. No Redis/Celery.

### 3.2 Realtime sync model (the core of the product)

Server-authoritative state machine per activity. This is the proven model used by Jellyfin SyncPlay, Syncplay, and OpenTogetherTube.

- Each running activity holds a state document, e.g. for Watch Movie:
  `{ mediaId, paused, position, playbackRate, stateVersion, updatedAtServerTime }`
- Clients never change state directly. They send **intents** over WebSocket: `play`, `pause`, `seek(t)`, `changeSubtitle`, etc.
- Server applies the intent, increments `stateVersion`, and broadcasts the new state with a server timestamp to all participants.
- Each client computes expected position = `position + (now - updatedAtServerTime) * rate` (clock offset estimated via ping/pong, NTP-style). If local player drifts > ~0.5–1 s → hard seek; small drift → adjust playbackRate slightly (soft correction, no visible jump).
- **Join-in-progress:** new participant receives the current state doc and jumps to the live position.
- **Buffering (from Jellyfin SyncPlay's playbook):** client reports `buffering`; server may auto-pause the group ("Waiting for Alice…") and resume when all ready. Group-pause is a room setting.
- **Reconnect:** WebSocket drop → client reconnects, sends `lastStateVersion`, server replies with full current state. No event replay needed — state is small and absolute.

Anyone in the activity can control playback (per user requirement); an activity owner can optionally lock controls.

The same protocol drives every activity type — only the state document differs:
- **Music:** same as movie + `queue[]` (playlist, votable).
- **Drawing:** state = ordered stroke log; intents = `stroke`, `undo`, `clear`. Broadcast strokes as they happen; persist snapshot periodically so late joiners load snapshot + tail instead of full history.
- **Future games:** same envelope (`intent in, state out`), new payload schema. The activity engine is deliberately generic: `ActivityType` defines `validate(intent, state) → newState`.

### 3.3 Media pipeline (upload → playable)

The .mkv problem: browsers do not play .mkv. Handling happens **once, at upload**, never at watch time.

1. Admin uploads file via **chunked, resumable upload** (tus-style). Movies are GBs; a dropped connection must not restart the upload.
2. Server runs `ffprobe`:
   - Container .mp4 with H.264 video + AAC audio → **store as-is**. Cost: none.
   - .mkv with H.264 + AAC/AC3 inside → **remux** to .mp4 (`ffmpeg -c copy`). Cost: seconds, no CPU burn.
   - Anything else (HEVC, VP9, weird audio) → **transcode once** to H.264/AAC .mp4. Cost: hours at `nice 19`, one job at a time; UI shows "processing" until done.
3. Subtitles: accept .srt/.ass → convert to .vtt (browsers' native format). Store alongside movie; selectable in player. Also extract embedded subtitle tracks from .mkv during step 2.
4. Music: accept .mp3/.flac/.m4a/.ogg; browsers play mp3/m4a/ogg natively; .flac transcoded once to high-bitrate AAC or served raw (flag per file).
5. **Download button:** serves the processed file (proxy handles it). Users on weak connections download first, then join the activity with `localFile` mode — the player uses their local copy while still obeying sync state (Syncplay's model). Server then only carries tiny WebSocket messages during playback.

### 3.4 Auth & roles

- Session-cookie auth (HttpOnly, Secure, SameSite). Passwords hashed with argon2id. No JWT — sessions in SQLite are simpler and revocable.
- Registration requires a single-use invite code generated by Admin.
- Roles v1: **Admin** (manage users, invite codes, upload/delete media, delete any room) and **Member** (create/join rooms, start activities, use media). Room-level: **room owner** (kick, lock controls, close room) vs **participant**.

### 3.5 Rooms & activities model

- User creates a room (name, optional password / invite-only toggle). Others join from a room list or invite link.
- A room has: members (presence-tracked), persistent text chat, and **at most one active activity at a time** (v1 simplification — confirm).
- Any member starts an activity (e.g. Watch Movie → pick from catalog). Members get a "join activity" prompt; joining is opt-in (you can sit in the room chatting without watching).
- Activity ends → summary event to room chat ("You watched Amélie together — 1h 58m") → feeds the movie-log feature.

### 3.6 Data model (first cut)

```
users(id, username, password_hash, role, created_at)
invite_codes(code, created_by, used_by, expires_at)
sessions(id, user_id, expires_at)
media(id, kind[movie|music], title, original_filename, status[processing|ready|failed],
      file_path, duration, size_bytes, uploaded_by, created_at)
subtitles(id, media_id, lang, label, file_path)
rooms(id, name, owner_id, is_private, created_at)
room_members(room_id, user_id, joined_at)
messages(id, room_id, user_id, body, created_at)
activities(id, room_id, type, state_json, status[active|ended], started_by, started_at, ended_at)
activity_participants(activity_id, user_id, joined_at, left_at)
jobs(id, type, payload_json, status, attempts, error, created_at)
```

Live activity state lives in memory (authoritative) and is checkpointed to `activities.state_json` so a server restart can restore it.

### 3.7 Capacity check (2 vCPU / 2 GB)

| Consumer | Budget |
|----------|--------|
| OS + Caddy/nginx | ~250 MB |
| App process | 50–300 MB (language-dependent) |
| SQLite | in-process, negligible |
| One ffmpeg transcode job | ~300–500 MB, niced — only during uploads |
| 10 viewers streaming same file | ~0 CPU (sendfile), bandwidth-bound |

Real constraints to watch: **disk space** (movies are 1–8 GB each — VPS disk size is an open question) and **uplink bandwidth** (10 concurrent streams × ~5 Mbps = 50 Mbps; typically fine, and the download-first mode removes this entirely).

## 4. Approaches considered

**A. Custom monolith (recommended, described above).** Full fit for requirements, smallest operational footprint, sync logic is a well-understood ~small state machine.

**B. Custom app + Jellyfin as media backend.** Jellyfin handles library/transcoding/subtitles; our app does rooms/activities and drives Jellyfin's API. Rejected: two systems on 2 GB is tight, Jellyfin's transcoder is the exact thing we must avoid, and integrating auth across both is awkward.

**C. Fork OpenTogetherTube.** Rooms + sync exist already. Rejected: built around external URLs (YouTube etc.), not owned uploads; adding auth/roles/music/drawing/couple-features fights its architecture; we'd own a fork of a codebase we didn't design.

## 5. Tech stack (decided 2026-07-03)

**Backend: Go.** Single static binary at ~30–80 MB RSS — the strongest fit for the 2 GB VPS. Standard library covers HTTP routing (`net/http`, Go 1.22+ patterns), chunked uploads, and `os/exec` for ffmpeg. Minimal dependency set: one WebSocket library, one SQLite driver. The built frontend is embedded in the binary via `embed.FS`, so deployment is one file + Caddy + systemd.

Rejected: Node/TypeScript and Python (FastAPI) — both workable at this scale but 2–5× the memory and a heavier ops story for no capability gain. Full comparison in `docs/research/2026-07-03-prior-art-and-process.md` §5.

**Frontend: Svelte 5 + Vite + Tailwind CSS v4**, served as a PWA. Smallest runtime among the major frameworks; design tokens from `design.md` map directly to Tailwind `@theme` CSS variables. Media via native `<video>`/`<audio>` elements (hls.js only if HLS ever becomes necessary); drawing via the native Canvas API. No player framework, no canvas library.

**Implementation discipline:** all implementation follows the **ponytail** skill (full level) — stdlib/native-platform first, no speculative abstraction, shortest working diff, deliberate shortcuts marked with `ponytail:` comments and harvested via `/ponytail-debt`. Implementation sessions load `ponytail:ponytail` before writing code.

## 6. Versioned scope

**V1 (MVP):** invite-code auth, admin media upload (movie + optional subtitle), media pipeline (remux/transcode-once), rooms with chat, Watch Movie activity with full sync (play/pause/seek by anyone, join-in-progress, reconnect), presence.

**V2:** Listen to Music activity (queue), download-before-watch local mode, drawing canvas activity, buffering group-pause.

**V3 (fun layer):** movie/music log with ratings & stats, watch wishlist, shared countdown to next visit, dual timezone clocks, floating emoji reactions during playback, daily question, scheduled watch dates, "touch" ping widget.

**Later:** WebRTC voice chat overlay, URL/torrent import, more games (trivia, guess-the-drawing, chess), radio/DJ mode.

## 7. Error handling

- **WS disconnect:** auto-reconnect with backoff; full state resync on reconnect (stateless recovery, no replay).
- **Upload failure:** resumable chunks; orphaned partial uploads garbage-collected after 24 h.
- **Transcode failure:** job marked `failed` with ffmpeg stderr tail; visible to Admin with retry button.
- **Server restart mid-activity:** activity state checkpointed to DB every few seconds and on every intent; clients reconnect and resume at correct position.
- **Disk full:** upload endpoint checks free space before accepting; Admin dashboard shows disk usage.

## 8. Testing

- Unit tests: sync state machine (intents → state transitions, drift math, join/reconnect paths) — this is the core, test it hard.
- Integration: auth flows, upload → probe → remux pipeline (small fixture files), room lifecycle.
- Manual/E2E: real playback sync across two browsers (automated E2E for media sync is possible with Playwright later).

## 9. Open questions for user

1. ~~Tech stack preference~~ → decided: Go + Svelte (§5).
2. VPS disk size? Determines how much media fits and whether we need an "auto-delete watched" policy.
3. Phone usage important for v1 (PWA polish) or laptop-first?
4. One active activity per room (v1) acceptable?
5. Which V3 fun features excite you most (prioritize)?

## 10. UI design direction — hyper-futuristic minimal

**Source of truth: `design.md` (NxCode design system) at repo root.** All colors, typography, radii, spacing, motion values come from its tokens — never hardcode hex values in components; map tokens to Tailwind `@theme` CSS variables once.

Independent validation: a ui-ux-pro-max design-system query for this product class recommended "Dark Mode (OLED), terminal dark + success green, minimal glow" — converging with NxCode. Its font/palette suggestions were discarded in favor of design.md.

**Concept: a calm command center for two.** The app reads like a beautifully engineered terminal the couple SSH'd into together. Hyper-futuristic comes from precision and restraint — not gradients or chrome.

- **Dark only.** Graphite ramp from design.md (`#060709` → `#0A0C0F` page → `#1A1F26` cards). No light mode (NxCode is dark-native; halves theming work).
- **Structure by hairline.** 1px `#2C343F` borders carry layout; flat fills; no heavy shadows.
- **Green = alive.** Signal green (#22D86B) reserved for presence and liveness: partner-online dot with soft glow, "activity live" indicator, primary action. The partner's presence dot is the emotional center of the UI — it glows like a cursor when they're online.
- **Cyan = data/focus.** Links, focus rings, seek-position markers.
- **Mono voice.** JetBrains Mono eyebrows (`// ROOM — MOVIE NIGHT`), timestamps, durations, sync status (`● SYNCED · 3 WATCHING`, `▸ 01:23:45`). All numerics monospaced.
- **Motion**: 120/200/360 ms, `cubic-bezier(.16,1,.3,1)`, opacity/transform only, no bounce, `prefers-reduced-motion` respected.
- **Icons**: Lucide, 1.75px stroke. No emoji in chrome (chat messages may contain user emoji/reactions — that's content, not chrome).

**Key screens:**
- **Rooms list** — command-center: room cards with mono status line (members online, active activity), one green "create room" primary action.
- **Room view** — activity stage (large) + chat rail (right, collapsible on mobile); member presence dots in header.
- **Theater mode** — when Watch Movie runs: chrome fades to near-black, custom minimal player controls (mono timecode, hairline seek bar with participants' buffer states), sync status pill top-right; controls auto-hide, reappear on pointer/touch.
- **Admin** — upload queue rendered like a build pipeline: `[##########----] transcoding · 42% · nice 19`, mono throughout, failed jobs red with stderr tail + retry.

**UX floors (non-negotiable, from ui-ux-pro-max):** contrast ≥4.5:1 for text on graphite; visible cyan focus rings; touch targets ≥44px; body ≥15–16px; skeletons over spinners past 300ms; `min-h-dvh` not `100vh`; no horizontal scroll at 375px.

## 11. Process log

Research, decision rationale, queries, and tooling recorded in `docs/research/2026-07-03-prior-art-and-process.md` for reproducibility.

## 12. As-built deviations (V1, 2026-07-04)

Implementation followed this spec via `docs/superpowers/plans/2026-07-03-together-v1.md` (14 tasks, subagent-driven, per-task + final review). Deliberate deviations, all reviewed and approved:

1. **§3.1 "reverse proxy serves media":** media is served by the Go process via `http.ServeFile` behind session auth, Caddy is a pure TLS proxy. Proxy-served files would have bypassed auth; stdlib gives Range/206 + sendfile anyway. Caddy auto-sets `X-Forwarded-Proto`, which drives the cookie's conditional `Secure` flag.
2. **§3.5 room membership:** no `room_members` table — every authenticated user may enter any room (instance is invite-gated, A3). Room privacy deferred (ponytail note in `internal/api/rooms.go`).
3. **§3.6 schema:** `invite_codes.expires_at`, `activity_participants`, and `sessions.id` (token is the PK) dropped as unneeded for V1; `media.error` added for failed-pipeline surfacing.
4. **Presence payload** includes `role` in addition to id/username (additive, harmless).
5. **§3.2 checkpoint cadence:** state checkpointed on *every* intent instead of "every few seconds" — simpler and stronger; restart-restore projects live position losslessly.
6. **§7 partial:** disk-free check, orphaned-upload GC, admin disk-usage display, and transcode retry *button* were not built in V1 — tracked in `docs/debt.md` (worker does reclaim crashed jobs at startup).

Hardening added beyond spec: backward-clock clamp in the sync state machine (server + JS mirror), transactional invite redemption, upload continuation tokens surviving page reloads, orphaned-job reclaim, SIGTERM fast-exit.

Full per-task review trail: `.superpowers/sdd/progress.md`. Current debt: `docs/debt.md`. Agent onboarding: `CLAUDE.md`.
