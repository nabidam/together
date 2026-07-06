# CONVENTIONS.md — Together

## Naming

### Go
- Packages: single lowercase word, no underscores (`state`, `catalog`, `live`, `api`).
- Exported identifiers: `CamelCase`; unexported: `camelCase`. No `Get` prefixes (`store.Room(id)`, not `store.GetRoom(id)`).
- Files: lowercase, one concern per file only when a file passes ~500 lines; otherwise fewest files wins.
- Tests: `_test.go` beside the code under test. Test names `TestSubject_Behavior` (e.g. `TestJoin_RejectsKickedUser`).

### JavaScript / Svelte
- Components: `PascalCase.svelte` in `web/src/components/`; route pages in `web/src/pages/`.
- Libs: lowercase `.js` in `web/src/lib/`; pure logic only — anything touching the DOM belongs in a component.
- Svelte 5 runes exclusively (`$state`, `$derived`, `$effect`). No legacy `$:` reactivity, no stores unless runes genuinely can't express it.
- Tests: `<name>.test.js` beside the lib, runnable with plain `node --test`.

### Wire protocol
- Event types: `SCREAMING_SNAKE` exactly as in `ARCHITECTURE.md` §4.2 (`JOIN_ROOM`, `CANVAS_UPDATE`, …).
- JSON payload fields: `camelCase` (`roomId`, `displayName`, `sizeBytes`).
- Field names are hand-matched between `internal/live/hub.go` and `web/src/lib/ws.js` — there is no codegen. Change both sides in the same commit, always.
- Time units: WebSocket `serverTime` is unix **milliseconds**; chat `timestamp` is unix **seconds**. Never mix.

## Error handling

### Go
- Wrap with context at each boundary: `fmt.Errorf("join room %s: %w", id, err)`. Lowercase messages, no trailing punctuation.
- HTTP handlers: errors become `http.Error` / JSON `{"error": "..."}` with a correct status code; never leak internal paths or stack traces to clients.
- WebSocket: recoverable client mistakes → `ERROR {code, message}` frame, connection stays open. Protocol violations (bad first frame, unparseable JSON repeatedly) → close.
- Canvas floods are dropped **silently** (no ERROR frames at draw frequency).
- No panics in request paths. Every WS read/serve goroutine has a `recover` at its top that logs and drops the connection.
- Logging: stdlib `log.Printf`, one line per event, `key=value` tail (`log.Printf("room created id=%s", id)`). No logging framework, no levels.

### JavaScript
- WS layer never throws into components: it exposes `connected` state and dispatches typed events; failures surface as UI states (offline overlay, toast), not exceptions.
- User-facing failures are non-blocking (toast/banner) unless the session is genuinely over (`KICKED`).

## Folder rules

- `cmd/server/` — main + embedded `webdist/` only. No business logic.
- `internal/state/` — pure in-memory data + one lock. No goroutines, no I/O, no imports from other internal packages.
- `internal/catalog/` — filesystem scan of `STATIC_MEDIA_PATH`. No knowledge of rooms.
- `internal/live/` — hub, WS protocol, playback state machine, timers. May import `state` and `catalog`.
- `internal/api/` — plain HTTP handlers. May import `state` and `catalog`, never `live`.
- `web/src/lib/` — pure logic; `web/src/components/` — leaf UI; `web/src/pages/` — routed views.
- New top-level directories require a spec change. Default answer is no.
- Dependency ceiling: `go.mod` contains `github.com/coder/websocket` and nothing else; `web/package.json` adds nothing beyond the existing Svelte/Vite/Tailwind toolchain. Stdlib/platform first, always.

## Test style

- Go: stdlib `testing` only — no testify, no mocks framework. Table-driven where cases are enumerable. Real stack in integration tests: `httptest.Server` + real `websocket.Dial` + `t.TempDir()` for catalog fixtures.
- Time in tests: inject durations/clocks (struct fields with defaults), never `time.Sleep`-and-hope. `go test ./... -race` must pass.
- Frontend: only pure lib logic is unit-tested (`node --test`, zero frameworks). Component behavior = `npm run build` + manual two-browser check, logged in `.superpowers/sdd/progress.md`.
- Every non-trivial branch (validation, role gates, limits) has a test that fails if the logic breaks. Trivial one-liners need no test.
- The sync state machine (`internal/live/watch.go` + `web/src/lib/sync.js`) keeps mirrored test suites — a change to one side without both suites updated is an incomplete change.

## Commit style

- Conventional prefixes matching existing history: `feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `chore:`.
- Imperative, lowercase after prefix, ≤72-char subject. Body only when the subject can't carry the "why".
- One commit per plan chunk (see `specs/001-core/PLAN.md`); each commit builds, tests green, `gofmt -l internal cmd` empty.
- **Before every commit after a frontend build:** `git restore cmd/server/webdist/index.html` (committed placeholder hazard).
- Deliberate shortcuts carry a marker in code, not in the commit message: `// ponytail: <ceiling + upgrade path>`.

## UI

- All styling tokens come from `design.md` via `@theme` in `web/src/app.css`. No raw hex in components. Dark only. Inter + JetBrains Mono. Lucide icons; no emoji in UI chrome.
- A11y floors: contrast ≥4.5:1, cyan focus rings, ≥44px touch targets, body ≥15px, `prefers-reduced-motion` respected.
