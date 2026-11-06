#!/usr/bin/env bash
# Read changed paths on stdin and run the matching local component checks.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/meal-planner-check.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
cat > "$tmp/changed.txt"

CLASSIFY_ALL="${CLASSIFY_ALL:-false}" PREVIEW_TRUSTED=false PREVIEW_ENABLED=false \
  bash .github/scripts/classify.sh < "$tmp/changed.txt" > "$tmp/classified.txt" 2> "$tmp/reasons.txt"
cat "$tmp/reasons.txt"
# The classifier emits only fixed name=true|false assignments and is tested in CI.
source "$tmp/classified.txt"

need_go=false
if [[ "$hosted_go" == true || "$local_go" == true || "$contracts" == true ]]; then
  need_go=true
fi

if [[ "$need_go" == true ]]; then
  unformatted="$(gofmt -l -- $(git ls-files '*.go'))"
  [[ -z "$unformatted" ]] || { echo "gofmt required:" >&2; echo "$unformatted" >&2; exit 1; }
  go vet ./...
fi

if [[ "$hosted_go" == true ]]; then
  scripts/with-heavy-lock.sh go test ./api/... ./hosted/...
fi
if [[ "$local_go" == true ]]; then
  scripts/with-heavy-lock.sh go test ./internal/core/... ./internal/localstore/... ./internal/mcpadapter/... ./cmd/...
  scripts/with-heavy-lock.sh go build ./cmd/...
fi
if [[ "$contracts" == true ]]; then
  scripts/with-heavy-lock.sh go test ./contracts/...
fi

if [[ "$web" == true ]]; then
  if [[ ! -d web/node_modules ]]; then
    echo "web dependencies are missing; run: npm ci --prefix web" >&2
    exit 1
  fi
  npm run --prefix web lint
  npm run --prefix web typecheck
  scripts/with-heavy-lock.sh npm run --prefix web test
  scripts/with-heavy-lock.sh npm run --prefix web build
fi

if [[ "$integration" == true ]]; then
  echo "integration selected: disposable-Postgres tests run in CI"
fi
if [[ "$docs" == true && "$need_go" == false && "$web" == false ]]; then
  echo "docs-only change: no component test selected"
fi
echo "affected local checks: OK"
