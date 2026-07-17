# together

Private, self-hosted watch-together for up to ten people. It runs as one Go + SQLite binary with an embedded Svelte 5 app. Rooms, guest sessions, chat, and playback state are intentionally in-memory; accounts, invite codes, and uploaded media persist.

Participants normally download or load their own copy of a video or audio file, then synchronize playback through WebSocket intents. Server streaming is an explicit fallback, never the default. Media is processed once at upload time; the app never live-transcodes.

## Development

```sh
# Backend API and embedded SPA on :8080.
ADMIN_USER=admin ADMIN_PASS=changeme go run ./cmd/server

# Vite development server on :5173; it proxies API, WebSocket, and media routes.
cd web && npm run dev
```

Useful environment variables:

| Variable | Default | Purpose |
|---|---:|---|
| `TOGETHER_ADDR` | `:8080` | HTTP listen address |
| `TOGETHER_DATA` | `./data` | SQLite database and media directory |
| `ADMIN_USER`, `ADMIN_PASS` | unset | Seed the first admin on first boot |
| `TOGETHER_ROOM_IDLE` | `30m` | Time a room remains available after its last WebSocket client leaves |

For sandbox smoke tests, use a free address such as `TOGETHER_ADDR=:18080` and `curl --noproxy '*'`.

## Verification

```sh
go test ./... -race
gofmt -l internal cmd
(cd web && node --test src/lib/*.test.js)
(cd web && npm run build)
```

`npm run build` writes generated assets under `cmd/server/webdist/`. Restore its committed `index.html` placeholder before committing frontend build output:

```sh
git restore cmd/server/webdist/index.html
```

## Production

Install ffmpeg and Caddy, then build the self-contained binary:

```sh
./build.sh
```

Copy `together` to `/usr/local/bin/`, provision `/var/lib/together` for the service user, set `ADMIN_USER` and `ADMIN_PASS` in `/etc/together.env` for the first boot, then install the units in `deploy/` and configure `deploy/Caddyfile` with the deployment domain.

## Documentation

| File | Purpose |
|---|---|
| `specs/001-core/SPEC.md` | Product scope and constraints |
| `specs/001-core/PRD.md` | User stories and acceptance criteria |
| `ARCHITECTURE.md` | Durable and in-memory contracts, API, and wire protocol |
| `UX.md` / `DESIGN.md` / `design.md` | Screen behavior, component adoption, and visual tokens |
| `CONVENTIONS.md` | Code, test, commit, and UI rules |
| `CLAUDE.md` | Maintainer onboarding and operational hazards |
| `docs/research/2026-07-03-prior-art-and-process.md` | Research and decision log |
