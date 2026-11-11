#!/usr/bin/env bash
# Execute the documented recovery command with a fake curl. No network or real credentials.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"
awk '/^# begin token-safe preview alias repair$/{copy=1;next} /^# end token-safe preview alias repair$/{copy=0} copy{print}' \
  "$root/docs/runbooks/hosted-deploy.md" > "$work/example.sh"
[[ -s "$work/example.sh" ]] || { echo 'missing runbook example' >&2; exit 1; }
cat > "$work/bin/curl" <<'CURL'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$@" > "$CURL_TEST_ARGS"
cat > "$CURL_TEST_STDIN"
CURL
chmod +x "$work/bin/curl"
export VERCEL_TOKEN=synthetic-test-token-not-a-credential
export CURL_TEST_ARGS="$work/argv" CURL_TEST_STDIN="$work/stdin"
PATH="$work/bin:$PATH" bash "$work/example.sh"
if grep -Fq "$VERCEL_TOKEN" "$CURL_TEST_ARGS"; then
  echo 'token exposed in curl arguments' >&2
  exit 1
fi
grep -Fxq -- '--config' "$CURL_TEST_ARGS"
grep -Fxq -- '-' "$CURL_TEST_ARGS"
grep -Fxq 'header = "Authorization: Bearer synthetic-test-token-not-a-credential"' "$CURL_TEST_STDIN"
echo 'runbook token travels through stdin, not argv'
