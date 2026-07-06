# FILE_STRUCTURE.md — Together (post-refactor, specs/001-core)

Complete tree after executing `specs/001-core/PLAN.md`. Files marked **(new)** are created by
the refactor; **(rewrite)** keep their path but change contents; unmarked files carry over.
Files deleted by the refactor are listed at the bottom.

```text
together/
├── .gitignore
├── ARCHITECTURE.md                  # V1 architecture (source spec for this refactor)
├── CLAUDE.md                        # (rewrite) agent guidance — commands/env updated
├── CONVENTIONS.md                   # (new) naming, errors, folders, tests, commits
├── FILE_STRUCTURE.md                # (new) this file
├── README.md                        # (rewrite) run instructions, env vars
├── build.sh                         # SPA build → embed → static binary
├── design.md                        # NxCode design tokens (source of truth for UI)
├── go.mod                           # (rewrite) single dep: github.com/coder/websocket
├── go.sum                           # (rewrite)
│
├── cmd/
│   └── server/
│       ├── main.go                  # (rewrite) env config, mux, graceful shutdown
│       └── webdist/
│           └── index.html           # committed placeholder — never commit built output
│
├── internal/
│   ├── api/
│   │   ├── rooms.go                 # (rewrite) POST /api/rooms, GET /api/media, GET /media/{id}
│   │   └── rooms_test.go            # (rewrite) httptest: create, list, Range/206, 404
│   ├── catalog/
│   │   ├── catalog.go               # (new) STATIC_MEDIA_PATH scan → items, id→path resolve
│   │   └── catalog_test.go          # (new)
│   ├── live/
│   │   ├── hub.go                   # (rewrite) WS protocol, fan-out, roles, migration, culling
│   │   ├── hub_test.go              # (rewrite) real WS dial tests per event family
│   │   ├── integration_test.go      # (new) full-stack scenario suite (Chunk 14)
│   │   ├── watch.go                 # playback state machine (Apply/PositionAt) — reused
│   │   └── watch_test.go            # reused, untouched
│   └── state/
│       ├── state.go                 # (new) Room/User/Stroke, Store, indices, limits
│       └── state_test.go            # (new)
│
├── deploy/
│   ├── Caddyfile                    # TLS termination + reverse proxy
│   └── together.service             # (rewrite) systemd unit — new env, no DB/backup refs
│
├── docs/
│   ├── debt.md                      # deferred-work ledger
│   ├── research/
│   │   └── 2026-07-03-prior-art-and-process.md
│   └── superpowers/
│       ├── plans/
│       │   └── 2026-07-03-together-v1.md
│       └── specs/
│           └── 2026-07-03-together-app-design.md
│
├── specs/
│   └── 001-core/
│       ├── PRD.md                   # product requirements (source spec)
│       └── PLAN.md                  # (new) ordered refactor chunks
│
└── web/
    ├── index.html
    ├── package.json
    ├── package-lock.json
    ├── vite.config.js
    ├── public/
    │   ├── icon.svg
    │   └── manifest.webmanifest
    └── src/
        ├── App.svelte               # (rewrite) hash-route switch Home/Room, no auth gate
        ├── app.css                  # design tokens via @theme — unchanged rules
        ├── main.js
        ├── components/
        │   ├── Canvas.svelte        # (new) drawing surface + toolbar + export + clear
        │   ├── Chat.svelte          # (rewrite) CHAT_MESSAGE/CHAT_BROADCAST, temp names
        │   ├── JoinGate.svelte      # (new) display-name prompt + validation
        │   ├── Participants.svelte  # (new) list, status badges, host controls
        │   ├── Player.svelte        # (rewrite) local-file playback, download, mismatch, drift
        │   └── SidePanel.svelte     # (new) collapsible panel (participants + chat)
        ├── lib/
        │   ├── api.js               # (rewrite) createRoom, getMedia only
        │   ├── canvas.js            # (new) stroke buffer, 50ms batching, normalization
        │   ├── canvas.test.js       # (new) node --test
        │   ├── router.svelte.js     # 8-line hash router — reused
        │   ├── sync.js              # (rewrite) PositionAt mirror, drift threshold 1.5s
        │   ├── sync.test.js         # (rewrite) clamp + threshold cases
        │   └── ws.js                # (rewrite) backoff, EMA offset, JOIN_ROOM, typed sends
        └── pages/
            ├── Home.svelte          # (new) create room, invite link
            └── Room.svelte          # (rewrite) theater layout, offline overlay, wiring
```

## Deleted by this refactor (Chunk 13)

```text
internal/auth/auth.go                # accounts/sessions — out of scope (PRD §8)
internal/auth/auth_test.go
internal/auth/http.go
internal/auth/http_test.go
internal/db/db.go                    # SQLite — state is memory-bound (PRD §7)
internal/db/db_test.go
internal/media/pipeline.go           # ffmpeg pipeline — no server-side transcoding
internal/media/pipeline_test.go
internal/media/serve.go              # replaced by internal/catalog + /media/{id}
internal/media/serve_test.go
internal/media/upload.go             # admin uploads — media is provisioned out-of-band
internal/media/upload_test.go
web/src/lib/upload.js
web/src/pages/Admin.svelte
web/src/pages/Login.svelte
web/src/pages/Rooms.svelte
deploy/backup.sh                     # nothing left to back up
deploy/together-backup.service
deploy/together-backup.timer
```

## Runtime artifacts (never committed)

```text
cmd/server/webdist/*                 # built SPA (gitignored except placeholder index.html)
together                             # static binary from build.sh
media/                               # STATIC_MEDIA_PATH default — operator-provisioned files
```
