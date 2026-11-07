#!/usr/bin/env bash
# Git pre-push hook body. Checks what is being pushed: the outgoing commits' paths and blob sizes, a
# secret scan of only the pushed range, and the affected component checks.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/meal-planner-pre-push.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
: > "$tmp/changed.txt"
classify_all=false
saw_ref=false
scans_failed=false
warned_gitleaks=false
zero=0000000000000000000000000000000000000000

# scan TIP BASE: hygiene and secret scan for the commits TIP adds on top of BASE (BASE may be empty).
scan() {
  local tip="$1" base="$2"
  if [[ -n "$base" ]]; then
    bash scripts/check-repo-hygiene.sh "$tip" "$base" || scans_failed=true
  else
    bash scripts/check-repo-hygiene.sh "$tip" || scans_failed=true
  fi
  if command -v gitleaks > /dev/null 2>&1; then
    if [[ -n "$base" ]]; then
      bash scripts/secret-scan.sh "$base..$tip" || scans_failed=true
    else
      bash scripts/secret-scan.sh "$tip" --not --remotes || scans_failed=true
    fi
  elif [[ "$warned_gitleaks" == false ]]; then
    warned_gitleaks=true
    echo "secret scan: gitleaks is not installed here (CI still runs it)"
  fi
}

# stdin: "<local ref> <local sha> <remote ref> <remote sha>" per ref being pushed.
while read -r _local_ref local_sha _remote_ref remote_sha; do
  saw_ref=true
  [[ "$local_sha" == "$zero" ]] && continue
  if [[ "$remote_sha" == "$zero" ]]; then
    base="$(git merge-base "$local_sha" origin/develop 2>/dev/null || true)"
  else
    base="$remote_sha"
  fi
  if [[ -z "$base" ]] || ! git cat-file -e "${base}^{commit}" 2>/dev/null; then
    classify_all=true
    scan "$local_sha" ""
  else
    scan "$local_sha" "$base"
    if ! git diff --no-renames --name-only "$base" "$local_sha" >> "$tmp/changed.txt"; then
      classify_all=true
    fi
  fi
done

if [[ "$saw_ref" == false ]]; then
  base="$(git merge-base HEAD origin/develop 2>/dev/null || true)"
  if [[ -z "$base" ]]; then
    classify_all=true
    scan HEAD ""
  else
    scan HEAD "$base"
    git diff --no-renames --name-only "$base" HEAD >> "$tmp/changed.txt"
  fi
fi

if [[ "$scans_failed" == true ]]; then
  echo "pre-push: repository hygiene or secret scan failed" >&2
  exit 1
fi

LC_ALL=C sort -u "$tmp/changed.txt" -o "$tmp/changed.txt"
CLASSIFY_ALL="$classify_all" bash scripts/check-changed.sh < "$tmp/changed.txt"
