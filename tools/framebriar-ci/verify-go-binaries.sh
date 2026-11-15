#!/usr/bin/env bash
# Trusted-side binary checks. Fully consume producer output before inspecting it:
# an early-closing head pipe can SIGPIPE a successful go command under pipefail.
set -euo pipefail
[[ $# == 1 && -d "$1" ]] || { echo 'Expected compiled functions directory' >&2; exit 1; }
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# Do not use process substitution: a failing find would otherwise be invisible
# to the loop and could turn an incomplete inventory into a successful scan.
find "$1" -name bootstrap -type f -print0 > "$work/binaries" || exit "$?"
count=0
while IFS= read -r -d '' binary; do
  go version -m "$binary" > "$work/metadata" || exit "$?"
  first=''
  IFS= read -r first < "$work/metadata" || [[ -n "$first" ]]
  [[ "$first" =~ go1\.26\.11$ ]] || { echo 'Compiled Go version does not match required go1.26.11' >&2; exit 1; }
  go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 -mode=binary "$binary" || exit "$?"
  count=$((count + 1))
done < "$work/binaries"
[[ "$count" -gt 0 ]] || { echo 'No compiled Go bootstraps found' >&2; exit 1; }
# The subsequent closed-output packager independently requires the exact layout.
