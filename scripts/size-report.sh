#!/usr/bin/env bash
# size-report.sh builds the app and the two probes, so the size each part
# of it contributes is visible: the UI framework, the core, or both.
set -euo pipefail

cd "$(dirname "$0")/.."

FLAGS=(-trimpath -ldflags "-s -w" -gcflags "all=-l" -tags mygo_noinspector)

build() {
  local name=$1 pkg=$2
  go build "${FLAGS[@]}" -o "dist/$name" "$pkg"
  printf "%-12s %10s\n" "$name" "$(du -h "dist/$name" | cut -f1)"
}

build_win() {
  local name=$1 pkg=$2
  GOOS=windows GOARCH=amd64 go build "${FLAGS[@]}" -o "dist/$name.exe" "$pkg"
  printf "%-12s %10s\n" "$name.exe" "$(du -h "dist/$name.exe" | cut -f1)"
}

mkdir -p dist
echo "binary        size"
build app        .
build probe-ui   ./cmd/probe-ui
build probe-core ./cmd/probe-core
echo "windows probes"
build_win probe-ui   ./cmd/probe-ui
build_win probe-core ./cmd/probe-core