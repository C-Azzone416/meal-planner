#!/usr/bin/env bash
# Fixture tests for the fail-closed Vercel deployment evidence checks.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
filter="$here/vercel-evidence.jq"
sha=0123456789abcdef0123456789abcdef01234567
pid=prj_test
prod='["athome-meals-ai.vercel.app"]'
pass=0
failn=0

check() { # check <description> <expected-pass:true|false> <target> <deployment-json> [domains-json] [inventory-complete]
  local desc=$1 want=$2 target=$3 dep=$4 domains=${5:-$prod} complete=${6:-true}
  local evidence rc=0 got=false
  evidence=$(jq --argjson prod_names "$domains" --argjson inventory_complete "$complete" \
    --arg expect "$target" --arg sha "$sha" --arg pid "$pid" \
    -f "$filter" <<< "$dep") || rc=$?
  if [[ $rc -eq 0 ]] && jq -e '.checks | all' <<< "$evidence" > /dev/null; then got=true; fi
  if [[ "$got" == "$want" ]]; then
    echo "ok   $desc"
    pass=$((pass + 1))
  else
    echo "FAIL $desc (expected pass=$want, got pass=$got)"
    [[ -z "$evidence" ]] || jq -c '.checks' <<< "$evidence"
    failn=$((failn + 1))
  fi
}

base=$(jq -n --arg sha "$sha" --arg pid "$pid" '{
  id: "dpl_test", url: "example.vercel.app", projectId: $pid,
  target: "production", readyState: "READY", aliasAssigned: true,
  alias: [], meta: {sourceSha: $sha}
}')

check "skip-domain accepts aliasAssigned=true with an empty alias list" true production "$base"
check "skip-domain accepts aliasAssigned=false with an empty alias list" true production "$(jq '.aliasAssigned=false' <<< "$base")"
check "a known production alias fails" false production "$(jq '.alias=["athome-meals-ai.vercel.app"]' <<< "$base")"
check "any other alias also fails a staged production deployment" false production "$(jq '.alias=["unexpected.vercel.app"]' <<< "$base")"
check "missing alias normalizes to an empty list" true production "$(jq 'del(.alias)' <<< "$base")"
check "an unknown production-domain inventory fails closed" false production "$base" '[]'
check "a nonempty fallback name cannot replace a complete domains inventory" false production "$base" "$prod" false
check "project mismatch fails" false production "$(jq '.projectId="prj_other"' <<< "$base")"
check "source SHA mismatch fails" false production "$(jq '.meta.sourceSha="ffffffffffffffffffffffffffffffffffffffff"' <<< "$base")"
check "target mismatch fails" false production "$(jq '.target="preview"' <<< "$base")"
check "non-ready deployment fails" false production "$(jq '.readyState="BUILDING"' <<< "$base")"
check "preview accepts no production alias" true preview "$(jq '.target="preview" | .alias=["athome-meals-dev.vercel.app"]' <<< "$base")"
check "preview rejects a production alias" false preview "$(jq '.target="preview" | .alias=["athome-meals-ai.vercel.app"]' <<< "$base")"

echo "$pass passed, $failn failed"
[[ $failn -eq 0 ]]
