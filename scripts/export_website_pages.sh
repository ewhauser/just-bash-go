#!/usr/bin/env bash

set -euo pipefail

repo_dir="${1:-}"
output_dir="${2:-}"

if [ -z "$repo_dir" ] || [ -z "$output_dir" ]; then
  echo "usage: $0 <repo-dir> <output-dir>" >&2
  exit 1
fi

(
  cd "$repo_dir"
  GBASH_WEBSITE_EXPORT="${GBASH_WEBSITE_EXPORT:-1}" \
  npm exec --yes pnpm@10.10.0 -- --dir website build
)

rm -rf "$output_dir"
mkdir -p "$output_dir"
cp -a "$repo_dir/website/out/." "$output_dir"/
touch "$output_dir/.nojekyll"
