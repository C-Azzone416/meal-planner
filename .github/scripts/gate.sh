#!/usr/bin/env bash
# Fail-closed evaluation for the `CI / required` job.
#
# Env: AFFECTED_RESULT                       result of the `affected` job
#      RES_SECRETS, RES_HYGIENE              results of the always-on scan jobs; must be success
#      SEL_<NAME>                            classifier output (true|false) for each gated job
#      RES_<NAME>                            result of that job (success|failure|cancelled|skipped)
#      SEL_DOCS, SEL_RELEASE_METADATA        informational only
#      NAMES (optional)                      space-separated gated job names, default below
# Stdout: markdown summary (append to $GITHUB_STEP_SUMMARY). Stderr: ::error:: lines.
# Exit 1 if any rule fails. Rules: docs/ci-cd-plan.md, "Stable required check".
set -uo pipefail

NAMES="${NAMES:-web hosted_go local_go contracts integration preview}"
failed=0
rows=""
evaluated=0

fail() { echo "::error::$*" >&2; failed=1; }

upper() { tr '[:lower:]-' '[:upper:]_' <<<"$1"; }

if [[ "${AFFECTED_RESULT:-}" != "success" ]]; then
  fail "affected job result is '${AFFECTED_RESULT:-<unset>}', expected success"
fi

# The scans always run, never depend on classification, and must pass.
SCANS="${SCANS:-secrets hygiene}"
for n in $SCANS; do
  u="$(upper "$n")"
  res_var="RES_$u"
  res="${!res_var:-}"
  verdict="ok"
  evaluated=$((evaluated + 1))
  if [[ "$res" != "success" ]]; then
    verdict="FAIL (result '${res:-<unset>}')"
    fail "$n scan result is '${res:-<unset>}', expected success"
  fi
  rows+="| $n | always | ${res:-—} | $verdict |"$'\n'
done

for n in $NAMES; do
  u="$(upper "$n")"
  # preview's selection output is hosted_preview
  if [[ "$n" == "preview" ]]; then sel_var="SEL_HOSTED_PREVIEW"; else sel_var="SEL_$u"; fi
  sel="${!sel_var:-}"
  res_var="RES_$u"
  res="${!res_var:-}"
  verdict="ok"
  evaluated=$((evaluated + 1))
  case "$sel" in
    true)
      if [[ "$res" != "success" ]]; then
        verdict="FAIL (selected, result '${res:-<unset>}')"
        fail "$n was selected but its result is '${res:-<unset>}'"
      fi ;;
    false)
      if [[ "$res" != "skipped" ]]; then
        verdict="FAIL (not selected, result '${res:-<unset>}')"
        fail "$n was not selected but its result is '${res:-<unset>}', expected skipped"
      else
        verdict="ok (intentionally skipped: not selected)"
      fi ;;
    *)
      verdict="FAIL (selection output '${sel:-<missing>}')"
      fail "classifier output for $n is '${sel:-<missing>}', expected true or false" ;;
  esac
  rows+="| $n | ${sel:-—} | ${res:-—} | $verdict |"$'\n'
done

# A gate that evaluated nothing must never pass.
if [[ $evaluated -eq 0 ]]; then fail "no jobs were evaluated"; fi

{
  echo "## CI required gate"
  echo
  echo "affected: \`${AFFECTED_RESULT:-<unset>}\`; docs=\`${SEL_DOCS:-—}\` release_metadata=\`${SEL_RELEASE_METADATA:-—}\`"
  echo
  echo "| Job | Selected | Result | Verdict |"
  echo "| --- | --- | --- | --- |"
  printf '%s' "$rows"
  echo
  if [[ $failed -eq 0 ]]; then echo "**Gate: PASS**"; else echo "**Gate: FAIL**"; fi
}
exit "$failed"
