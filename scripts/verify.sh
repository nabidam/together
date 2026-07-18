#!/usr/bin/env sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
WEB_DIST_WAS_DIRTY=false

if ! git -C "$ROOT" diff --quiet -- cmd/server/webdist/index.html; then
  WEB_DIST_WAS_DIRTY=true
fi

restore_webdist() {
  if [ "$WEB_DIST_WAS_DIRTY" = false ]; then
    git -C "$ROOT" restore --source=HEAD -- cmd/server/webdist/index.html 2>/dev/null || true
  fi
}
trap restore_webdist 0 HUP INT TERM

cd "$ROOT"
go test ./... -race
go vet ./...
test -z "$(gofmt -l internal cmd)"

cd web
npm ci
node --test src/lib/*.test.js
npm run build

cd "$ROOT"
if command -v ffmpeg >/dev/null 2>&1 && command -v ffprobe >/dev/null 2>&1; then
  ./scripts/security-e2e.sh
else
  printf '%s\n' 'verify: skipping security-e2e (ffmpeg and ffprobe are required; release gate remains mandatory)'
fi
