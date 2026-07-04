# together

Private watch-together app for two. Go + SQLite + Svelte, one binary on a 2GB VPS. Synced movie playback in rooms with chat and presence; admin uploads .mp4/.mkv + subtitles, processed once by ffmpeg (never live-transcoded).

## Docs

| File | What |
|------|------|
| `CLAUDE.md` | Agent/developer onboarding: commands, architecture, constraints, hazards |
| `docs/superpowers/specs/2026-07-03-together-app-design.md` | Design spec; §12 = as-built deviations |
| `docs/superpowers/plans/2026-07-03-together-v1.md` | Executed V1 implementation plan |
| `docs/research/2026-07-03-prior-art-and-process.md` | Prior-art research + process/decision log |
| `docs/debt.md` | Deferred work, accepted ceilings, V2 scope |
| `design.md` | NxCode design tokens — source of truth for all UI styling |

## Dev
    ADMIN_USER=admin ADMIN_PASS=changeme go run ./cmd/server   # api on :8080
    cd web && npm run dev                                       # ui on :5173, proxies to :8080

## Test
    go test ./... && (cd web && node --test src/lib/sync.test.js)

## Deploy (2-core/2GB VPS)
1. Install ffmpeg + caddy. `./build.sh`, copy `together` to `/usr/local/bin/`.
2. `useradd -r together && mkdir -p /var/lib/together && chown together /var/lib/together`
3. `/etc/together.env`: `ADMIN_USER=…`, `ADMIN_PASS=…` (first boot only).
4. Install `deploy/together.service`, `deploy/Caddyfile` (set your domain), enable both.
