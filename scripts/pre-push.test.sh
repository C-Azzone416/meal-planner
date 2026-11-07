#!/usr/bin/env bash
# pre-push.sh must check what is pushed (paths, sizes, secrets in the pushed range), not the worktree.
set -euo pipefail

here="$(cd "$(dirname "$0")/.." && pwd)"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/meal-planner-pre-push-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
zero=0000000000000000000000000000000000000000
failures=0

git init -q --bare "$tmp/remote.git"
repo="$tmp/repo"
git init -q -b main "$repo"
git -C "$repo" config user.email test@example.test
git -C "$repo" config user.name test
git -C "$repo" config commit.gpgsign false
git -C "$repo" remote add origin "$tmp/remote.git"
mkdir -p "$repo/scripts" "$repo/.github/scripts"
for f in pre-push.sh check-changed.sh check-repo-hygiene.sh secret-scan.sh with-heavy-lock.sh; do cp "$here/scripts/$f" "$repo/scripts/"; done
cp "$here/.github/scripts/classify.sh" "$repo/.github/scripts/"
echo base > "$repo/README.md"
git -C "$repo" add -A && git -C "$repo" commit -q -m base
git -C "$repo" push -q origin main:develop
git -C "$repo" fetch -q origin
base="$(git -C "$repo" rev-parse HEAD)"

commit() { git -C "$repo" add -A -f && git -C "$repo" commit -q -m "$1" && git -C "$repo" rev-parse HEAD; }

# run_hook NAME WANT PATTERN LOCAL_SHA REMOTE_SHA
run_hook() {
  local name="$1" want="$2" pattern="$3" local_sha="$4" remote_sha="$5" rc=0
  (cd "$repo" && echo "refs/heads/feat $local_sha refs/heads/feat $remote_sha" | bash scripts/pre-push.sh) > "$tmp/out" 2>&1 || rc=$?
  if [[ $rc -ne $want ]]; then echo "FAIL $name: want rc=$want got rc=$rc"; cat "$tmp/out"; failures=$((failures + 1)); return; fi
  if [[ -n "$pattern" ]] && ! grep -Fq -- "$pattern" "$tmp/out"; then
    echo "FAIL $name: output lacks '$pattern'"; cat "$tmp/out"; failures=$((failures + 1)); return
  fi
  echo "ok   $name"
}

git -C "$repo" checkout -q -b feat
mkdir -p "$repo/docs" && echo notes > "$repo/docs/notes.md"
docs="$(commit "docs only")"
run_hook "new branch, docs only"                       0 "docs-only change" "$docs" "$zero"

# The worktree is irrelevant: a forbidden file that is only on disk (untracked) does not fail the push...
mkdir -p "$repo/node_modules/p" && echo x > "$repo/node_modules/p/i.js"
run_hook "untracked worktree file is not pushed"       0 "" "$docs" "$zero"
rm -rf "$repo/node_modules"

# ...but a root node_modules that is in the pushed commits does, for a new branch and for an update.
echo x > "$repo/.keep" && mkdir -p "$repo/node_modules/p" && echo y > "$repo/node_modules/p/i.js"
bad="$(commit "add node_modules")"
run_hook "new branch with root node_modules"           1 "forbidden file" "$bad" "$zero"
run_hook "update with root node_modules"               1 "forbidden file" "$bad" "$docs"

# Removed again in the tip: the tip tree is clean but the pushed range still contained it.
git -C "$repo" rm -q -r -f node_modules
removed="$(commit "remove node_modules")"
run_hook "bad file removed within the pushed range"    1 "node_modules/p/i.js" "$removed" "$docs"
run_hook "clean tip with nothing new in the range"     0 "" "$removed" "$removed"

# A deletion push is not checked.
run_hook "branch deletion"                             0 "" "$zero" "$docs"

# Secret scanning covers only the pushed range, and includes merges.
if command -v gitleaks > /dev/null 2>&1; then
  prefix=gh
  token="${prefix}p_aB3dE5gH7jK9mN1pQ3sT5vW7yZ9bD1fH3jK5"
  git -C "$repo" reset -q --hard "$base"
  echo "token=$token" > "$repo/leak.txt"
  leak="$(commit "leak")"
  run_hook "secret in the pushed range"                1 "leaks found" "$leak" "$zero"
  echo more > "$repo/more.md"
  after="$(commit "after the leak")"
  run_hook "leak already on the remote is not rescanned" 0 "" "$after" "$leak"
else
  echo "skip secret cases (gitleaks is not installed)"
fi

if (( failures > 0 )); then
  echo "pre-push: $failures failure(s)" >&2
  exit 1
fi
echo "pre-push: OK"
