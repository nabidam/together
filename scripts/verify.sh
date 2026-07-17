#!/usr/bin/env sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

restore_webdist() {
  git -C "$ROOT" restore --source=HEAD -- cmd/server/webdist/index.html 2>/dev/null || true
}
trap restore_webdist EXIT HUP INT TERM

cd "$ROOT"
go test ./... -race
go vet ./...
test -z "$(gofmt -l internal cmd)"

cd web
npm ci
node --test src/lib/*.test.js
npm run build
