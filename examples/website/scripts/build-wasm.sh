#!/usr/bin/env bash
set -euo pipefail

if [ "${1-}" = "" ]; then
  echo "usage: $0 /path/to/website-app"
  exit 1
fi

target_dir="$(cd "$1" && pwd)"
public_dir="$target_dir/public"
examples_dir="$(cd "$(dirname "$0")/../.." && pwd)"

mkdir -p "$public_dir"

cd "$examples_dir"
GOOS=js GOARCH=wasm go build -o "$public_dir/gbash.wasm" ./website/wasm
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" "$public_dir/wasm_exec.js"

echo "wrote $public_dir/gbash.wasm"
echo "wrote $public_dir/wasm_exec.js"

