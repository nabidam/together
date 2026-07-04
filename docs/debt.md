# Known debt & accepted ceilings

Snapshot after V1 merge (2026-07-04, head `9e45542`). Two sources: deliberate `// ponytail:` shortcuts in code (harvest live with `/ponytail-debt`), and review findings deferred at the final whole-branch review. Read this before starting V2 work.

## Deferred from final review (do these opportunistically)

| Item | Where | Why deferred |
|------|-------|--------------|
| Disk-free check before accepting uploads + orphaned-partial GC + admin disk-usage display (spec §7) | `internal/media/upload.go` | ~6 lines (`syscall.Statfs`); disk is the VPS's real constraint — do this early in V2 |
| Retry button for failed transcode jobs (spec §7 promised it) | admin UI + one endpoint | Startup reclaim covers crash case; manual retry still missing |
| Room movie-picker shows non-ready media to admins, fails silently on click | `web/src/pages/Room.svelte` | Filter `status === "ready"` or render the `error` frame |
| Chat history not refetched on WS reconnect — messages during disconnect invisible until reload | `web/src/pages/Room.svelte` | Refetch on `hello` |
| Rooms deleted while occupied live on in the hub (dangling room_id messages) | `internal/live/hub.go` | Trusted users; revisit with FK cascade decision |
| `.ass`/`.vtt` subtitle uploads stored as `.srt` (ffmpeg content-probes, usually works) | `internal/media/upload.go` | Preserve real extension when convenient |
| Download filename is `<id>.mp4`, not the title | `internal/media/serve.go` | Cosmetic |
| Chat limit: UI counts chars, server counts bytes (multibyte max-length messages dropped) | `Chat.svelte` / `hub.go` | Align on runes server-side |
| Focus-ring utility string duplicated 4× | `web/src/app.css` + components | Consolidate to `.focus-ring` next time app.css is touched |
| Backward-clock test doesn't assert `UpdatedAt` unchanged after regressed `Apply` | `internal/live/watch_test.go` | One assertion |

## Accepted ceilings (documented in code, not bugs)

- No migration framework — schema is `CREATE IF NOT EXISTS`; add numbered migrations at first post-release schema change (`internal/db/db.go`).
- Upload PATCH Stat-then-Write not atomic — single admin uploads sequentially; per-id lock if concurrent uploads ever happen (`internal/media/upload.go`).
- Resuming an upload the worker already processed re-uploads under a new id → possible duplicate row; admin deletes it (`web/src/lib/upload.js`).
- Subtitle glob ordering mislabels at ≥11 sidecars per movie (`internal/media/pipeline.go`).
- Slow WS consumers drop frames; state broadcasts are absolute so nothing is lost (`internal/live/hub.go`).
- No HTTP drain on SIGTERM — process exits, clients reconnect and resync by design (`cmd/server/main.go`).
- No service worker — offline mode meaningless for a live-sync app (`web/index.html`).
- Embedded-mkv subtitle track extraction deferred — sidecar `.srt` covers V1 flow (`internal/media/pipeline.go`).
- Room privacy/passwords deferred — whole instance is invite-gated (`internal/api/rooms.go`).
- Watch-only activity engine — introduce an activity interface when the music activity lands in V2 (`internal/live/hub.go`).
- `MemoryMax=1200M` in systemd covers the whole cgroup incl. ffmpeg children; untested under real transcode load (`deploy/together.service`).

## V2 scope (from spec §6)

Music activity (queue), download-then-play-local mode, drawing canvas activity, buffering group-pause. V3: fun layer (movie log + stats, countdown, timezone clocks, reactions, wishlist, daily question, "touch" ping).
