#!/usr/bin/env sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
OUT_DIR=${OUT_DIR:-"$ROOT/dist"}
TARGETS=${TARGETS:-"linux/amd64 linux/arm64"}
SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH:-0}

restore_webdist() {
  git -C "$ROOT" restore --source=HEAD -- cmd/server/webdist/index.html 2>/dev/null || true
}
trap restore_webdist EXIT HUP INT TERM

mkdir -p "$OUT_DIR"
OUT_DIR=$(CDPATH= cd -- "$OUT_DIR" && pwd)
rm -f "$OUT_DIR"/together_linux_*.tar.gz "$OUT_DIR"/SHA256SUMS

cd "$ROOT/web"
npm ci
npm run build

for target in $TARGETS; do
  goos=${target%/*}
  goarch=${target#*/}
  stage=$(mktemp -d)
  archive="$OUT_DIR/together_${goos}_${goarch}.tar.gz"

  (
    cd "$ROOT"
    GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
      go build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" -o "$stage/together" ./cmd/server
  )

  tar --sort=name --mtime="@$SOURCE_DATE_EPOCH" --owner=0 --group=0 --numeric-owner \
    -C "$stage" -czf "$archive" together
  rm -rf "$stage"
done

(
  cd "$OUT_DIR"
  sha256sum together_linux_*.tar.gz > SHA256SUMS
)

echo "release archives written to $OUT_DIR"
