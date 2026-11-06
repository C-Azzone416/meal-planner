#!/bin/sh
set -eu

root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
wrapper="$root/scripts/with-heavy-lock.sh"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/meal-planner-lock-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
lock="$tmp/heavy.lock"
events="$tmp/events"

CAROLINE_HEAVY_LOCK_FILE="$lock" HEAVY_LOCK_LABEL=first \
  "$wrapper" sh -c 'echo first-start >> "$1"; sleep 1; echo first-end >> "$1"' sh "$events" &
first_pid=$!

while ! grep -q '^first-start$' "$events" 2>/dev/null; do sleep 0.05; done
CAROLINE_HEAVY_LOCK_FILE="$lock" HEAVY_LOCK_LABEL=second \
  "$wrapper" sh -c 'echo second >> "$1"' sh "$events"
wait "$first_pid"

expected='first-start
first-end
second'
actual="$(cat "$events")"
[ "$actual" = "$expected" ] || {
  echo "lock did not serialize commands:" >&2
  cat "$events" >&2
  exit 1
}

CAROLINE_HEAVY_LOCK_FILE="$lock" HEAVY_LOCK_LABEL=outer \
  "$wrapper" "$wrapper" sh -c 'exit 0'
VERCEL=1 CAROLINE_HEAVY_LOCK_FILE=/not/writable/heavy.lock \
  "$wrapper" sh -c 'exit 0'

command_pid_file="$tmp/crash-command.pid"
CAROLINE_HEAVY_LOCK_FILE="$lock" HEAVY_LOCK_LABEL=crash \
  "$wrapper" sh -c 'echo $$ > "$1"; exec sleep 30' sh "$command_pid_file" &
lockf_pid=$!
while [ ! -s "$lock.owner" ] || [ ! -s "$command_pid_file" ]; do sleep 0.05; done
owner_pid="$(sed -n '2s/^pid=//p' "$lock.owner")"
kill -9 "$owner_pid"
wait "$lockf_pid" 2>/dev/null || true
CAROLINE_HEAVY_LOCK_FILE="$lock" HEAVY_LOCK_LABEL=recovered "$wrapper" sh -c 'exit 0'
kill "$(cat "$command_pid_file")" 2>/dev/null || true

set +e
CAROLINE_HEAVY_LOCK_FILE="$lock" HEAVY_LOCK_LABEL=failure "$wrapper" sh -c 'exit 23'
status=$?
set -e
[ "$status" -eq 23 ] || { echo "expected status 23, got $status" >&2; exit 1; }
[ ! -e "$lock.owner" ] || { echo "owner metadata was not cleaned up" >&2; exit 1; }

echo "with-heavy-lock: OK"
