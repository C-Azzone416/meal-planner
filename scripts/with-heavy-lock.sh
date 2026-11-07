#!/bin/sh
# Serialize resource-heavy local work across Caroline's repositories (macOS).
#
# Lifetime guarantee: the lock is a flock(2) lock on an open file description (fd 9). The command
# is exec'd with that descriptor inherited, so the lock is held for as long as the command, or any
# process it started that still has fd 9 open, is alive. Killing this wrapper (SIGKILL, SIGTERM) is
# killing the command itself because they are the same process; there is no outer lock holder whose
# death could release the lock while the command keeps running. A crash releases the lock because the
# kernel closes the descriptor.
#
# A background process that the command leaves behind keeps the lock until it exits.
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

if [ ! -x /usr/bin/lockf ]; then
  echo "heavy-lock: /usr/bin/lockf is required on this Mac" >&2
  exit 69
fi

exec 9>>"$lock_file"

# Wait in short slices so that a wrapper killed while waiting leaves at most a one-second helper behind.
announced=false
while :; do
  status=0
  /usr/bin/lockf -s -t 1 9 || status=$?
  [ "$status" -eq 0 ] && break
  if [ "$status" -ne 75 ]; then
    echo "heavy-lock: could not lock $lock_file (lockf status $status)" >&2
    exit "$status"
  fi
  if [ "$announced" = false ]; then
    announced=true
    # Best-effort: the owner file can vanish or change at any moment, and it can be stale after a crash.
    owner_pid="$(sed -n '2s/^pid=//p' "$owner_file" 2>/dev/null || true)"
    owner_repo="$(sed -n '3s/^repo=//p' "$owner_file" 2>/dev/null || true)"
    if [ -n "$owner_pid" ] && kill -0 "$owner_pid" 2>/dev/null && [ -n "$owner_repo" ]; then
      echo "heavy-lock: waiting for $owner_repo" >&2
    else
      echo "heavy-lock: waiting for another repository" >&2
    fi
  fi
done

# Informational only. Written to a temporary file and renamed so a reader never sees a partial file.
# Not removed on exit (this process becomes the command); readers check that the pid is still alive.
{
  echo "token=$$-$(date +%s)"
  echo "pid=$$"
  echo "repo=$label"
  echo "started=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
} 2>/dev/null > "$owner_file.$$" && mv -f "$owner_file.$$" "$owner_file" 2>/dev/null || rm -f "$owner_file.$$" 2>/dev/null || true

echo "heavy-lock: acquired for $label" >&2
CAROLINE_HEAVY_LOCK_HELD="$$"
export CAROLINE_HEAVY_LOCK_HELD
exec "$@"
