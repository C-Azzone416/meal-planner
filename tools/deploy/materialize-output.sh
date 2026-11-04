#!/usr/bin/env bash
# Make a `vercel build` output self-contained, or check that it is.
# Usage: materialize-output.sh [project-root]          materialize (build job, after `vercel build`, before packaging)
#        materialize-output.sh --check <extract-dir>   assert only (deploy job, after safe-extract, before `vercel deploy`)
#
# Why: for files that live outside a function's own directory (the compiled Go binary is left in the build
# runner's /tmp), `vercel build` writes `filePathMap` into the function's .vc-config.json instead of copying
# the file: { "<path inside the function>": "<source path relative to the project root>" }. The map is resolved at
# deploy time against the deploy-side project root, so a build output that is built on one runner and deployed
# from another points at files that do not exist there (`ENOENT ... readlink '/tmp/<id>/bootstrap'`). See
# docs/runbooks/hosted-deploy.md for the vercel 65.9.0 source references.
#
# Materialize mode, for every `.vc-config.json` under <root>/.vercel/output that has a non-empty `filePathMap`:
# resolves each source as the CLI does (lexical join of <root> and the map value; the value may contain `..`
# and leave the root, that is the real case), requires it to be a regular file (not a symlink, not a directory),
# copies it to <function dir>/<key> keeping its mode (the executable bit matters), and deletes `filePathMap` from
# the config (the key is deleted, not emptied: the consumer hydrates only `if (functionConfig.filePathMap ...)`,
# and `{}` is truthy, so an empty map would be spread into the Lambda config). Keys must be relative, without
# `.`, `..` or empty components, so they stay inside the function dir. Sources reached through a symlinked
# directory are rejected too. Afterwards no `.vc-config.json` may have a non-empty `filePathMap` and nothing
# under the output may be a symlink; every copy must be a regular file physically inside its function directory
# (so vercel's prebuilt-path check, validatePrebuiltFilePath, would accept it, though with the map gone the CLI
# no longer needs that check). Every .vc-config.json must hold exactly one JSON document.
# This is a correctness tool, not a security boundary: the build job's code already owns the build runner. The
# boundary is on the deploy side (checksum, safe-extract, --check, the token reaching only the deploy job).
# Known limit: Vercel can alias several routes to one function with a `.func` symlink; that fails the symlink
# checks here and in safe-extract. Not used today.
# Check mode runs only the `filePathMap` assertion. Fails closed with ::error:: messages; needs bash and jq.
set -euo pipefail

fail() { echo "::error::materialize-output: $*" >&2; exit 1; }
usage() { echo "usage: materialize-output.sh [project-root] | --check <extract-dir>" >&2; exit 2; }

command -v jq > /dev/null || fail "jq is required"

# Scratch space outside the output, so a failed run leaves nothing behind in it. Every list and every jq result
# goes through a file or an explicit status check: a process substitution or a pipeline would hide a failing
# find, sort or jq from `set -e` and could turn a read error into "nothing to do".
tmpd=$(mktemp -d) || fail "cannot create a temporary directory"
trap 'rm -rf "$tmpd"' EXIT

# Lexically normalise an absolute path (like node's path.join): drop `.` and empty parts, apply `..`.
lexnorm() {
  local IFS=/ part
  local -a parts stack=()
  read -ra parts <<< "$1"
  for part in "${parts[@]}"; do
    case $part in
      '' | .) ;;
      ..) if [[ ${#stack[@]} -gt 0 ]]; then unset "stack[${#stack[@]}-1]"; fi ;;
      *) stack+=("$part") ;;
    esac
  done
  if [[ ${#stack[@]} -eq 0 ]]; then echo /; else printf '/%s' "${stack[@]}"; echo; fi
}

# list_configs <dir> <outfile>: every .vc-config.json under <dir>, NUL-separated and sorted, into <outfile>.
list_configs() {
  find "$1" -name .vc-config.json -print0 > "$tmpd/find.out" || fail "cannot list .vc-config.json files under $1"
  sort -z < "$tmpd/find.out" > "$2" || fail "cannot sort the .vc-config.json list"
}

# assert_no_filepathmap <output-dir>: each .vc-config.json is exactly one JSON object with no filePathMap entries.
assert_no_filepathmap() {
  local cfg
  list_configs "$1" "$tmpd/assert.list"
  while IFS= read -r -d '' cfg; do
    [[ -f "$cfg" && ! -L "$cfg" ]] || fail "$cfg is not a regular file"
    jq -es 'length == 1 and (.[0] | type == "object" and ((.filePathMap // {}) | type == "object" and length == 0))' "$cfg" > /dev/null 2>&1 \
      || fail "build output is not self-contained: $cfg is not exactly one valid JSON object or has a non-empty filePathMap (it would reference files outside the output on the build runner)"
  done < "$tmpd/assert.list"
}

if [[ ${1:-} == --check ]]; then
  [[ $# -eq 2 ]] || usage
  out="$2/.vercel/output"
  [[ -d "$out" && ! -L "$out" ]] || fail "$out is not a directory"
  assert_no_filepathmap "$out"
  echo "materialize-output: $out is self-contained (no filePathMap)"
  exit 0
fi

[[ $# -le 1 && ${1:-} != -* ]] || usage
root=${1:-.}
[[ -d "$root" ]] || fail "project root $root is not a directory"
root=$(cd "$root" && pwd -P) || fail "cannot resolve project root $root"
out="$root/.vercel/output"
[[ -d "$out" && ! -L "$out" ]] || fail "$out is not a directory (did vercel build run?)"

no_symlinks() { # no_symlinks <when>
  local link
  link=$(find "$out" -type l -print -quit) || fail "cannot search .vercel/output for symlinks"
  [[ -z "$link" ]] || fail "symlink under .vercel/output $1: $link (symlinks are rejected by safe-extract and would not survive packaging)"
}
no_symlinks "before materializing"

copied=0 configs=0
list_configs "$out" "$tmpd/main.list"
while IFS= read -r -d '' cfg; do
  [[ -f "$cfg" && ! -L "$cfg" ]] || fail "$cfg is not a regular file"
  jq -es 'length == 1 and (.[0] | type == "object")' "$cfg" > /dev/null 2>&1 || fail "$cfg is not exactly one JSON object (malformed JSON, or more than one document?)"
  # Anything but absent, null or {} must be an object of non-empty strings without control characters.
  jq -es '(.[0].filePathMap // {}) | type == "object" and all(to_entries[]; (.key | type == "string" and length > 0 and (explode | any(. < 32) | not)) and (.value | type == "string" and length > 0 and (explode | any(. < 32) | not)))' "$cfg" > /dev/null 2>&1 \
    || fail "$cfg: filePathMap is not an object of non-empty strings without control characters"
  expected=$(jq -s '(.[0].filePathMap // {}) | length' "$cfg") || fail "cannot count the filePathMap entries of $cfg"
  [[ $expected =~ ^[0-9]+$ ]] || fail "$cfg: unexpected filePathMap entry count: $expected"
  [[ $expected -gt 0 ]] || continue

  funcdir=$(dirname "$cfg")
  [[ $funcdir == *.func ]] || fail "$cfg has a filePathMap but is not in a *.func directory"
  configs=$((configs + 1))
  funcdir_real=$(cd "$funcdir" && pwd -P) || fail "cannot resolve $funcdir"

  jq -j '.filePathMap | to_entries[] | .key, "\u0000", .value, "\u0000"' "$cfg" > "$tmpd/entries" || fail "cannot read the filePathMap entries of $cfg"
  made=0
  while IFS= read -r -d '' key && IFS= read -r -d '' value; do
    # The key is where the file lives inside the function: relative, no `.`/`..`/empty components.
    [[ $key != /* ]] || fail "$cfg: absolute filePathMap key: $key"
    [[ $key != */ && ! $key =~ (^|/)(\.\.?)?(/|$) ]] || fail "$cfg: filePathMap key has an empty, '.' or '..' component or a trailing slash: $key"
    [[ $key != .vc-config.json ]] || fail "$cfg: filePathMap key would overwrite .vc-config.json"
    dest="$funcdir/$key"
    case $(lexnorm "$dest") in "$(lexnorm "$funcdir")"/*) ;; *) fail "$cfg: filePathMap key escapes the function directory: $key" ;; esac
    [[ ! -e "$dest" && ! -L "$dest" ]] || fail "$cfg: $dest already exists in the function directory (duplicate key, or a key that is a prefix of another)"

    # The value is relative to the project root, as vercel computes it; join like node's path.join (a leading `/`
    # is not special), then require a regular file reached without any symlink: not the file itself, and not a
    # directory on the way (root is already physical, so only links below it, or outside it via `..`, count). The
    # build runner's /tmp is a real directory; a symlinked one (macOS /var) is refused rather than followed.
    src=$(lexnorm "$root/$value")
    [[ ! -L "$src" ]] || fail "$cfg: filePathMap source for $key is a symlink: $src"
    [[ -e "$src" ]] || fail "$cfg: filePathMap source for $key does not exist: $src (from $value)"
    [[ -f "$src" ]] || fail "$cfg: filePathMap source for $key is not a regular file: $src"
    srcdir=$(dirname "$src")
    srcdir_real=$(cd "$srcdir" && pwd -P) || fail "$cfg: cannot resolve the directory of the source for $key: $srcdir"
    [[ $srcdir_real == "$srcdir" ]] || fail "$cfg: filePathMap source for $key is reached through a symlinked directory: $srcdir resolves to $srcdir_real"

    mkdir -p -- "$(dirname "$dest")" 2> "$tmpd/err" || fail "$cfg: cannot create the directory for $key (is another key a file at that path?): $(cat "$tmpd/err")"
    cp -p -- "$src" "$dest" || fail "$cfg: cannot copy $src to $dest"
    # vercel rejects a prebuilt file that resolves outside the uploaded project (INVALID_PREBUILT_FILE_PATH);
    # make sure the copy really is a regular file physically inside its own function directory.
    [[ -f "$dest" && ! -L "$dest" ]] || fail "$cfg: copy of $key is not a regular file: $dest"
    case $(cd "$(dirname "$dest")" && pwd -P) in
      "$funcdir_real" | "$funcdir_real"/*) ;;
      *) fail "$cfg: copy of $key is not inside its function directory: $dest" ;;
    esac
    made=$((made + 1))
  done < "$tmpd/entries"
  # Never drop the map unless every entry it listed was copied.
  [[ $made -eq $expected ]] || fail "$cfg: copied $made of $expected filePathMap entries"
  copied=$((copied + made))

  # Rewrite in place (not rename over it) so the config keeps its mode and owner.
  jq --indent 2 'del(.filePathMap)' "$cfg" > "$tmpd/rewritten.json" || fail "cannot rewrite $cfg"
  jq -e 'type == "object" and (has("filePathMap") | not)' "$tmpd/rewritten.json" > /dev/null 2>&1 || fail "rewritten $cfg still has a filePathMap"
  cat -- "$tmpd/rewritten.json" > "$cfg" || fail "cannot write $cfg"
done < "$tmpd/main.list"

assert_no_filepathmap "$out"
no_symlinks "after materializing"
echo "materialize-output: materialized $copied file(s) into $configs function(s); output is self-contained"
