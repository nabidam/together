# Known debt & accepted ceilings

Snapshot after V1 merge (2026-07-04, head `9e45542`), updated after the 2026-07-05 hardening audit. Two sources: deliberate `// ponytail:` shortcuts in code (harvest live with `/ponytail-debt`), and review findings deferred at review time. Read this before starting V2 work.

## Fixed in the 2026-07-05 hardening audit

Whole-codebase review for correctness/robustness gaps. Landed (all tested, gofmt-clean, smoke-tested live):

1. Guarded five `rows, _ := d.Query(...)` sites that panicked the whole process on any DB error (rooms list, room messages, invites list, media subtitles, media delete).
2. Capped concurrent argon2id hashes at 2 (`internal/auth/auth.go`) — 64 MB/hash on an unauthenticated endpoint could breach `MemoryMax=1200M` and get the service OOM-killed.
3. `http.MaxBytesReader` (4 KB) on login/register/room-create bodies; username capped at 32 chars.
4. `finish` rolls media back to `uploading` if the job INSERT fails (was: stuck `processing` forever, no job, no retry path).
5. Pipeline resumes from `dst` when a crash lands between blob move and status flip (was: rerun marked media `failed` despite complete output — the idempotency claim was false for that window). Regression test: `TestProcessResumesAfterMoveCrash`. Duration now probed from output.
6. Media delete also removes the `uploads/` blob and subtitle sidecars (was: disk leak on the box's scarcest resource). `ServeRoutes` now takes `dataDir`.
7. Chat insert failures logged (was: silent history loss).
8. `ReadHeaderTimeout: 10s` (slowloris); deliberately no write/idle timeouts — they'd kill WS and media streams.
9. Expired-session GC at boot (`auth.GC`).
10. Index `messages(room_id, id)` — history query was a full scan of the only unbounded table.

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

## Found in the 2026-07-05 audit, deliberately not implemented

| Item | Where | Why deferred |
|------|-------|--------------|
| **No backups** — DB + media live on one VPS disk; the single biggest operational gap | ops (VPS) | Litestream, or nightly cron `sqlite3 .backup` + rsync of `media/`. Ops task, not code — needs owner decision on destination |
| ffmpeg has no timeout — one pathological file hangs the single worker until restart | `internal/media/pipeline.go` `run()` | Fix is `exec.CommandContext` + cap, but the right cap for a 2 h movie at `nice 19` is hours; owner picks the ceiling |
| WS reconnect never gives up — expired session mid-room hammers reconnects every 1–8 s forever | `web/src/lib/ws.js` | On repeated failures, check `/api/me` and redirect to login |
| No login rate limiting beyond the new hash semaphore | `internal/auth/http.go` | Invite-only private instance; argon2 already slow; add fail2ban at Caddy if wanted |
| Chat message can render twice if it arrives over WS while history fetch is in flight | `web/src/pages/Room.svelte` | Cosmetic, narrow window; dedupe by id when chat refetch-on-reconnect lands |

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
