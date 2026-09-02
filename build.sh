#!/usr/bin/env bash
# Build a UPX-compressed load-test binary.
# Usage: ./build.sh [output-path]
set -euo pipefail

OUT="${1:-./load-test}"
SRC="main.go"

echo "→ building $SRC"
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$OUT" .

echo "→ compressing with UPX"
upx --best --lzma "$OUT" >/dev/null

echo "→ done: $OUT ($(du -h "$OUT" | cut -f1))"
