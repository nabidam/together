#!/usr/bin/env sh
set -e
cd "$(dirname "$0")"
(cd web && npm ci && npm run build)
CGO_ENABLED=0 go build -ldflags="-s -w" -o together ./cmd/server
echo "built ./together ($(du -h together | cut -f1))"
