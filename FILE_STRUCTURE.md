# FILE_STRUCTURE.md — Together (post-V2, specs/001-core)

Living document. Complete tree after executing `specs/001-core/PLAN.md`. **(new)** = created by V2; **(rewrite)** = path kept, contents substantially change; unmarked = carries over from shipped V1. Deletions at the bottom.

```text
together/
├── .gitignore
├── ARCHITECTURE.md                    # V2 technical truth (living)
├── CLAUDE.md                          # (rewrite, chunk 9) agent guidance
├── CONVENTIONS.md                     # (rewrite) naming, errors, folders, tests, commits
├── DESIGN.md                          # (new) NxCode adoption map for shadcn-svelte
├── FILE_STRUCTURE.md                  # (rewrite) this file
├── README.md                          # (rewrite, chunk 9) run instructions, env vars
├── UX.md                              # screens S1–S7, M1–M4, V1 view, flows F1–F4
├── build.sh                           # SPA build → webdist → static binary ./together
├── design.md                          # NxCode design system — single token source
├── go.mod                             # exactly: coder/websocket, modernc.org/sqlite, x/crypto
├── go.sum
│
├── cmd/server/
│   ├── main.go                        # (rewrite) env (+TOGETHER_ROOM_IDLE), mux: live routes,
│   │                                  #   RequireRoom wiring, embedded SPA + fallback
│   └── webdist/
│       └── index.html                 # committed placeholder — never commit built output
│
├── internal/
│   ├── auth/
│   │   ├── auth.go                    # argon2id, account sessions, invite codes
│   │   ├── auth_test.go
│   │   ├── http.go                    # login/register/logout/me, Require middleware
│   │   └── http_test.go
│   ├── db/
│   │   ├── db.go                      # (rewrite) idempotent DDL + V2 cutover
│   │   │                              #   (drops rooms/messages/activities; kind → video|audio)
│   │   └── db_test.go                 # (rewrite) cutover idempotence
│   ├── live/
│   │   ├── watch.go                   # pure sync state machine — unchanged
│   │   ├── watch_test.go              # unchanged (most-tested unit; keep it that way)
│   │   ├── hub.go                     # (rewrite) in-memory rooms, WS frames (hello/presence/
│   │   │                              #   status/room_closed), chat ring, empty timer,
│   │   │                              #   per-room recover; checkpointing deleted
│   │   ├── hub_test.go                # (rewrite) WS-dial protocol + timer + panic tests
│   │   ├── rooms.go                   # (new) room lifecycle HTTP, guest sessions, join,
│   │   │                              #   token regenerate, RequireRoom middleware
│   │   └── rooms_test.go              # (new)
│   └── media/
│       ├── upload.go                  # (rewrite) kind from probe, not client
│       ├── upload_test.go
│       ├── pipeline.go                # (rewrite) + audio branch (move-as-is / -c:a aac)
│       ├── pipeline_test.go           # (rewrite) + lavfi audio fixtures
│       ├── serve.go                   # (rewrite) room-auth media routes, /download,
│       │                              #   /api/rooms/{id}/meta helper, kind filter video|audio
│       └── serve_test.go              # (rewrite)
│
├── deploy/
│   ├── Caddyfile                      # TLS termination + reverse proxy
│   ├── together.service               # systemd unit
│   ├── backup.sh                      # nightly restic DB backup
│   ├── together-backup.service
│   └── together-backup.timer
│
├── docs/
│   ├── research/2026-07-03-prior-art-and-process.md
│   └── superpowers/specs/2026-07-03-together-app-design.md   # V1 spec (historical)
│
├── specs/001-core/
│   ├── SPEC.md                        # V2 spec (+§9 resolved decisions)
│   ├── PRD.md                         # product requirements (FR/NFR/AC)
│   ├── PLAN.md                        # (new) 9 chunks, 4 demo gates
│   └── TASKS.md                       # (phase 4 output — not yet written)
│
└── web/
    ├── index.html
    ├── package.json                   # (rewrite) + shadcn-svelte (accepted V2 ceiling)
    ├── package-lock.json
    ├── components.json                # (new) shadcn config; ui alias → src/components/ui
    ├── vite.config.js
    ├── public/
    │   ├── icon.svg
    │   └── manifest.webmanifest
    └── src/
        ├── main.js
        ├── App.svelte                 # (rewrite) #/join/{token} before the /api/me gate
        ├── app.css                    # (rewrite) @theme from design.md + shadcn var map
        │                              #   (+--radius-pill, --duration-*); V1 primitives deleted
        ├── pages/
        │   ├── Login.svelte           # (rewrite) S1, shadcn
        │   ├── Register.svelte        # (new) S2, split out of Login
        │   ├── Home.svelte            # (new) S3 — renamed from Rooms.svelte
        │   ├── Room.svelte            # (rewrite) S4/S5 shell: WS owner, kind switch
        │   ├── JoinGuest.svelte       # (new) S6 + terminal invalid/full states
        │   └── Admin.svelte           # (rewrite) S7 + kind column
        ├── components/
        │   ├── ui/…                   # (new, generated) shadcn-svelte primitives — vendor
        │   ├── MediaPickerDialog.svelte  # (new) M1
        │   ├── RoomStrip.svelte       # (new) leave / title / HOST badge / Room menu
        │   ├── RoomMenu.svelte        # (new) copy link · regenerate · end
        │   ├── EndRoomDialog.svelte   # (new) M2
        │   ├── RegenerateLinkDialog.svelte  # (new) M3
        │   ├── PlayFromServerDialog.svelte  # (new) M4
        │   ├── AcquisitionPanel.svelte      # (new) UX §3.4 incl. mismatch state
        │   ├── Player.svelte          # (rewrite) blob: src, arm overlay, drift loop
        │   ├── AudioPlayer.svelte     # (new) S5 now-playing
        │   ├── SidePanel.svelte       # (new) collapsible; participants + chat
        │   ├── Participants.svelte    # (new) dot ladder + tooltips
        │   ├── Chat.svelte            # (rewrite) ring-buffer history from hello
        │   └── RoomClosed.svelte      # (new) V1 terminal view
        └── lib/
            ├── router.svelte.js       # 8-line hash router — unchanged
            ├── ws.js                  # (rewrite) backoff, EMA offset, V2 frames
            ├── sync.js                # PositionAt mirror — unchanged (lockstep w/ watch.go)
            ├── sync.test.js           # unchanged
            ├── localfile.js           # (new) size check + objectURL helpers
            ├── localfile.test.js      # (new)
            ├── api.js                 # (rewrite) + rooms/join/meta wrappers
            └── upload.js              # resumable upload client (kind field dropped)
```

## Deleted by V2

```text
internal/api/rooms.go                  # DB-backed rooms → internal/live/rooms.go (chunk 1)
internal/api/rooms_test.go
web/src/pages/Rooms.svelte             # renamed → pages/Home.svelte (chunk 4)
.btn-primary / .btn-ghost / .input     # app.css primitives, retired for shadcn (chunk 7)
rooms / messages / activities          # SQLite tables, dropped at boot cutover (chunk 1)
```

## Runtime artifacts (never committed)

```text
cmd/server/webdist/*                   # built SPA (gitignored except placeholder index.html)
together                               # static binary from build.sh
data/                                  # SQLite (WAL) + media files under TOGETHER_DATA
```
