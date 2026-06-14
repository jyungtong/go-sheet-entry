#!/bin/bash
set -e

GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
  go build -trimpath -ldflags="-s -w" -o go-sheet-entry-linux-arm64 .

docker buildx build --platform linux/arm64 \
  -t ghcr.io/jyungtong/go-sheet-entry:latest \
  --push .
