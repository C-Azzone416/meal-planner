#!/bin/sh
# Serialize resource-heavy local work across Caroline's repositories (macOS).
#
# The lock is held until the WHOLE workload is gone, not just its main process:
#
#  * The wrapper takes a flock(2) lock on fd 9 and keeps it for its whole life. The command inherits fd 9
#    too, so if the wrapper is SIGKILLed the lock stays held while the command or any child that kept fd 9
#    is alive.
#  * The command runs in its own process group. Some runtimes (Node's child_process.spawn, for one) do not
#    pass fd 9 on to the processes they start, so after the command dies its children may run on without the
#    descriptor. The wrapper therefore stays as the parent, waits for the command, then keeps the lock until
#    the process group is empty.
#  * SIGINT, SIGTERM and SIGHUP sent to the wrapper are forwarded to the whole group; the wrapper then waits
#    for the group as above.
#
# Residual risks (also in docs/development-workflow.md): if the wrapper and every holder of fd 9 are SIGKILLed,
# processes that no longer hold fd 9 can outlive the lock; processes that leave the group (setsid, detached
# spawns, daemons) are not tracked; a command that ignores SIGTERM keeps the lock until it exits.
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

token="$$-$(date +%s)"
# Informational only. Written to a temporary file and renamed so a reader never sees a partial file;
# readers check that the pid is still alive.
{
  echo "token=$token"
  echo "pid=$$"
  echo "repo=$label"
  echo "started=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
} 2>/dev/null > "$owner_file.$$" && mv -f "$owner_file.$$" "$owner_file" 2>/dev/null || rm -f "$owner_file.$$" 2>/dev/null || true

cleanup_owner() {
  if [ "$(sed -n '1p' "$owner_file" 2>/dev/null || true)" = "token=$token" ]; then
    rm -f "$owner_file" 2>/dev/null || true
  fi
}

echo "heavy-lock: acquired for $label" >&2
CAROLINE_HEAVY_LOCK_HELD="$$"
export CAROLINE_HEAVY_LOCK_HELD

# Job control gives the command its own process group (pgid == its pid). A background process group cannot
# read the terminal, so a terminal stdin is replaced by /dev/null.
set -m
if [ -t 0 ]; then
  "$@" < /dev/null &
else
  "$@" &
fi
child=$!

group="$(ps -o pgid= -p "$child" 2>/dev/null | tr -d ' ' || true)"
if [ "$group" = "$child" ]; then
  tracked=true
else
  tracked=false
  echo "heavy-lock: could not put the command in its own process group; holding the lock only while the command itself runs" >&2
fi

forward() {
  if [ "$tracked" = true ]; then
    kill -s "$1" -- "-$child" 2>/dev/null || true
  else
    kill -s "$1" "$child" 2>/dev/null || true
  fi
}
trap 'forward INT' INT
trap 'forward TERM' TERM
trap 'forward HUP' HUP

# `wait` returns early when a trapped signal arrives; keep waiting until the command has really exited.
status=0
while :; do
  { wait "$child"; } 2> /dev/null && status=0 || status=$?
  kill -0 "$child" 2>/dev/null || break
done

# The command is gone. Keep the lock until nothing is left in its process group.
if [ "$tracked" = true ] && kill -0 -- "-$child" 2>/dev/null; then
  echo "heavy-lock: waiting for processes the command left running" >&2
  while kill -0 -- "-$child" 2>/dev/null; do
    sleep 0.2
  done
fi

cleanup_owner
echo "heavy-lock: released for $label (status $status)" >&2
exit "$status"
