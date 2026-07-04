# together

Private watch-together app for two. Go + SQLite + Svelte. Spec and plan in `docs/`.

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
