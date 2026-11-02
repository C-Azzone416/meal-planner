#!/usr/bin/env bash
# Fail closed unless the CI workflow succeeded for a commit pushed to main.
#
# Usage: tools/release/verify-ci.sh <sha>
# Env:   GITHUB_REPOSITORY (owner/repo) and GH_TOKEN (needs actions: read).
#
# "CI / required" is only the UI display name (workflow / job). The check run
# name is just the job name, so we look at runs of ci.yml for this SHA on
# main (push events only) and require a job named exactly "required" with
# conclusion "success".
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <sha>" >&2
  exit 2
fi
sha="$1"
repo="${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"

run_ids="$(gh api --paginate \
  "repos/${repo}/actions/workflows/ci.yml/runs?head_sha=${sha}&event=push&branch=main&per_page=100" \
  --jq '.workflow_runs[].id')"
if [ -z "$run_ids" ]; then
  echo "error: no ci.yml push runs on main for $sha" >&2
  exit 1
fi

for id in $run_ids; do
  ok="$(gh api --paginate "repos/${repo}/actions/runs/${id}/jobs?per_page=100" \
    --jq '[.jobs[] | select(.name == "required" and .conclusion == "success")] | length' \
    | awk '{s+=$1} END {print s+0}')"
  if [ "$ok" -ge 1 ]; then
    echo "ok: ci.yml run $id has a successful 'required' job for $sha"
    exit 0
  fi
  echo "run $id: no successful 'required' job" >&2
done
echo "error: no successful 'required' job in any ci.yml run for $sha" >&2
exit 1
