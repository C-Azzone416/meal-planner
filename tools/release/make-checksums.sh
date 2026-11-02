#!/usr/bin/env bash
# Write SHA256SUMS for every release archive in <dir>.
# Usage: tools/release/make-checksums.sh <dir>
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <dir>" >&2
  exit 2
fi
cd "$1"
shopt -s nullglob
files=(mealplanner_*.tar.gz mealplanner_*.zip)
if [ "${#files[@]}" -eq 0 ]; then
  echo "error: no release archives in $1" >&2
  exit 1
fi
sha256sum "${files[@]}" > SHA256SUMS
cat SHA256SUMS
