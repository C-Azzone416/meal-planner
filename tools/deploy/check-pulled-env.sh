#!/usr/bin/env bash
# Check that env files written by `vercel pull` define only Vercel system variable NAMES.
# Usage: check-pulled-env.sh <file|dir>...   (a directory, e.g. .vercel, is searched for .env* files)
# Exit:  0 every name is allowlisted, 1 an unexpected name or an unparseable line, 2 usage or I/O error.
#
# For the token-bearing pull jobs. `vercel pull` writes .vercel/.env.<target>.local with a live
# VERCEL_OIDC_TOKEN, and this repository and its Actions logs are public. So this prints variable NAMES and
# line numbers only: never values, their lengths or prefixes, and never the text of a line it cannot parse.
# A name outside the allowlist means build-time env vars exist, which the env-free build job cannot receive,
# so the env handoff design must be revisited (docs/runbooks/hosted-deploy.md); it must not be fixed by widening the list.
#
# Parses dotenv format: blank lines, `#` comments, an optional `export ` prefix, NAME=value with `=` allowed
# in the value, single-, double- (with backslash escapes) and backtick-quoted values that may span lines,
# and CRLF line endings. Anything else fails closed (exit 1): an unterminated quote, a lone CR (a line break to
# dotenv, so it could hide a second assignment), a NUL byte, a name that is not uppercase [A-Z][A-Z0-9_]* (so a
# stray base64 line is never printed as a "name"), an unquoted value starting with `=` (base64 padding), and a
# .env* path that is a symlink or not a regular file. A file that cannot be opened or read exits 2.
#
# Allowlist: VERCEL_* plus these exact names, each listed as a system environment variable at
# https://vercel.com/docs/environment-variables/system-environment-variables
#   VERCEL  "An indicator to show that system environment variables have been exposed to your project's Deployments."
#   CI      "An indicator that the code is running in a Continuous Integration environment."
# Add an exact name only with a Vercel docs citation; no prefix families other than VERCEL_.
# Allowlisted names other than VERCEL_* are printed on a pass too, so the log shows which ones were present.
# Bash 3.2 compatible (macOS) so the test runs locally.
set -euo pipefail
export LC_ALL=C # byte semantics, so ${#var} counts bytes for the NUL check

allowed_exact=" VERCEL CI "
assign_re='^[[:space:]]*(export[[:space:]]+)?([A-Za-z_][A-Za-z0-9_]*)[[:space:]]*=[[:space:]]*(.*)$'
trailing_re='^[[:space:]]*(#.*)?$'

usage_error() { echo "::error::check-pulled-env: $*" >&2; exit 2; }

[[ $# -gt 0 ]] || usage_error "usage: check-pulled-env.sh <file|dir>..."

# find_close <text> <quote>: sets `close` to the index of the first unescaped <quote> in <text>, or -1.
# Only double quotes honour backslash escapes; a backslash at the end of a line escapes the newline.
find_close() {
  local s=$1 q=$2 i c esc=0
  close=-1
  for ((i = 0; i < ${#s}; i++)); do
    c=${s:i:1}
    if ((esc)); then esc=0; continue; fi
    if [[ $q == '"' && $c == "\\" ]]; then esc=1; continue; fi
    if [[ $c == "$q" ]]; then close=$i; return 0; fi
  done
}

# check_file <file>: prints names-only findings; returns 1 if any name is unexpected or any line unparseable.
check_file() {
  local f=$1 n=0 line value rest q="" name="" start=0 bad=0 count=0 extra=""
  record() { # record <name> <line>: the assignment of <name> starting at <line> parsed completely
    if ! [[ $1 =~ ^[A-Z][A-Z0-9_]*$ ]]; then unparseable "$2"; return 0; fi # never print a non-name
    count=$((count + 1))
    if [[ $1 =~ ^VERCEL_[A-Z0-9_]+$ ]]; then
      :
    elif [[ $allowed_exact == *" $1 "* ]]; then
      [[ ", $extra, " == *", $1, "* ]] || extra="${extra:+$extra, }$1"
    else
      echo "::error::check-pulled-env: $f line $2: unexpected variable name $1"
      bad=1
    fi
  }
  unparseable() { echo "::error::check-pulled-env: $f line $1: unparseable"; bad=1; }

  # Read the whole file first: cat reports open and read errors, which a `while read ...; done < file` loop
  # would turn into an early EOF and a pass. The trailing x keeps trailing newlines. Values stay in memory only.
  local bytes content
  bytes=$(wc -c < "$f") || usage_error "cannot read $f"
  content=$(cat -- "$f" && printf x) || usage_error "cannot read $f"
  content=${content%x}
  if ((${#content} != bytes)); then # command substitution drops NUL bytes
    echo "::error::check-pulled-env: $f contains NUL bytes: unparseable"
    bad=1
  fi

  while [[ -n $content ]]; do
    line=${content%%$'\n'*}
    if [[ $line == "$content" ]]; then content=""; else content=${content#*$'\n'}; fi
    n=$((n + 1))
    line=${line%$'\r'}
    if [[ $line == *$'\r'* ]]; then # a lone CR is a line break to dotenv
      unparseable "$n"
      continue
    fi
    if [[ -n $q ]]; then # inside a quoted value that started on line $start
      find_close "$line" "$q"
      ((close >= 0)) || continue
      rest=${line:close+1}
      q=""
      if [[ $rest =~ $trailing_re ]]; then record "$name" "$start"; else unparseable "$start"; fi
      continue
    fi
    [[ $line =~ $trailing_re ]] && continue # blank or comment
    if ! [[ $line =~ $assign_re ]]; then
      unparseable "$n"
      continue
    fi
    name=${BASH_REMATCH[2]}
    value=${BASH_REMATCH[3]}
    case ${value:0:1} in
      '"' | "'" | '`')
        q=${value:0:1}
        start=$n
        find_close "${value:1}" "$q"
        if ((close >= 0)); then
          rest=${value:close+2}
          q=""
          if [[ $rest =~ $trailing_re ]]; then record "$name" "$n"; else unparseable "$n"; fi
        fi
        ;;
      =*) unparseable "$n" ;; # base64 padding, not a value Vercel writes
      *) record "$name" "$n" ;;
    esac
  done
  [[ -z $q ]] || unparseable "$start" # unterminated quoted value

  if ((bad)); then
    echo "::error::check-pulled-env: $f defines a name outside the Vercel system variable allowlist or could not be parsed; build-time env vars exist, so this env handoff design must be revisited"
    return 1
  fi
  echo "ok $f: $count variable(s), all Vercel system variable names"
  echo "allowed system vars present (non-VERCEL_): ${extra:-none}"
}

# Collect files. A failing find (e.g. an unreadable directory) is an I/O error, so its output goes to a file
# and its status is checked, rather than reading it through a process substitution. A .env* entry that is a
# symlink or anything but a regular file is not checked, so it fails closed.
files=()
status=0
not_regular() { echo "::error::check-pulled-env: $1 is a symlink or not a regular file; refusing to check it"; status=1; }
list=$(mktemp) || usage_error "mktemp failed"
trap 'rm -f "$list"' EXIT
for arg in "$@"; do
  if [[ -L $arg ]]; then
    not_regular "$arg"
  elif [[ -d $arg ]]; then
    find "$arg" -name '.env*' ! -type f -print0 > "$list" || usage_error "find failed in $arg"
    while IFS= read -r -d '' f; do not_regular "$f"; done < "$list"
    find "$arg" -type f -name '.env*' -print0 > "$list" || usage_error "find failed in $arg"
    while IFS= read -r -d '' f; do files+=("$f"); done < "$list"
  elif [[ -f $arg ]]; then
    files+=("$arg")
  else
    usage_error "$arg is not a file or directory"
  fi
done

if [[ ${#files[@]} -eq 0 ]]; then
  echo "no env files found in: $*"
  exit "$status"
fi

for f in "${files[@]}"; do
  check_file "$f" || status=1
done
exit "$status"
