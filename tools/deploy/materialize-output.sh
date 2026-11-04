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
# the config (the CLI's own `vercel describe` continuation does the same: `delete config.filePathMap`). Keys must
# be relative, without `.`, `..` or empty components, so they stay inside the function dir. Afterwards no
# `.vc-config.json` may have a non-empty `filePathMap` and nothing under the output may be a symlink; every
# copy must be a regular file physically inside its function directory (so vercel's prebuilt-path check,
# validatePrebuiltFilePath, would accept it, though with the map gone the CLI no longer needs that check).
# Check mode runs only the `filePathMap` assertion. Fails closed with ::error:: messages; needs bash and jq.
set -euo pipefail

fail() { echo "::error::materialize-output: $*" >&2; exit 1; }

command -v jq > /dev/null || fail "jq is required"

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

# Print every .vc-config.json under $1, NUL-separated and sorted.
list_configs() { find "$1" -name .vc-config.json -print0 | sort -z; }

# assert_no_filepathmap <output-dir> <message-suffix>
assert_no_filepathmap() {
  local out=$1 cfg
  while IFS= read -r -d '' cfg; do
    [[ -f "$cfg" && ! -L "$cfg" ]] || fail "$cfg is not a regular file"
    jq -e 'type == "object" and ((.filePathMap // {}) | type == "object" and length == 0)' "$cfg" > /dev/null 2>&1 \
      || fail "build output is not self-contained: $cfg is not valid JSON or has a non-empty filePathMap (it would reference files outside the output on the build runner)"
  done < <(list_configs "$out")
}

if [[ ${1:-} == --check ]]; then
  dir=${2:?usage: materialize-output.sh --check <extract-dir>}
  out="$dir/.vercel/output"
  [[ -d "$out" && ! -L "$out" ]] || fail "$out is not a directory"
  assert_no_filepathmap "$out"
  echo "materialize-output: $out is self-contained (no filePathMap)"
  exit 0
fi

root=${1:-.}
[[ -d "$root" ]] || fail "project root $root is not a directory"
root=$(cd "$root" && pwd -P)
out="$root/.vercel/output"
[[ -d "$out" && ! -L "$out" ]] || fail "$out is not a directory (did vercel build run?)"

no_symlinks() { # no_symlinks <when>
  local link
  link=$(find "$out" -type l -print -quit)
  [[ -z "$link" ]] || fail "symlink under .vercel/output $1: $link (symlinks are rejected by safe-extract and would not survive packaging)"
}
no_symlinks "before materializing"

copied=0 configs=0
while IFS= read -r -d '' cfg; do
  [[ -f "$cfg" && ! -L "$cfg" ]] || fail "$cfg is not a regular file"
  jq -e 'type == "object"' "$cfg" > /dev/null 2>&1 || fail "$cfg is not a JSON object (malformed JSON?)"
  # Anything but absent, null or {} must be an object of non-empty strings without control characters.
  jq -e '(.filePathMap // {}) | type == "object" and all(to_entries[]; (.key | type == "string" and length > 0 and (explode | any(. < 32) | not)) and (.value | type == "string" and length > 0 and (explode | any(. < 32) | not)))' "$cfg" > /dev/null 2>&1 \
    || fail "$cfg: filePathMap is not an object of non-empty strings without control characters"
  [[ $(jq '(.filePathMap // {}) | length' "$cfg") -gt 0 ]] || continue

  funcdir=$(dirname "$cfg")
  [[ $funcdir == *.func ]] || fail "$cfg has a filePathMap but is not in a *.func directory"
  configs=$((configs + 1))
  funcdir_real=$(cd "$funcdir" && pwd -P)

  while IFS= read -r -d '' key && IFS= read -r -d '' value; do
    # The key is where the file lives inside the function: relative, no `.`/`..`/empty components.
    [[ $key != /* ]] || fail "$cfg: absolute filePathMap key: $key"
    [[ $key != */ && ! $key =~ (^|/)(\.\.?)?(/|$) ]] || fail "$cfg: filePathMap key has an empty, '.' or '..' component or a trailing slash: $key"
    [[ $key != .vc-config.json ]] || fail "$cfg: filePathMap key would overwrite .vc-config.json"
    dest="$funcdir/$key"
    case $(lexnorm "$dest") in "$(lexnorm "$funcdir")"/*) ;; *) fail "$cfg: filePathMap key escapes the function directory: $key" ;; esac
    [[ ! -e "$dest" && ! -L "$dest" ]] || fail "$cfg: $dest already exists in the function directory"

    # The value is relative to the project root, as vercel computes it; join like node's path.join (a
    # leading `/` is not special), then require a regular file.
    src=$(lexnorm "$root/$value")
    [[ ! -L "$src" ]] || fail "$cfg: filePathMap source for $key is a symlink: $src"
    [[ -e "$src" ]] || fail "$cfg: filePathMap source for $key does not exist: $src (from $value)"
    [[ -f "$src" ]] || fail "$cfg: filePathMap source for $key is not a regular file: $src"

    mkdir -p -- "$(dirname "$dest")"
    cp -p -- "$src" "$dest" || fail "$cfg: cannot copy $src to $dest"
    # vercel rejects a prebuilt file that resolves outside the uploaded project (INVALID_PREBUILT_FILE_PATH);
    # make sure the copy really is a regular file physically inside its own function directory.
    [[ -f "$dest" && ! -L "$dest" ]] || fail "$cfg: copy of $key is not a regular file: $dest"
    case $(cd "$(dirname "$dest")" && pwd -P) in
      "$funcdir_real" | "$funcdir_real"/*) ;;
      *) fail "$cfg: copy of $key is not inside its function directory: $dest" ;;
    esac
    copied=$((copied + 1))
  done < <(jq -j '.filePathMap | to_entries[] | .key, "\u0000", .value, "\u0000"' "$cfg")

  tmp=$(mktemp "$funcdir/.vc-config.json.XXXXXX")
  jq --indent 2 'del(.filePathMap)' "$cfg" > "$tmp" || { rm -f -- "$tmp"; fail "cannot rewrite $cfg"; }
  mv -f -- "$tmp" "$cfg"
done < <(list_configs "$out")

assert_no_filepathmap "$out"
no_symlinks "after materializing"
echo "materialize-output: materialized $copied file(s) into $configs function(s); output is self-contained"
