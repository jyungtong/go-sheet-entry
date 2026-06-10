#!/bin/bash
set -e

BINARY="go-sheet-entry-linux-arm64"
OUT="bin/$BINARY"

mkdir -p bin

GOOS=linux GOARCH=arm64 go build \
  -ldflags="-s -w" \
  -o "$OUT" \
  .

echo "Built: $OUT"
