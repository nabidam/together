# CLAUDE.md

This file provides maintainer guidance for this repository.

## What this is

"Together" is a private, self-hosted watch-together app for a long-distance couple (≤10 users). It is a Go monolith + SQLite + embedded Svelte 5 SPA, deployed as one binary behind Caddy on a 2-vCPU / 2 GB VPS. The current V2 design has ephemeral in-memory rooms, guest invite sessions, local-file-first video/audio playback, and an in-memory chat ring.

## Commands

```bash
# Backend dev server (API + embedded SPA on :8080)
ADMIN_USER=admin ADMIN_PASS=password go run ./cmd/server
# Env: TOGETHER_ADDR (default :8080), TOGETHER_DATA (default ./data),
# TOGETHER_ROOM_IDLE (default 30m), ADMIN_USER/ADMIN_PASS (seed admin, first boot only)

# Frontend dev with hot reload (proxies /api, /ws, /media to :8080)
cd web && npm run dev

# All Go tests (media fixtures need ffmpeg/ffprobe on PATH)
go test ./... -race
# Single package / single test
go test ./internal/live/ -v -run TestTwoHostTabsCoexist
# Frontend logic tests (plain node, no framework)
cd web && node --test src/lib/*.test.js

# Production build (SPA → cmd/server/webdist → embedded in static binary ./together)
./build.sh

# Format check (CI-level expectation: empty output)
gofmt -l internal cmd
```

In this sandbox, use `curl --noproxy '*'` and a free port (`TOGETHER_ADDR=:18080`) when smoke-testing; 8080 is often taken.

## Critical repo hazard

`cmd/server/webdist/index.html` is a committed placeholder (so `go build` works without a frontend build); the rest of `webdist/` is gitignored. Every `npm run build` overwrites it. Before any commit after building the frontend, run `git restore cmd/server/webdist/index.html`.

## Hard constraints

- **Go dependencies are exactly three:** `github.com/coder/websocket`, `modernc.org/sqlite`, and `golang.org/x/crypto`. Everything else is stdlib; tests use stdlib `testing` only.
- **Never live-transcode.** ffmpeg runs only inside the upload job worker (`internal/media/pipeline.go`), one job at a time, under `nice -n 19`.
- **UI design tokens come only from `design.md`**, mapped once into `@theme` in `web/src/app.css`. No raw hex in components. Dark only; Inter + JetBrains Mono; Lucide icons; no emoji in UI chrome.
- **Ponytail discipline:** `// ponytail: <ceiling + upgrade path>` comments mark deliberate shortcuts. Read them before changing the bounded design.
- A11y floors: text contrast ≥4.5:1, cyan focus rings, ≥44px touch targets, body ≥15px, and `prefers-reduced-motion` support.

## Architecture

**Realtime sync is server-authoritative.** `internal/live/watch.go` is a pure state machine (`WatchState`, `Apply`, `PositionAt`) where position is projected from `position + (now − updatedAt) × rate`. Clients send `play`/`pause`/`seek` intents over WebSocket; `internal/live/hub.go` applies them under a per-room mutex and broadcasts absolute state. `web/src/lib/sync.js` mirrors `PositionAt`, including the backward-clock clamp; change both implementations and their tests together. A `Room` has a 200-message in-memory chat ring, connected clients, and an empty-room timer. `Hub` owns room and guest-session state; no other package may retain a room pointer.

**Clock sync:** clients ping every 10 seconds; `pong.serverTime` and the `ws.js` EMA offset are unix milliseconds. Chat `createdAt` is unix seconds. Never mix the units.

**Wire protocol:** JSON over `GET /ws/{roomId}`. Inbound frames are `chat`, `start`, `end`, `intent`, `ping`, `status`, and `leave`; outbound frames are `hello`, `presence`, `chat`, `activity`, `left`, `user_left`, `user_rejoined`, `room_closed`, `pong`, and `error`. Reconnect recovery is stateless: `hello` carries the current presence, activity, and chat ring. Frontend field names are hand-matched to `hub.go`; there is no schema codegen.

**Media pipeline:** admin uploads are resumable 8 MB chunks. `finish` enqueues a job; `pipeline.go` probes with ffprobe and either moves/remuxes supported media or transcodes once. It selects `video` or `audio` from the probe; audio never takes the libx264 path. Subtitles convert from `.srt` to `.vtt`. On boot the worker reclaims jobs stuck `running`. Stream/download routes use `http.ServeFile` as the opt-in fallback and retain range support.

**Auth and rooms:** account sessions use `together_session`; public invite joins mint in-memory `together_guest` sessions scoped to one room. `auth.Require` protects account routes. `live.RequireRoom` protects room metadata and WebSocket upgrades for an account or matching guest; `live.RequireRoomMedia` additionally limits a guest to its room's media. Room IDs and join tokens are crypto-random. Guest names are 1–32 chars after control stripping, suffix only against connected names, and remain stable through reconnects.

**Frontend shell:** the hash router (`web/src/lib/router.svelte.js`) deliberately has no dependency. `App.svelte` renders `#/join/{token}` before the `/api/me` account gate. Account users reach Home, Room, and Admin; guests only reach their joined room. `Room.svelte` owns V2 socket state and switches between `Player` and `AudioPlayer` by media kind. The built SPA is embedded via `//go:embed all:webdist` with an SPA fallback.

**DB:** a single SQLite file runs in WAL mode with idempotent schema setup in `internal/db/db.go`. V2 boot drops V1 persistent room/message/activity tables and normalizes media kinds from `movie|music` to `video|audio`. Do not reintroduce persistent room state without a spec and architecture change.

## Documentation map

- `specs/001-core/SPEC.md` — V2 product scope and constraints.
- `specs/001-core/PRD.md` — user stories and acceptance criteria.
- `specs/001-core/PLAN.md` and `TASKS.md` — gated implementation plan and execution ledger.
- `ARCHITECTURE.md` — source of truth for contracts, routes, and the wire protocol.
- `UX.md`, `DESIGN.md`, and `design.md` — screen behavior, component adoption, and NxCode tokens.
- `CONVENTIONS.md` — naming, testing, commit, and UI rules.
- `docs/research/2026-07-03-prior-art-and-process.md` — research and decision log.

## Testing conventions

TDD for anything with logic. Go integration tests run the real stack: httptest + real WebSocket dials + real SQLite in `t.TempDir()` + real ffmpeg fixtures via `-f lavfi` synthesis (skip when ffmpeg is absent). Frontend uses plain `node --test` only for pure library logic; component behavior requires a production build and the applicable human two-browser journey. Run `go test ./... -race`, `gofmt -l internal cmd`, and `cd web && node --test src/lib/*.test.js` before each implementation commit.
