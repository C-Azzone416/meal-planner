#!/usr/bin/env bash
# Tests for classify.sh: every row of the selection matrix.
# Run: bash .github/scripts/classify_test.sh
set -uo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
fail=0

# check NAME "paths (newline separated)" "expected: web hosted_go local_go contracts integration hosted_preview" [ENV=VAL ...]
check() {
  local name="$1" paths="$2" want="$3"; shift 3
  local out got
  out="$(printf '%s\n' "$paths" | env "$@" bash "$here/classify.sh" 2>/dev/null)" || { echo "FAIL $name: script error"; fail=1; return; }
  got=""
  for k in web hosted_go local_go contracts integration hosted_preview; do
    v="$(sed -n "s/^$k=//p" <<<"$out")"
    [[ "$v" == true ]] && got+="$k "
  done
  got="${got% }"
  if [[ "$got" == "$want" ]]; then echo "ok   $name"; else echo "FAIL $name: want [$want] got [$got]"; fail=1; fi
}

T="PREVIEW_TRUSTED=true PREVIEW_ENABLED=true"
# shellcheck disable=SC2086
check "docs only (docs/)"        "docs/ci-cd-plan.md"            ""
check "docs only (root md)"      "README.md"                     ""
check "docs trusted+enabled"     "docs/a.md"                     "" $T
check "web only"                 "web/src/App.tsx"               "web"
check "web preview off"          "web/src/App.tsx"               "web" PREVIEW_TRUSTED=true
check "web preview on"           "web/src/App.tsx"               "web hosted_preview" $T
check "web preview untrusted"    "web/src/App.tsx"               "web" PREVIEW_ENABLED=true
check "hosted api"               "api/health.go"                 "hosted_go local_go contracts integration"
check "hosted pkg"               "hosted/x/y.go"        "hosted_go local_go contracts integration"
check "vercel.json"              "vercel.json"                   "hosted_go local_go contracts integration"
check "hosted preview on"        "api/health.go"                 "hosted_go local_go contracts integration hosted_preview" $T
check "cli only"                 "cmd/mealplanner/main.go"          "local_go contracts integration"
check "cli preview never"        "cmd/mealplanner/main.go"          "local_go contracts integration" $T
check "mcp only"                 "cmd/mealplanner-mcp/main.go"      "local_go contracts integration"
check "persistence/migration"    "hosted/db/migrations/001.sql" "hosted_go local_go contracts integration"
check "localstore"               "internal/localstore/store.go"  "local_go contracts integration"
check "localstore preview never" "internal/localstore/store.go"  "local_go contracts integration" $T
check "core"                     "internal/core/recipe.go"       "hosted_go local_go contracts integration"
check "core preview on"          "internal/core/recipe.go"       "hosted_go local_go contracts integration hosted_preview" $T
check "contracts"                "contracts/openapi.yaml"        "web hosted_go local_go contracts integration"
check "contracts preview on"     "contracts/openapi.yaml"        "web hosted_go local_go contracts integration hosted_preview" $T
check "go.mod"                   "go.mod"                        "web hosted_go local_go contracts integration"
check "go.sum"                   "go.sum"                        "web hosted_go local_go contracts integration"
check "workflow"                 ".github/workflows/ci.yml"      "web hosted_go local_go contracts integration"
check "workflow preview on"      ".github/workflows/ci.yml"      "web hosted_go local_go contracts integration hosted_preview" $T
check "unknown root file"        "Makefile"                      "web hosted_go local_go contracts integration"
check "unknown dir"              "scripts/x.sh"                  "web hosted_go local_go contracts integration"
check "unknown under internal"   "internal/other/x.go"           "web hosted_go local_go contracts integration"
check "nested md is not docs"    "web/README.md"                 "web"
check "nested md outside docs"   "api/README.md"                 "hosted_go local_go contracts integration"
check "unknown nested md"        "tools/README.md"               "web hosted_go local_go contracts integration"
check "docs + web"               $'docs/a.md\nweb/x.ts'          "web"
check "web + cli"                $'web/x.ts\ncmd/mealplanner/main.go'      "web local_go contracts integration"
check "docs + unknown"           $'docs/a.md\nfoo.bin'           "web hosted_go local_go contracts integration"
check "empty diff"               ""                              ""
check "CLASSIFY_ALL"             ""                              "web hosted_go local_go contracts integration" CLASSIFY_ALL=true
check "CLASSIFY_ALL preview"     ""                              "web hosted_go local_go contracts integration hosted_preview" CLASSIFY_ALL=true $T

# docs flag and release_metadata
out="$(echo docs/a.md | bash "$here/classify.sh" 2>/dev/null)"
grep -qx 'docs=true' <<<"$out" && echo "ok   docs flag" || { echo "FAIL docs flag"; fail=1; }
out="$(echo web/a.ts | bash "$here/classify.sh" 2>/dev/null)"
grep -qx 'docs=false' <<<"$out" && echo "ok   docs flag off" || { echo "FAIL docs flag off"; fail=1; }

exit "$fail"
