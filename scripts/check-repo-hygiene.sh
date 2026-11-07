#!/usr/bin/env bash
# Check what is being committed or pushed, not what happens to be in the worktree.
#
#   check-repo-hygiene.sh [TIP [BASE]]
#
# TIP  (default HEAD) a commit whose whole tree is checked: forbidden paths and blobs over 1 MiB.
# BASE (optional)     also check every blob introduced by BASE..TIP, so a bad file added in one commit
#                     and removed in the next is still caught. Without BASE only TIP's tree is checked.
#
# Any failure to resolve a commit or enumerate objects fails the check; nothing is skipped silently.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/meal-planner-hygiene.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

tip_arg="${1:-HEAD}"
base_arg="${2:-}"
max_bytes=1048576
failed=0

if ! tip="$(git rev-parse --verify --quiet "${tip_arg}^{commit}")"; then
  echo "repository hygiene: cannot resolve commit '$tip_arg'" >&2
  exit 1
fi
if [[ -n "$base_arg" ]] && ! base="$(git rev-parse --verify --quiet "${base_arg}^{commit}")"; then
  echo "repository hygiene: cannot resolve base commit '$base_arg'" >&2
  exit 1
fi

# check_path PATH SIZE WHERE
check_path() {
  local path="$1" size="$2" where="$3"
  case "$path" in
    .env.example|*/.env.example) ;;
    .env|.env.*|*/.env|*/.env.*|*.db|*.db-*|*.sqlite|*.sqlite3 \
      |node_modules|node_modules/*|*/node_modules|*/node_modules/* \
      |dist|dist/*|*/dist|*/dist/* \
      |coverage|coverage/*|*/coverage|*/coverage/*)
      echo "repository hygiene: forbidden file in $where: $path" >&2
      failed=1
      ;;
  esac
  if [[ "$size" =~ ^[0-9]+$ ]] && (( size > max_bytes )); then
    echo "repository hygiene: file exceeds 1 MiB in $where: $path ($size bytes)" >&2
    failed=1
  fi
}

# 1. Every path and size in TIP's tree. Records: "<mode> <type> <object> <size>\t<path>\0".
if ! git ls-tree -r -z -l "$tip" > "$tmp/tree"; then
  echo "repository hygiene: could not list the tree of $tip" >&2
  exit 1
fi
while IFS= read -r -d '' record; do
  meta="${record%%$'\t'*}"
  path="${record#*$'\t'}"
  # shellcheck disable=SC2086 # meta is four space-separated fields
  set -- $meta
  [[ "${2:-}" == blob ]] || continue
  check_path "$path" "${4:-}" "$tip_arg"
done < "$tmp/tree"

# 2. Every file added or changed by any commit between BASE and TIP (merge commits are diffed against
#    each parent), not just those that survive in TIP. Diffing per commit rather than listing new blobs
#    means a forbidden path is still reported when its content matches a blob that already exists.
if [[ -n "$base_arg" ]]; then
  if ! git rev-list "$base..$tip" > "$tmp/commits"; then
    echo "repository hygiene: could not enumerate commits in $base_arg..$tip_arg" >&2
    exit 1
  fi
  : > "$tmp/changes"
  while IFS= read -r commit; do
    if ! git diff-tree -r -z -m --root --no-renames --no-commit-id --diff-filter=AMT --abbrev=40 "$commit" >> "$tmp/changes"; then
      echo "repository hygiene: could not diff commit $commit" >&2
      exit 1
    fi
  done < "$tmp/commits"
  # Records alternate: ":<oldmode> <newmode> <oldsha> <newsha> <status>" NUL "<path>" NUL.
  : > "$tmp/new-shas"
  : > "$tmp/new-paths"
  while IFS= read -r -d '' meta && IFS= read -r -d '' path; do
    # shellcheck disable=SC2086 # meta is five space-separated fields
    set -- $meta
    [[ "${2:-}" == 160000 ]] && continue # submodule pointer, not a blob
    printf '%s\n' "${4:-}" >> "$tmp/new-shas"
    printf '%s\0' "$path" >> "$tmp/new-paths"
  done < "$tmp/changes"
  if ! git cat-file --batch-check='%(objectsize)' < "$tmp/new-shas" > "$tmp/new-sizes"; then
    echo "repository hygiene: could not read object sizes in $base_arg..$tip_arg" >&2
    exit 1
  fi
  while IFS= read -r size && IFS= read -r -d '' path <&3; do
    check_path "$path" "$size" "$base_arg..$tip_arg"
  done < "$tmp/new-sizes" 3< "$tmp/new-paths"
fi

if (( failed != 0 )); then
  exit 1
fi
echo "repository hygiene: OK"
