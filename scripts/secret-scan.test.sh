#!/usr/bin/env bash
# secret-scan.sh must find secrets in the scanned range, including ones that only appear in a merge
# commit, must redact its output, and must not rescan history outside the range.
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/secret-scan.sh"
if ! command -v gitleaks > /dev/null 2>&1; then
  echo "secret-scan: SKIP (gitleaks is not installed)"
  exit 0
fi
tmp="$(mktemp -d "${TMPDIR:-/tmp}/meal-planner-secret-scan-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
failures=0

# Built at run time so this file never contains a complete token and does not trip the repository scan.
prefix=gh
token="${prefix}p_aB3dE5gH7jK9mN1pQ3sT5vW7yZ9bD1fH3jK5"
other="${prefix}p_Zy8xW6vU4tS2rQ0pO8nM6lK4jI2hG0fE8dC6"

repo="$tmp/repo"
git init -q -b main "$repo"
git -C "$repo" config user.email test@example.test
git -C "$repo" config user.name test
git -C "$repo" config commit.gpgsign false
head_sha() { git -C "$repo" rev-parse HEAD; }

echo base > "$repo/f"
git -C "$repo" add -A && git -C "$repo" commit -q -m base
base="$(head_sha)"

scan() { (cd "$repo" && bash "$script" "$@") > "$tmp/out" 2>&1; }
expect() {
  local name="$1" want="$2"; shift 2
  local rc=0
  scan "$@" || rc=$?
  if [[ $rc -ne $want ]]; then
    echo "FAIL $name: want rc=$want got rc=$rc"; cat "$tmp/out"; failures=$((failures + 1)); return
  fi
  if grep -Fq -e "$token" -e "$other" "$tmp/out"; then
    echo "FAIL $name: output is not redacted"; failures=$((failures + 1)); return
  fi
  echo "ok   $name"
}

rc=0; (cd "$repo" && bash "$script") > /dev/null 2>&1 || rc=$?
if [[ $rc -eq 64 ]]; then echo "ok   usage error without arguments"; else echo "FAIL usage error (rc=$rc)"; failures=$((failures + 1)); fi

echo clean > "$repo/g"
git -C "$repo" add -A && git -C "$repo" commit -q -m clean
clean="$(head_sha)"
expect "clean range passes" 0 "$base..$clean"

echo "token=$token" > "$repo/leak.txt"
git -C "$repo" add -A && git -C "$repo" commit -q -m leak
leak="$(head_sha)"
expect "secret in range fails and is redacted" 1 "$base..$leak"
expect "secret outside the range is not rescanned" 0 "$leak..$leak"
expect "SHA --not BASE form" 1 "$leak" --not "$base"

# A secret that exists only in a merge commit (conflict resolution), not in either parent.
git -C "$repo" reset -q --hard "$clean"
git -C "$repo" checkout -q -b side "$base"
echo side > "$repo/s"
git -C "$repo" add -A && git -C "$repo" commit -q -m side
git -C "$repo" checkout -q main
git -C "$repo" merge --no-ff --no-commit -q side > /dev/null 2>&1 || true
echo "token=$other" > "$repo/merge-only.txt"
git -C "$repo" add -A && git -C "$repo" commit -q -m "merge side"
merge="$(head_sha)"
expect "secret introduced only by a merge fails" 1 "$base..$merge"

if (( failures > 0 )); then
  echo "secret-scan: $failures failure(s)" >&2
  exit 1
fi
echo "secret-scan: OK"
