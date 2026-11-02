#!/usr/bin/env bash
# Extract a release archive and assert both binaries report the version.
# Usage: tools/release/smoke-test.sh <archive> <version>
# Only meaningful when the archive matches the machine running it.
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <archive> <version>" >&2
  exit 2
fi
archive="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
version="$2"
dir="$(mktemp -d)"
trap 'rm -rf "$dir"' EXIT

case "$archive" in
  *.tar.gz) tar -xzf "$archive" -C "$dir"; ext="" ;;
  *.zip)
    ext=".exe"
    # bsdtar reads zip. On Windows bash, plain `tar` is GNU tar (no zip), so use
    # System32's bsdtar with native paths; elsewhere bsdtar is `tar`.
    if command -v cygpath >/dev/null 2>&1 && [ -n "${SYSTEMROOT:-}" ]; then
      "$(cygpath -u "$SYSTEMROOT")/System32/tar.exe" -xf "$(cygpath -w "$archive")" -C "$(cygpath -w "$dir")"
    else
      tar -xf "$archive" -C "$dir"
    fi ;;
  *) echo "error: unknown archive type: $archive" >&2; exit 2 ;;
esac

root="$(find "$dir" -mindepth 1 -maxdepth 1 -type d | head -n1)"
for bin in mealplanner mealplanner-mcp; do
  got="$("$root/${bin}${ext}" --version | tr -d '\r')"
  want="${bin} ${version}"
  if [ "$got" != "$want" ]; then
    echo "FAIL: ${bin} --version printed '$got', want '$want'" >&2
    exit 1
  fi
  echo "ok: $got"
done
