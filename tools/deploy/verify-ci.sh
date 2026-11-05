#!/usr/bin/env bash
# Verify that branch CI succeeded for one exact commit on main.
# Usage: verify-ci.sh <full-sha>
# Env:   GITHUB_REPOSITORY (owner/name), GH_TOKEN (needs actions: read)
#
# Rule (fails closed on anything else): the newest ci.yml run for this SHA from a push to main must be
# completed, and its job named exactly `required` (displayed as "CI / required") must have concluded success.
# Also used by the CLI release workflow (release-cli.yml).
set -euo pipefail

sha=${1:?usage: verify-ci.sh <full-sha>}
repo=${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}
[[ "$sha" =~ ^[0-9a-f]{40}$ ]] || { echo "::error::sha must be a full 40-character lowercase hex SHA" >&2; exit 1; }

runs=$(gh api -X GET "repos/$repo/actions/workflows/ci.yml/runs" \
  -f head_sha="$sha" -f event=push -f branch=main -f per_page=100)

# Newest matching run; the filters are re-checked locally rather than trusted.
run=$(jq -c --arg sha "$sha" '
  [.workflow_runs[] | select(.head_sha == $sha and .event == "push" and .head_branch == "main")]
  | sort_by(.created_at) | last // empty' <<< "$runs")
[[ -n "$run" ]] || { echo "::error::no ci.yml push run on main found for $sha" >&2; exit 1; }

id=$(jq -r '.id' <<< "$run")
status=$(jq -r '.status' <<< "$run")
[[ "$status" == completed ]] || { echo "::error::ci.yml run $id is $status, not completed" >&2; exit 1; }

jobs=$(gh api -X GET "repos/$repo/actions/runs/$id/jobs" -f filter=latest -f per_page=100)
required=$(jq -c '[.jobs[] | select(.name == "required")]' <<< "$jobs")
[[ "$(jq 'length' <<< "$required")" == 1 ]] || { echo "::error::expected exactly one job named 'required' in run $id" >&2; exit 1; }
jq -e '.[0].conclusion == "success"' <<< "$required" > /dev/null \
  || { echo "::error::job 'required' in run $id concluded $(jq -r '.[0].conclusion' <<< "$required"), not success" >&2; exit 1; }

echo "CI / required succeeded: run $id (https://github.com/$repo/actions/runs/$id) for $sha"
