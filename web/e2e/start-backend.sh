#!/usr/bin/env bash
# Test-only real Go/SQL boundary with a synthetic Framebriar transport.
set -euo pipefail
cd "$(dirname "$0")/../.."
FEEDBACK_BROWSER_FIXTURE=1 go test -tags=integration -race -run '^TestFeedbackBrowserServer$' -count=1 -timeout=17m -v ./hosted &
fixture_pid=$!
cleanup() {
  curl --silent --show-error --max-time 5 --request POST http://127.0.0.1:4182/__fixture/shutdown >/dev/null 2>&1 || true
  wait "$fixture_pid" || true
}
trap cleanup EXIT INT TERM
wait "$fixture_pid"
