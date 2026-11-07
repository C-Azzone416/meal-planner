#!/bin/sh
# shellcheck disable=SC2016 # single-quoted sh -c bodies are expanded by the inner shell on purpose
# Tests for with-heavy-lock.sh. The important property is that a second job NEVER starts while any
# process of the first job is still alive, however the first job's wrapper is stopped.
set -eu

root="$(CDPATH="" cd -- "$(dirname "$0")/.." && pwd)"
wrapper="$root/scripts/with-heavy-lock.sh"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/meal-planner-lock-test.XXXXXX")"
pids_to_kill=""
cleanup() {
  for pid in $pids_to_kill; do kill -9 "$pid" 2>/dev/null || true; done
  rm -rf "$tmp"
}
trap cleanup EXIT HUP INT TERM
export CAROLINE_HEAVY_LOCK_FILE="$tmp/heavy.lock"
lock="$CAROLINE_HEAVY_LOCK_FILE"

fail() { echo "FAIL: $*" >&2; exit 1; }

wait_for() { # wait_for description command...   (up to ~10s)
  description="$1"; shift
  n=0
  until "$@"; do
    n=$((n + 1))
    [ "$n" -le 200 ] || fail "timed out waiting for $description"
    sleep 0.05
  done
}
file_nonempty() { [ -s "$1" ]; }
alive() { kill -0 "$1" 2>/dev/null; }
dead() { ! alive "$1"; }

# 1. Two jobs are serialized.
events="$tmp/events"
HEAVY_LOCK_LABEL=first "$wrapper" sh -c 'echo first-start >> "$1"; sleep 1; echo first-end >> "$1"' sh "$events" 2>/dev/null &
first_pid=$!
wait_for "first job" grep -qs "^first-start$" "$events"
HEAVY_LOCK_LABEL=second "$wrapper" sh -c 'echo second >> "$1"' sh "$events" 2>/dev/null
wait "$first_pid"
[ "$(cat "$events")" = "first-start
first-end
second" ] || { cat "$events" >&2; fail "lock did not serialize commands"; }

# 2. Nested wrappers do not deadlock; CI/Vercel bypass the lock entirely.
HEAVY_LOCK_LABEL=outer "$wrapper" "$wrapper" sh -c 'exit 0' 2>/dev/null
VERCEL=1 CAROLINE_HEAVY_LOCK_FILE=/not/writable/heavy.lock "$wrapper" sh -c 'exit 0'
CI=true CAROLINE_HEAVY_LOCK_FILE=/not/writable/heavy.lock "$wrapper" sh -c 'exit 0'

# 3. Exit status passes through.
status=0
HEAVY_LOCK_LABEL=failure "$wrapper" sh -c 'exit 23' 2>/dev/null || status=$?
[ "$status" -eq 23 ] || fail "expected status 23, got $status"

# A contender that records whether the previous job's processes are still alive when it starts.
# contend NAME PIDFILE... : starts in the background; its events file gets "NAME ok" or "NAME OVERLAP <pid>".
contend() {
  name="$1"; shift
  HEAVY_LOCK_LABEL="$name" "$wrapper" sh -c '
    name="$1"; out="$2"; shift 2
    for pidfile in "$@"; do
      pid="$(cat "$pidfile")"
      if kill -0 "$pid" 2>/dev/null; then echo "$name OVERLAP $pid" >> "$out"; exit 1; fi
    done
    echo "$name ok" >> "$out"
  ' sh "$name" "$tmp/contender-events" "$@" 2>/dev/null &
  contender_pid=$!
  pids_to_kill="$pids_to_kill $contender_pid"
}
still_waiting() { # the contender has not started and is still running
  [ ! -e "$tmp/contender-events" ] && alive "$contender_pid"
}
hold_for() { # hold_for SECONDS command...: assert still_waiting for that long
  end=$(($(date +%s) + $1))
  while [ "$(date +%s)" -lt "$end" ]; do
    still_waiting || fail "$2 (contender started or died while the first job was alive)"
    sleep 0.1
  done
}

# 4. SIGKILL the wrapper (= the workload's main process) while a descendant of the workload is still
#    running: the contender must keep waiting until that descendant is gone. No overlap.
: > "$tmp/contender-events"; rm -f "$tmp/contender-events"
HEAVY_LOCK_LABEL=victim "$wrapper" sh -c '
  sleep 60 &
  echo $! > "$1"
  echo $$ > "$2"
  wait
' sh "$tmp/child.pid" "$tmp/main.pid" 2>/dev/null &
victim_wrapper=$!
pids_to_kill="$pids_to_kill $victim_wrapper"
wait_for "victim pids" file_nonempty "$tmp/child.pid"
wait_for "victim main pid" file_nonempty "$tmp/main.pid"
pids_to_kill="$pids_to_kill $(cat "$tmp/child.pid")"
[ "$(cat "$tmp/main.pid")" = "$victim_wrapper" ] || fail "the wrapper must become the workload (same pid)"
contend after-sigkill "$tmp/child.pid" "$tmp/main.pid"
hold_for 2 "contender must wait while the workload runs"
kill -9 "$victim_wrapper"
wait "$victim_wrapper" 2>/dev/null || true
hold_for 2 "contender must wait while the workload's child still runs after the wrapper was SIGKILLed"
kill "$(cat "$tmp/child.pid")"
wait_for "contender to run once the workload is gone" file_nonempty "$tmp/contender-events"
wait "$contender_pid"
[ "$(cat "$tmp/contender-events")" = "after-sigkill ok" ] || { cat "$tmp/contender-events" >&2; fail "overlap after SIGKILL"; }

# 5. SIGTERM the running job: it stops, and only then can the contender run.
rm -f "$tmp/contender-events" "$tmp/child.pid" "$tmp/main.pid"
HEAVY_LOCK_LABEL=victim "$wrapper" sh -c 'echo $$ > "$1"; exec sleep 60' sh "$tmp/main.pid" 2>/dev/null &
victim_wrapper=$!
pids_to_kill="$pids_to_kill $victim_wrapper"
wait_for "victim main pid" file_nonempty "$tmp/main.pid"
contend after-sigterm "$tmp/main.pid"
hold_for 2 "contender must wait while the workload runs"
kill -TERM "$victim_wrapper"
wait "$victim_wrapper" 2>/dev/null || true
wait_for "contender to run after SIGTERM" file_nonempty "$tmp/contender-events"
wait "$contender_pid"
[ "$(cat "$tmp/contender-events")" = "after-sigterm ok" ] || { cat "$tmp/contender-events" >&2; fail "overlap after SIGTERM"; }

# 6. A contender that is itself killed while waiting neither crashes the holder nor leaks the lock.
rm -f "$tmp/contender-events" "$tmp/main.pid"
HEAVY_LOCK_LABEL=holder "$wrapper" sh -c 'echo $$ > "$1"; exec sleep 3' sh "$tmp/main.pid" 2>/dev/null &
holder=$!
pids_to_kill="$pids_to_kill $holder"
wait_for "holder pid" file_nonempty "$tmp/main.pid"
HEAVY_LOCK_LABEL=impatient "$wrapper" sh -c 'echo ran >> "$1"' sh "$tmp/impatient" 2>/dev/null &
impatient=$!
sleep 0.5
kill -9 "$impatient"
wait "$impatient" 2>/dev/null || true
alive "$holder" || fail "killing a waiting contender must not stop the holder"
wait "$holder"
HEAVY_LOCK_LABEL=after "$wrapper" sh -c 'exit 0' 2>/dev/null

# 7. The owner metadata is best-effort: an unreadable, stale or garbage owner file never breaks a contender.
rm -f "$lock.owner"
HEAVY_LOCK_LABEL=holder "$wrapper" sh -c 'echo $$ > "$1"; exec sleep 2' sh "$tmp/main.pid" 2>/dev/null &
holder=$!
pids_to_kill="$pids_to_kill $holder"
wait_for "holder owner file" file_nonempty "$lock.owner"
chmod 000 "$lock.owner"
HEAVY_LOCK_LABEL=reader "$wrapper" sh -c 'exit 0' 2>"$tmp/reader.err" || fail "unreadable owner file must not fail the contender"
grep -q 'waiting for another repository' "$tmp/reader.err" || fail "expected a generic waiting message"
wait "$holder"
printf 'garbage\n' > "$lock.owner"
HEAVY_LOCK_LABEL=reader "$wrapper" sh -c 'exit 0' 2>/dev/null || fail "garbage owner file must not fail the wrapper"
rm -f "$lock.owner"

echo "with-heavy-lock: OK"
