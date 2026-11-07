#!/usr/bin/env bash
# Tests for check-repo-hygiene.sh against throwaway repositories.
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/check-repo-hygiene.sh"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/meal-planner-hygiene-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
failures=0

repo="$tmp/repo"
git init -q -b main "$repo"
git -C "$repo" config user.email test@example.test
git -C "$repo" config user.name test
git -C "$repo" config commit.gpgsign false
commit() { git -C "$repo" add -A -f && git -C "$repo" commit -q -m "$1" && git -C "$repo" rev-parse HEAD; }

echo ok > "$repo/README.md"
base="$(commit base)"

# expect NAME WANT(0|1) [PATTERN-in-stderr] -- args...
expect() {
  local name="$1" want="$2" pattern="$3"; shift 3
  local rc=0
  (cd "$repo" && bash "$script" "$@") > "$tmp/out" 2> "$tmp/err" || rc=$?
  if [[ $rc -ne $want ]]; then
    echo "FAIL $name: want rc=$want got rc=$rc"; cat "$tmp/err"; failures=$((failures + 1)); return
  fi
  if [[ -n "$pattern" ]] && ! grep -Fq -- "$pattern" "$tmp/err"; then
    echo "FAIL $name: stderr lacks '$pattern'"; cat "$tmp/err"; failures=$((failures + 1)); return
  fi
  echo "ok   $name"
}

expect "clean tree"                      0 "" "$base"
expect "defaults to HEAD"                0 ""
expect "unknown tip fails"               1 "cannot resolve commit" does-not-exist
expect "unknown base fails"              1 "cannot resolve base commit" "$base" does-not-exist

echo SECRET=1 > "$repo/.env.example"
mkdir -p "$repo/sub/ok"
echo x > "$repo/sub/ok/file.txt"
allowed="$(commit "allowed files")"
expect ".env.example is allowed"         0 "" "$allowed" "$base"

# Forbidden paths, at the repository root and nested, which a worktree-only `*/dir/*` pattern missed.
for bad in node_modules/pkg/index.js dist/app.js coverage/lcov.info web/node_modules/x.js web/dist/a.js web/coverage/b.json .env .env.local app/.env data.sqlite local.db; do
  git -C "$repo" reset -q --hard "$allowed"
  mkdir -p "$repo/$(dirname "$bad")"
  echo x > "$repo/$bad"
  tip="$(commit "add $bad")"
  expect "forbidden: $bad (tip tree)"    1 "forbidden file" "$tip"
  expect "forbidden: $bad (range)"       1 "forbidden file" "$tip" "$allowed"
done

# Oversized blob.
git -C "$repo" reset -q --hard "$allowed"
dd if=/dev/zero of="$repo/big.bin" bs=1024 count=1100 2> /dev/null
big="$(commit "add big file")"
expect "blob over 1 MiB"                 1 "exceeds 1 MiB" "$big"

# Added in one commit and removed in the next: the tip tree is clean, the pushed range is not.
git -C "$repo" rm -q -f big.bin
mkdir -p "$repo/node_modules/pkg" && echo x > "$repo/node_modules/pkg/i.js"
commit "remove big, add node_modules" > /dev/null
git -C "$repo" rm -q -r -f node_modules
removed="$(commit "remove node_modules")"
expect "tip tree alone is clean"         0 "" "$removed"
expect "range catches intermediate big"  1 "exceeds 1 MiB" "$removed" "$allowed"
expect "range catches intermediate dir"  1 "node_modules/pkg/i.js" "$removed" "$allowed"
expect "range after the bad commits ok"  0 "" "$removed" "$removed"

# Failures to enumerate must fail, not pass silently.
broken="$tmp/broken"
git init -q -b main "$broken"
git -C "$broken" config user.email test@example.test
git -C "$broken" config user.name test
git -C "$broken" config commit.gpgsign false
echo one > "$broken/a.txt"
git -C "$broken" add -A && git -C "$broken" commit -q -m one
first="$(git -C "$broken" rev-parse HEAD)"
mkdir "$broken/dir" && echo two > "$broken/dir/b.txt"
git -C "$broken" add -A && git -C "$broken" commit -q -m two
second="$(git -C "$broken" rev-parse HEAD)"
subtree="$(git -C "$broken" rev-parse "$second:dir")"
chmod u+w "$broken/.git/objects/${subtree:0:2}" "$broken/.git/objects/${subtree:0:2}/${subtree:2}"
rm -f "$broken/.git/objects/${subtree:0:2}/${subtree:2}"
rc=0
(cd "$broken" && bash "$script" "$second") > /dev/null 2> "$tmp/err" || rc=$?
if [[ $rc -eq 0 ]]; then echo "FAIL missing tree object must fail the tip check"; failures=$((failures + 1)); else echo "ok   missing tree object fails the tip check"; fi
rc=0
(cd "$broken" && bash "$script" "$second" "$first") > /dev/null 2> "$tmp/err" || rc=$?
if [[ $rc -eq 0 ]]; then echo "FAIL missing tree object must fail the range check"; failures=$((failures + 1)); else echo "ok   missing tree object fails the range check"; fi

if (( failures > 0 )); then
  echo "check-repo-hygiene: $failures failure(s)" >&2
  exit 1
fi
echo "check-repo-hygiene: OK"
