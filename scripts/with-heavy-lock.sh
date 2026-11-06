#!/bin/sh
# Serialize resource-heavy local work across Caroline's repositories.
# macOS lockf owns the lock in the kernel, so a crash or killed command releases it.
set -eu

if [ "$#" -eq 0 ]; then
  echo "usage: $0 command [arguments ...]" >&2
  exit 64
fi

if [ "${CI:-}" = "true" ] || [ "${VERCEL:-}" = "1" ] || [ -n "${CAROLINE_HEAVY_LOCK_HELD:-}" ]; then
  exec "$@"
fi

lock_root="${CAROLINE_HEAVY_LOCK_ROOT:-${TMPDIR:-/tmp}}"
lock_file="${CAROLINE_HEAVY_LOCK_FILE:-${lock_root%/}/caroline-development-heavy.lock}"
owner_file="${lock_file}.owner"
label="${HEAVY_LOCK_LABEL:-$(basename "$(git rev-parse --show-toplevel 2>/dev/null || pwd)")}"

if [ -n "${CAROLINE_HEAVY_LOCK_ACQUIRED:-}" ]; then
  token="$CAROLINE_HEAVY_LOCK_ACQUIRED"
  cleanup() {
    if [ -f "$owner_file" ] && [ "$(sed -n '1p' "$owner_file" 2>/dev/null)" = "token=$token" ]; then
      rm -f "$owner_file"
    fi
  }
  trap cleanup EXIT HUP INT TERM
  {
    echo "token=$token"
    echo "pid=$$"
    echo "repo=$label"
    echo "started=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  } > "$owner_file"
  echo "heavy-lock: acquired for $label"
  export CAROLINE_HEAVY_LOCK_HELD="$token"
  set +e
  "$@"
  status=$?
  set -e
  echo "heavy-lock: released for $label (status $status)"
  exit "$status"
fi

if [ ! -x /usr/bin/lockf ]; then
  echo "heavy-lock: /usr/bin/lockf is required on this Mac" >&2
  exit 69
fi

if [ -f "$owner_file" ]; then
  waiting_for="$(sed -n '3s/^repo=//p' "$owner_file" 2>/dev/null)"
  echo "heavy-lock: waiting for ${waiting_for:-another repository}"
fi

token="$$-$(date +%s)"
exec /usr/bin/lockf -k "$lock_file" /usr/bin/env \
  CAROLINE_HEAVY_LOCK_ACQUIRED="$token" \
  HEAVY_LOCK_LABEL="$label" \
  "$0" "$@"
