#!/usr/bin/env bash
# Validate a tar archive and extract it into a fresh, empty directory.
# Usage: safe-extract.sh <tar> <dest>
# Env:   SAFE_EXTRACT_ALLOW_RE  optional ERE; every entry name must match it (e.g. '^\.vercel/(output(/|$)|project\.json$)')
#
# For jobs that hold a secret and unpack an archive built by a job that ran untrusted code. Rejects, before
# anything is written, any entry that is not a regular file or directory (symlinks, hardlinks, devices, fifos can
# redirect later entries), and any name that is absolute or contains a `..` component. Extraction
# then goes only into <dest>, which must be absent or empty, with ownership and permissions not restored.
# Fails closed: unparseable listing lines are rejected too.
#
# Hardlink rejection relies on GNU tar's verbose listing showing hardlinks with type `h` (so the first-character
# check below rejects them). The GitHub-hosted ubuntu runners use GNU tar; bsdtar lists them differently, so do
# not run this where `tar` is bsdtar without re-checking (tools/deploy/safe-extract_test.sh covers it).
set -euo pipefail

tarfile=${1:?usage: safe-extract.sh <tar> <dest>}
dest=${2:?usage: safe-extract.sh <tar> <dest>}
allow_re=${SAFE_EXTRACT_ALLOW_RE:-}

fail() { echo "::error::safe-extract: $*" >&2; exit 1; }

[[ -f "$tarfile" && ! -L "$tarfile" ]] || fail "$tarfile is not a regular file"

# Entry types: the first character of each verbose listing line must be '-' (file) or 'd' (directory).
verbose=$(tar -tvf "$tarfile") || fail "cannot list $tarfile"
bad_types=$(printf '%s\n' "$verbose" | grep -v -E '^[-d]' || true)
[[ -z "$bad_types" ]] || fail "archive has entries that are not regular files or directories: $(printf '%s' "$bad_types" | head -n3 | tr '\n' ';')"

# Entry names.
names=$(tar -tf "$tarfile") || fail "cannot list $tarfile"
[[ -n "$names" ]] || fail "archive is empty"
while IFS= read -r name; do
  [[ "$name" != /* ]] || fail "absolute path in archive: $name"
  [[ ! "$name" =~ (^|/)\.\.(/|$) ]] || fail "'..' path component in archive: $name"
  if [[ -n "$allow_re" ]] && ! [[ "$name" =~ $allow_re ]]; then
    fail "unexpected entry in archive: $name"
  fi
done <<< "$names"

# Destination: fresh and empty.
if [[ -e "$dest" || -L "$dest" ]]; then
  [[ -d "$dest" && ! -L "$dest" ]] || fail "$dest exists and is not a plain directory"
  [[ -z "$(ls -A "$dest")" ]] || fail "$dest is not empty"
else
  mkdir -p "$dest"
fi

tar -xf "$tarfile" -C "$dest" --no-same-owner --no-same-permissions

# Belt and braces: nothing but regular files and directories may have landed.
extra=$(find "$dest" -mindepth 1 ! -type f ! -type d -print -quit)
[[ -z "$extra" ]] || fail "unexpected non-regular file after extraction: $extra"
echo "safe-extract: extracted $(printf '%s\n' "$names" | wc -l | tr -d ' ') entries into $dest"
