#!/usr/bin/env bash
# Scan git history for secrets with the gitleaks CLI, redacted.
#
#   secret-scan.sh BASE..HEAD            the commits a push or pull request adds
#   secret-scan.sh SHA --not --remotes   a new branch: everything no remote has yet
#   secret-scan.sh SHA                   a commit and all of its history
#
# Every argument is a `git log` revision argument. `-m` makes merge commits show their changes against
# each parent, so a secret that only appears in a merge (a conflict resolution) is found; without it
# git log shows no diff for merges. `--full-history` keeps every side branch in the range.
#
# Exit codes: 0 clean, 1 findings or scan error, 64 usage, 69 gitleaks is not installed.
set -euo pipefail

if (( $# == 0 )); then
  echo "usage: $0 BASE..HEAD | SHA [--not --remotes]" >&2
  exit 64
fi
if ! command -v gitleaks > /dev/null 2>&1; then
  echo "secret scan: gitleaks is not installed" >&2
  exit 69
fi

root="$(git rev-parse --show-toplevel)"
cd "$root"
exec gitleaks git --no-banner --redact --log-opts="-m --full-history $*"
