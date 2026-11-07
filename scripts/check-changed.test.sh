#!/usr/bin/env bash
# check-changed.sh must hand gofmt each Go path intact, including paths with spaces.
set -euo pipefail

here="$(cd "$(dirname "$0")/.." && pwd)"
if ! command -v gofmt > /dev/null 2>&1; then
  echo "check-changed: SKIP (gofmt is not installed)"
  exit 0
fi
tmp="$(mktemp -d "${TMPDIR:-/tmp}/meal-planner-check-changed-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

repo="$tmp/repo"
git init -q -b main "$repo"
git -C "$repo" config user.email test@example.test
git -C "$repo" config user.name test
mkdir -p "$repo/scripts" "$repo/.github/scripts"
cp "$here/scripts/check-changed.sh" "$repo/scripts/"
cp "$here/.github/scripts/classify.sh" "$repo/.github/scripts/"
printf 'module example.test/x\n\ngo 1.21\n' > "$repo/go.mod"
printf 'package x\nfunc   Bad( ){}\n' > "$repo/has space.go"
printf 'package y\n\nfunc Good() {}\n' > "$repo/fine.go"
git -C "$repo" add -A && git -C "$repo" commit -q -m init

rc=0
(cd "$repo" && echo 'has space.go' | CLASSIFY_ALL=true bash scripts/check-changed.sh) > "$tmp/out" 2> "$tmp/err" || rc=$?
[[ $rc -ne 0 ]] || { echo "FAIL: unformatted Go file was accepted"; exit 1; }
grep -qx 'gofmt required:' "$tmp/err" || { echo "FAIL: expected the gofmt message"; cat "$tmp/err"; exit 1; }
grep -qx 'has space.go' "$tmp/err" || { echo "FAIL: the path with a space was split or not reported"; cat "$tmp/err"; exit 1; }
! grep -qi 'no such file' "$tmp/err" || { echo "FAIL: gofmt received split path fragments"; cat "$tmp/err"; exit 1; }
! grep -qx 'fine.go' "$tmp/err" || { echo "FAIL: a formatted file was reported"; cat "$tmp/err"; exit 1; }

echo "check-changed: OK"
