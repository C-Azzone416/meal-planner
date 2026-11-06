#!/usr/bin/env bash
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

failed=0
while IFS= read -r -d '' path; do
  [[ -f "$path" ]] || continue
  case "$path" in
    .env.example|*/.env.example) ;;
    .env|.env.*|*/.env|*/.env.*|*.db|*.db-*|*.sqlite|*.sqlite3|*/node_modules/*|*/dist/*|*/coverage/*)
      echo "repository hygiene: forbidden tracked file: $path" >&2
      failed=1
      ;;
  esac
  if [[ "$(uname -s)" == Darwin ]]; then
    size="$(stat -f %z "$path")"
  else
    size="$(stat -c %s "$path")"
  fi
  if (( size > 1048576 )); then
    echo "repository hygiene: tracked file exceeds 1 MiB: $path ($size bytes)" >&2
    failed=1
  fi
done < <(git ls-files -z)

if (( failed != 0 )); then
  exit 1
fi
echo "repository hygiene: OK"
