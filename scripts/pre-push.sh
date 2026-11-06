#!/usr/bin/env bash
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/meal-planner-pre-push.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
: > "$tmp/changed.txt"
classify_all=false
saw_ref=false
zero=0000000000000000000000000000000000000000

while read -r local_ref local_sha remote_ref remote_sha; do
  saw_ref=true
  [[ "$local_sha" == "$zero" ]] && continue
  if [[ "$remote_sha" == "$zero" ]]; then
    base="$(git merge-base "$local_sha" origin/develop 2>/dev/null || true)"
  else
    base="$remote_sha"
  fi
  if [[ -z "$base" ]] || ! git cat-file -e "${base}^{commit}" 2>/dev/null; then
    classify_all=true
  elif ! git diff --no-renames --name-only "$base" "$local_sha" >> "$tmp/changed.txt"; then
    classify_all=true
  fi
done

if [[ "$saw_ref" == false ]]; then
  base="$(git merge-base HEAD origin/develop 2>/dev/null || true)"
  if [[ -z "$base" ]]; then
    classify_all=true
  else
    git diff --no-renames --name-only "$base" HEAD >> "$tmp/changed.txt"
  fi
fi
LC_ALL=C sort -u "$tmp/changed.txt" -o "$tmp/changed.txt"

bash scripts/check-repo-hygiene.sh
if command -v gitleaks >/dev/null 2>&1; then
  gitleaks detect --no-banner --redact
else
  echo "secret scan: gitleaks is not installed here (CI still runs it)"
fi

CLASSIFY_ALL="$classify_all" bash scripts/check-changed.sh < "$tmp/changed.txt"
