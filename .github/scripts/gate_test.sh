#!/usr/bin/env bash
# Tests for gate.sh. Run: bash .github/scripts/gate_test.sh
set -uo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
fail=0

# run NAME EXPECT(0|1) [VAR=VAL ...]  -- starts from an all-unselected, all-skipped baseline
run() {
  local name="$1" want="$2"; shift 2
  env -i PATH="$PATH" AFFECTED_RESULT=success RES_SECRETS=success RES_HYGIENE=success \
    SEL_WEB=false SEL_HOSTED_GO=false SEL_LOCAL_GO=false SEL_CONTRACTS=false SEL_INTEGRATION=false SEL_HOSTED_PREVIEW=false \
    RES_WEB=skipped RES_HOSTED_GO=skipped RES_LOCAL_GO=skipped RES_CONTRACTS=skipped RES_INTEGRATION=skipped RES_PREVIEW=skipped \
    "$@" bash "$here/gate.sh" >/dev/null 2>&1
  local rc=$?
  if [[ $rc -eq $want ]]; then echo "ok   $name"; else echo "FAIL $name: want rc=$want got rc=$rc"; fail=1; fi
}

run "docs only: everything skipped"          0
run "selected job succeeded"                 0 SEL_WEB=true RES_WEB=success
run "selected job failed"                    1 SEL_WEB=true RES_WEB=failure
run "selected job cancelled"                 1 SEL_WEB=true RES_WEB=cancelled
run "selected job unexpectedly skipped"      1 SEL_WEB=true RES_WEB=skipped
run "unselected job ran"                     1 RES_WEB=success
run "unselected job failed"                  1 RES_WEB=failure
run "affected failed"                        1 AFFECTED_RESULT=failure
run "affected cancelled"                     1 AFFECTED_RESULT=cancelled
run "affected skipped"                       1 AFFECTED_RESULT=skipped
run "missing selection output"               1 SEL_LOCAL_GO=
run "garbage selection output"               1 SEL_LOCAL_GO=maybe
run "unknown result"                         1 SEL_WEB=true RES_WEB=weird
run "missing result for selected job"        1 SEL_WEB=true RES_WEB=
run "preview selected and succeeded"         0 SEL_HOSTED_PREVIEW=true RES_PREVIEW=success SEL_WEB=true RES_WEB=success
run "preview selected but skipped"           1 SEL_HOSTED_PREVIEW=true RES_PREVIEW=skipped SEL_WEB=true RES_WEB=success
run "preview selected but failed"            1 SEL_HOSTED_PREVIEW=true RES_PREVIEW=failure SEL_WEB=true RES_WEB=success
run "preview ran though unselected"          1 RES_PREVIEW=success
run "all selected and green"                 0 SEL_WEB=true SEL_HOSTED_GO=true SEL_LOCAL_GO=true SEL_CONTRACTS=true SEL_INTEGRATION=true RES_WEB=success RES_HOSTED_GO=success RES_LOCAL_GO=success RES_CONTRACTS=success RES_INTEGRATION=success
run "secret scan failed"                     1 RES_SECRETS=failure
run "secret scan skipped"                    1 RES_SECRETS=skipped
run "secret scan cancelled"                  1 RES_SECRETS=cancelled
run "secret scan result missing"             1 RES_SECRETS=
run "hygiene failed"                         1 RES_HYGIENE=failure
run "hygiene skipped"                        1 RES_HYGIENE=skipped
run "hygiene result missing"                 1 RES_HYGIENE=
run "scan failure with components green"     1 RES_SECRETS=failure SEL_WEB=true RES_WEB=success

exit "$fail"
