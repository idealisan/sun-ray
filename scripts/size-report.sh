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

mkdir -p dist
echo "binary        size"
build app        .
build probe-ui   ./cmd/probe-ui
build probe-core ./cmd/probe-core