# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

"Together" — a private, self-hosted watch-together app for a long-distance couple (≤10 users). Go monolith + SQLite + embedded Svelte 5 SPA, deployed as one binary behind Caddy on a 2-vCPU / 2 GB VPS. V1 (synced movie watching, rooms, chat, admin uploads) is complete and merged.

## Commands

```bash
# Backend dev server (API + embedded SPA on :8080)
ADMIN_USER=admin ADMIN_PASS=password go run ./cmd/server
# Env: TOGETHER_ADDR (default :8080), TOGETHER_DATA (default ./data), ADMIN_USER/ADMIN_PASS (seed admin, first boot only)

# Frontend dev with hot reload (proxies /api, /ws, /media to :8080)
cd web && npm run dev

# All Go tests (~10s; media package synthesizes real ffmpeg fixtures — ffmpeg/ffprobe required on PATH)
go test ./...
# Single package / single test
go test ./internal/live/ -v -run TestStartReplacesActiveActivity
# Frontend logic tests (plain node, no framework)
cd web && node --test src/lib/sync.test.js

# Production build (SPA → cmd/server/webdist → embedded in static binary ./together)
./build.sh

# Format check (CI-level expectation: empty output)
gofmt -l internal cmd
```

In this sandbox: use `curl --noproxy '*'` and a free port (`TOGETHER_ADDR=:18080`) when smoke-testing; 8080 is often taken.

## Critical repo hazard

`cmd/server/webdist/index.html` is a **committed placeholder** (so `go build` works without a frontend build); the rest of `webdist/` is gitignored. Every `npm run build` overwrites it. **Before any commit after building the frontend:** `git restore cmd/server/webdist/index.html`. This has been accidentally committed multiple times.

## Hard constraints (from spec — do not violate)

- **Go dependencies are exactly three:** `github.com/coder/websocket`, `modernc.org/sqlite`, `golang.org/x/crypto`. Everything else stdlib. Tests use stdlib `testing` only (no testify).
- **Never live-transcode.** ffmpeg runs only inside the upload job worker (`internal/media/pipeline.go`), one job at a time, under `nice -n 19`.
- **UI design tokens come only from `design.md`** (NxCode design system, repo root), mapped once into `@theme` in `web/src/app.css`. No raw hex in components. Dark only. Inter + JetBrains Mono. Lucide icons; no emoji in UI chrome.
- **Ponytail discipline:** laziest working solution; `// ponytail: <ceiling + upgrade path>` comments mark deliberate shortcuts. Read them before "fixing" the simplicity they document. Harvest with `/ponytail-debt`.
- A11y floors: text contrast ≥4.5:1, cyan focus rings, ≥44px touch targets, body ≥15px, `prefers-reduced-motion` respected.

## Architecture (the parts that span files)

**Realtime sync is server-authoritative.** The core is `internal/live/watch.go` — a pure state machine (`WatchState`, `Apply`, `PositionAt`) where position is projected from `position + (now − updatedAt) × rate`. Clients never mutate playback directly: they send intents (`play`/`pause`/`seek`) over WebSocket; the hub (`internal/live/hub.go`) applies them under a per-room mutex, checkpoints `state_json` to the `activities` table on every intent, and broadcasts absolute state. `web/src/lib/sync.js` is a **line-for-line JS mirror of `PositionAt`** (including the backward-clock clamp) — if you change one, change both, and both have tests. The player (`web/src/components/Player.svelte`) is echo-driven: user actions only send intents; the `<video>` element changes only in response to broadcast state; a 500ms loop drift-corrects (>1s hard seek, >0.15s playbackRate nudge).

**Clock sync:** clients ping every 10s; `pong` carries `serverTime` (unix **ms**); `ws.js` keeps an EMA offset. Chat `createdAt` is unix **seconds**. Don't mix the units.

**Wire protocol** (JSON over `GET /ws/{roomId}`): in — `chat`, `start`, `end`, `intent`, `ping`; out — `hello` (full state for join/reconnect), `presence`, `chat`, `activity`, `pong`, `error`. Reconnect recovery is stateless: `hello` carries everything, no event replay. Frontend field names are hand-matched to `hub.go` — there is no schema codegen.

**Media pipeline:** admin uploads via resumable 8MB chunks (`internal/media/upload.go`; client `web/src/lib/upload.js` keeps a localStorage continuation token keyed on filename+size). `finish` enqueues a `jobs` row; the worker (`pipeline.go`) probes with ffprobe and picks the cheapest path: mp4/h264/aac → move as-is; h264+aac in mkv → remux (`-c copy`); h264+other-audio → audio-only transcode; else full libx264. Subtitles `.srt` → `.vtt`. On boot the worker reclaims jobs stuck `running` (crash recovery). Streaming is `http.ServeFile` behind auth (Range/206 support free from stdlib) — deliberate deviation from spec §3.1 (proxy-served media would bypass session auth).

**Auth:** session cookie (HttpOnly, SameSite=Lax, Secure when TLS-terminated — detected via `r.TLS`/`X-Forwarded-Proto`), argon2id, sessions in SQLite. Invite-code registration, single-use, transactional (failed registration must not burn the code — there's a regression test). Roles: `admin` (uploads, invites, delete anything) / `member`. `auth.Require(db, adminOnly, handler)` wraps every protected route including WS and media bytes.

**Frontend shell:** hash router (8 lines, `web/src/lib/router.svelte.js` — deliberately no router dep), Svelte 5 runes throughout (no legacy `$:`), `App.svelte` switches Login/Rooms/Room/Admin on `/api/me` + hash. Built SPA is embedded via `//go:embed all:webdist` with an SPA fallback that serves index.html for unknown GET paths.

**DB:** single SQLite file, WAL, schema applied idempotently in `internal/db/db.go` (`CREATE TABLE IF NOT EXISTS` — no migration framework until the schema first changes post-release; ponytail note there).

## Documentation map

- `docs/superpowers/specs/2026-07-03-together-app-design.md` — the spec: architecture rationale, V1/V2/V3 scope, UI direction (§10), capacity math.
- `docs/superpowers/plans/2026-07-03-together-v1.md` — the executed V1 implementation plan (14 tasks).
- `docs/research/2026-07-03-prior-art-and-process.md` — prior-art research, decision log, reproducibility.
- `docs/debt.md` — known deferred work and accepted ceilings; read before starting V2.
- `.superpowers/sdd/progress.md` — task-by-task execution ledger with review outcomes (gitignored scratch).
- `design.md` — NxCode design tokens + rules; source of truth for all UI styling.

## Testing conventions

TDD for anything with logic. Go integration tests run the real stack (httptest + real WebSocket dials + real SQLite in `t.TempDir()` + real ffmpeg fixtures via `-f lavfi` synthesis — skip pattern if ffmpeg missing). Frontend: only pure logic (`sync.js`) is unit-tested via `node --test`; component behavior verified by build + manual two-browser checks. The sync state machine (`internal/live/watch_test.go`) is the most heavily tested unit in the repo — keep it that way.
