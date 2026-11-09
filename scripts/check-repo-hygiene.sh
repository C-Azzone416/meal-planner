#!/usr/bin/env bash
# Check what is being committed or pushed, not what happens to be in the worktree.
#
#   check-repo-hygiene.sh [TIP [BASE | --outgoing | --history]]
#
# TIP         (default HEAD) a commit whose whole tree is checked: forbidden paths (gitlinks included) and
#             blobs over 1 MiB.
# BASE        also check every file added or changed by each commit in BASE..TIP, so a bad file added in one
#             commit and removed in the next is still caught.
# --outgoing  the same for every commit reachable from TIP that no remote-tracking ref has (what a push of TIP
#             sends when there is no known base).
# --history   the same for every commit reachable from TIP (CI when there is no usable base).
# Without a second argument only TIP's tree is checked.
#
# Any failure to resolve a commit or enumerate objects fails the check; nothing is skipped silently.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/meal-planner-hygiene.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

tip_arg="${1:-HEAD}"
base_arg="${2:-}"
rev_args=()
range_label=""
max_bytes=1048576
failed=0

if ! tip="$(git rev-parse --verify --quiet "${tip_arg}^{commit}")"; then
  echo "repository hygiene: cannot resolve commit '$tip_arg'" >&2
  exit 1
fi
case "$base_arg" in
  "") ;;
  --outgoing) rev_args=("$tip" --not --remotes); range_label="outgoing commits of $tip_arg" ;;
  --history) rev_args=("$tip"); range_label="history of $tip_arg" ;;
  -*)
    echo "repository hygiene: unknown option '$base_arg'" >&2
    exit 64
    ;;
  *)
    if ! base="$(git rev-parse --verify --quiet "${base_arg}^{commit}")"; then
      echo "repository hygiene: cannot resolve base commit '$base_arg'" >&2
      exit 1
    fi
    rev_args=("$base..$tip")
    range_label="$base_arg..$tip_arg"
    ;;
esac

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
  # Blobs have a size; gitlinks (submodule entries, type "commit") only get the path policy.
  case "${2:-}" in
    blob) check_path "$path" "${4:-}" "$tip_arg" ;;
    commit) check_path "$path" "" "$tip_arg" ;;
  esac
done < "$tmp/tree"

# 2. Every file added or changed by any commit between BASE and TIP (merge commits are diffed against
#    each parent), not just those that survive in TIP. Diffing per commit rather than listing new blobs
#    means a forbidden path is still reported when its content matches a blob that already exists.
if (( ${#rev_args[@]} > 0 )); then
  if ! git rev-list "${rev_args[@]}" > "$tmp/commits"; then
    echo "repository hygiene: could not enumerate commits: $range_label" >&2
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
    if [[ "${2:-}" == 160000 ]]; then # submodule pointer: path policy only, there is no blob to size
      check_path "$path" "" "$range_label"
      continue
    fi
    printf '%s\n' "${4:-}" >> "$tmp/new-shas"
    printf '%s\0' "$path" >> "$tmp/new-paths"
  done < "$tmp/changes"
  if ! git cat-file --batch-check='%(objectsize)' < "$tmp/new-shas" > "$tmp/new-sizes"; then
    echo "repository hygiene: could not read object sizes: $range_label" >&2
    exit 1
  fi
  while IFS= read -r size && IFS= read -r -d '' path <&3; do
    check_path "$path" "$size" "$range_label"
  done < "$tmp/new-sizes" 3< "$tmp/new-paths"
fi

if (( failed != 0 )); then
  exit 1
fi
echo "repository hygiene: OK"
