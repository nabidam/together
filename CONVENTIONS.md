# CONVENTIONS.md — Together

Living document. Applies to the V2 codebase (ephemeral rooms + local playback). Where this file and `ARCHITECTURE.md` disagree, ARCHITECTURE wins; fix the loser in the same commit.

## Naming

### Go
- Packages: single lowercase word (`db`, `auth`, `live`, `media`). No new packages without a spec change.
- Exported `CamelCase`, unexported `camelCase`. No `Get` prefixes (`hub.Room(id)`, not `hub.GetRoom(id)`).
- Files: lowercase, fewest files wins; split only past ~500 lines.
- Tests: `_test.go` beside the code. Names `TestSubject_Behavior` (`TestJoin_ReconnectKeepsName`).

### JavaScript / Svelte
- Components `PascalCase.svelte` in `web/src/components/`; routed views in `web/src/pages/`; shadcn-generated primitives in `web/src/components/ui/` (vendor — reskin via tokens, don't hand-edit logic).
- `web/src/lib/`: lowercase `.js`, pure logic only — DOM belongs in components.
- Svelte 5 runes exclusively (`$state`, `$derived`, `$effect`). No legacy `$:`, no stores unless runes can't express it.
- Tests: `<name>.test.js` beside the lib, plain `node --test`.

### Wire protocol
- Frame `type` strings: lowercase, `snake_case` when multiword, exactly as `ARCHITECTURE.md` §4.5 (`hello`, `presence`, `chat`, `activity`, `intent`, `status`, `room_closed`, `pong`, `error`, `start`, `end`, `ping`). Enum values likewise (`file_ready`, `in_sync`).
- JSON fields: `camelCase` (`roomId`, `sizeBytes`, `isGuest`, `createdAt`).
- Field names are hand-matched between `internal/live/hub.go` and `web/src/lib/ws.js` — no codegen. Change both sides in the same commit, always.
- Time units: `pong.serverTime` is unix **milliseconds**; chat `createdAt` is unix **seconds**. Never mix.

## Error handling

### Go
- Wrap with context at each boundary: `fmt.Errorf("join room %s: %w", id, err)`. Lowercase messages, no trailing punctuation.
- HTTP: correct status + `{"error": "human-readable message"}` — the one error shape everywhere. Never leak paths or stack traces. Join-token probes get an undifferentiated 404 (no live-room oracle).
- WebSocket: recoverable client mistakes → `error` frame, connection stays open. Repeated protocol violations → close that connection only.
- Panics: none in request paths by design, but every goroutine touching a room carries the per-room `recover()` (log with room id → tear down that room → process keeps serving). This is load-bearing (NFR-7) — never remove it.
- Logging: stdlib `log.Printf`, one line per event, `key=value` tail: `log.Printf("room closed id=%s reason=%s", id, reason)`. No framework, no levels.

### JavaScript
- `ws.js` never throws into components: it exposes connection state and dispatches typed messages; failures surface as UI states (reconnect banner, inline errors), not exceptions.
- While disconnected, intents and chat are visibly disabled — never silently swallowed (AC-3.6).
- Size mismatch and acquisition failures are inline states of the panel, never modals, and never auto-fall-back to streaming (FR-13).

## Folder rules

- `cmd/server/` — main, env config, mux wiring, embedded `webdist/`. No business logic.
- `internal/db/` — schema DDL + boot cutover only. Imports nothing internal.
- `internal/auth/` — accounts, argon2id, account sessions, invite codes, `Require` middleware.
- `internal/live/` — the room world: `watch.go` (pure: no I/O, no clock ownership — caller passes `now`), `hub.go` (rooms map, WS, broadcast, chat ring, timers), `rooms.go` (lifecycle, guest sessions, `RequireRoom`). **Only this package touches room state**; nothing else may hold a pointer into a room.
- `internal/media/` — upload, pipeline, serve. **Only `pipeline.go` invokes ffmpeg/ffprobe**, one job at a time, `nice -n 19`, ingest only — never live.
- New top-level directories require a spec change. Default answer is no.
- Dependency budget (binding): `go.mod` = exactly `github.com/coder/websocket`, `modernc.org/sqlite`, `golang.org/x/crypto`. `web/package.json` = the existing Svelte 5/Vite/Tailwind v4 toolchain + `lucide-svelte` + fontsource + `shadcn-svelte` and its transitive requirements (accepted V2 ceiling — nothing else rides in with it).

## Test style

- Go: stdlib `testing` only — no testify, no mock frameworks. Table-driven where cases enumerate. Integration tests run the real stack: `httptest.Server` + real `websocket.Dial` + real SQLite in `t.TempDir()` + real ffmpeg fixtures via `-f lavfi` synthesis (skip if ffmpeg missing).
- Time: inject durations (`TOGETHER_ROOM_IDLE`, struct fields with defaults) — never `time.Sleep`-and-hope. `go test ./... -race` must pass.
- Frontend: only pure lib logic is unit-tested (`node --test`, zero frameworks): `sync.js`, `localfile.js`. Component behavior = `npm run build` + manual two-browser checks logged in `.superpowers/sdd/progress.md`.
- The sync state machine (`internal/live/watch.go` ↔ `web/src/lib/sync.js`) keeps mirrored suites — changing one side without both suites updated is an incomplete change.
- Every non-trivial branch (validation, auth gates, limits, suffixing) has a test that fails if the logic breaks.

## Commit style

- Conventional prefixes: `feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `chore:`. Imperative, lowercase after prefix, ≤72-char subject.
- One commit per `specs/001-core/PLAN.md` chunk; each commit builds, tests green, `gofmt -l internal cmd` empty.
- **Before every commit after a frontend build:** `git restore cmd/server/webdist/index.html` (committed-placeholder hazard — has bitten multiple times).
- Deliberate shortcuts carry `// ponytail: <ceiling + upgrade path>` in code, not in the commit message. Read existing ponytail comments before "fixing" the simplicity they document.

## UI

- All styling values come from `design.md` (NxCode) via `@theme` in `web/src/app.css`; `DESIGN.md` is the adoption map for shadcn-svelte. No raw hex, px font sizes, or ad-hoc radii in components. Dark only. Inter + JetBrains Mono. Lucide icons; no emoji in UI chrome; sentence case everywhere.
- A11y floors: contrast ≥4.5:1, cyan (secondary token) focus rings, ≥44px touch targets, body ≥15px, `prefers-reduced-motion` respected.
