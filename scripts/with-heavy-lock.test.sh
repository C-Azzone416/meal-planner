#!/bin/sh
# shellcheck disable=SC2016 # single-quoted sh -c bodies are expanded by the inner shell on purpose
# Tests for with-heavy-lock.sh. The important property is that a second job NEVER starts while any
# process of the first job's process group is still alive, however the first job is stopped.
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
# wait without bash's "Killed: 9" job report
reap() { { wait "$1"; } 2> /dev/null; }

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
# contend NAME PIDFILE...: starts in the background; $tmp/contender-events gets "NAME ok" or "NAME OVERLAP <pid>".
contend() {
  name="$1"; shift
  rm -f "$tmp/contender-events"
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
still_waiting() { [ ! -e "$tmp/contender-events" ] && alive "$contender_pid"; }
hold_for() { # hold_for SECONDS MESSAGE: the contender must not start for that long
  end=$(($(date +%s) + $1))
  while [ "$(date +%s)" -lt "$end" ]; do
    still_waiting || fail "$2 (contender started or died while the first job was alive)"
    sleep 0.1
  done
}
expect_contender() { # expect_contender NAME: it ran, with no overlap
  wait_for "contender $1 to run" file_nonempty "$tmp/contender-events"
  reap "$contender_pid"
  [ "$(cat "$tmp/contender-events")" = "$1 ok" ] || { cat "$tmp/contender-events" >&2; fail "overlap: $1"; }
}
start_victim() { # start_victim SCRIPT: runs SCRIPT (sh -c) as the workload; sets victim_wrapper
  rm -f "$tmp/child.pid" "$tmp/main.pid"
  HEAVY_LOCK_LABEL=victim "$wrapper" sh -c "$1" sh "$tmp/child.pid" "$tmp/main.pid" 2>/dev/null &
  victim_wrapper=$!
  pids_to_kill="$pids_to_kill $victim_wrapper"
  wait_for "victim main pid" file_nonempty "$tmp/main.pid"
}
track_children() { pids_to_kill="$pids_to_kill $(cat "$tmp/child.pid" 2>/dev/null || true) $(cat "$tmp/main.pid" 2>/dev/null || true)"; }

# 4. The command is SIGKILLed while a child that does NOT hold fd 9 keeps running (this is what Node's
#    child_process.spawn produces: it does not pass extra descriptors on). The wrapper must keep the lock
#    until that child is gone.
start_victim 'sleep 60 9>&- & echo $! > "$1"; echo $$ > "$2"; wait'
wait_for "victim child pid" file_nonempty "$tmp/child.pid"
track_children
contend after-command-kill "$tmp/child.pid" "$tmp/main.pid"
hold_for 1 "contender must wait while the workload runs"
kill -9 "$(cat "$tmp/main.pid")"
hold_for 2 "contender must wait while a child without fd 9 outlives the command"
alive "$victim_wrapper" || fail "the wrapper must stay until the process group is empty"
kill "$(cat "$tmp/child.pid")"
expect_contender after-command-kill
reap "$victim_wrapper" || true

# 5. The same with real Node child_process.spawn, when node is installed.
if command -v node > /dev/null 2>&1; then
  start_victim 'node -e "
    const { spawn } = require(\"child_process\");
    const child = spawn(\"sleep\", [\"60\"], { stdio: \"ignore\" });
    require(\"fs\").writeFileSync(process.argv[1], String(child.pid));
    require(\"fs\").writeFileSync(process.argv[2], String(process.pid));
    setInterval(() => {}, 1000);
  " "$1" "$2"'
  wait_for "node child pid" file_nonempty "$tmp/child.pid"
  track_children
  contend after-node-kill "$tmp/child.pid" "$tmp/main.pid"
  hold_for 1 "contender must wait while node runs"
  kill -9 "$(cat "$tmp/main.pid")"
  hold_for 2 "contender must wait while node's spawned child outlives node"
  kill "$(cat "$tmp/child.pid")"
  expect_contender after-node-kill
  reap "$victim_wrapper" || true
else
  echo "skip: node is not installed, so the Node spawn case was not run"
fi

# 6. SIGKILL the wrapper itself: the command still holds fd 9, so the lock stays held while it runs.
start_victim 'echo $$ > "$2"; exec sleep 60'
track_children
contend after-wrapper-kill "$tmp/main.pid"
hold_for 1 "contender must wait while the workload runs"
kill -9 "$victim_wrapper"
reap "$victim_wrapper" || true
hold_for 2 "contender must wait after the wrapper is SIGKILLed while the command still runs"
kill "$(cat "$tmp/main.pid")"
expect_contender after-wrapper-kill

# 7. SIGTERM to the wrapper is forwarded to the whole group (children included); the wrapper waits for the group.
start_victim 'sleep 60 9>&- & echo $! > "$1"; echo $$ > "$2"; wait'
wait_for "victim child pid" file_nonempty "$tmp/child.pid"
track_children
contend after-sigterm "$tmp/child.pid" "$tmp/main.pid"
hold_for 1 "contender must wait while the workload runs"
kill -TERM "$victim_wrapper"
expect_contender after-sigterm
status=0
reap "$victim_wrapper" || status=$?
[ "$status" -ne 0 ] || fail "TERM: the wrapper should report the signal"
dead "$(cat "$tmp/child.pid")" || fail "SIGTERM was not forwarded to the whole group"

# SIGINT likewise (as Ctrl-C reaches the wrapper). An asynchronous job of a non-interactive shell ignores
# SIGINT, so start the wrapper under job control, which restores the default as for a command run from a terminal.
set -m
start_victim 'echo $$ > "$2"; exec sleep 60'
set +m
track_children
contend after-sigint "$tmp/main.pid"
hold_for 1 "contender must wait while the workload runs"
kill -INT "$victim_wrapper"
expect_contender after-sigint
status=0
reap "$victim_wrapper" || status=$?
[ "$status" -ne 0 ] || fail "INT: the wrapper should report the signal"

# 8. A contender that is itself killed while waiting neither stops the holder nor leaks the lock.
rm -f "$tmp/main.pid"
HEAVY_LOCK_LABEL=holder "$wrapper" sh -c 'echo $$ > "$1"; exec sleep 3' sh "$tmp/main.pid" 2>/dev/null &
holder=$!
pids_to_kill="$pids_to_kill $holder"
wait_for "holder pid" file_nonempty "$tmp/main.pid"
HEAVY_LOCK_LABEL=impatient "$wrapper" sh -c 'echo ran >> "$1"' sh "$tmp/impatient" 2>/dev/null &
impatient=$!
sleep 0.5
kill -9 "$impatient"
reap "$impatient" || true
alive "$holder" || fail "killing a waiting contender must not stop the holder"
wait "$holder"
HEAVY_LOCK_LABEL=after "$wrapper" sh -c 'exit 0' 2>/dev/null

# 9. The owner metadata is best-effort: unreadable, stale or garbage never breaks a contender, and a normal
#    exit removes it.
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
HEAVY_LOCK_LABEL=tidy "$wrapper" sh -c 'exit 0' 2>/dev/null
[ ! -e "$lock.owner" ] || fail "owner metadata was not cleaned up after a normal exit"

echo "with-heavy-lock: OK"
