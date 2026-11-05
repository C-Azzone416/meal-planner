#!/usr/bin/env bash
# Classify changed paths into CI components.
#
# Input : changed paths on stdin, one per line (use `git diff --no-renames --name-only`).
# Env   : CLASSIFY_ALL=true          force every component (base commit unknown, diff failed).
#         PREVIEW_TRUSTED=true       event is a push to refs/heads/develop (never a pull request).
#         PREVIEW_ENABLED=true       mirrors vars.PREVIEW_ENABLED.
# Output: `name=true|false` lines on stdout (safe to append to $GITHUB_OUTPUT);
#         per-path reasoning on stderr, prefixed with `#`.
# Path map and rationale: docs/ci-cd-plan.md, "Path map".
set -euo pipefail

docs=false web=false hosted_go=false local_go=false contracts=false
toolchain=false core=false unknown=false cli=false
seen=false

mark_all() { web=true hosted_go=true local_go=true contracts=true; }

if [[ "${CLASSIFY_ALL:-false}" == "true" ]]; then
  seen=true
  toolchain=true
  echo "# CLASSIFY_ALL: base unavailable or diff failed; selecting everything" >&2
fi

while IFS= read -r p || [[ -n "$p" ]]; do
  [[ -z "$p" ]] && continue
  seen=true
  case "$p" in
    docs/*)                    docs=true;       why="docs" ;;
    web/*)                     web=true;        why="web" ;;
    api/*|hosted/*|vercel.json)
                               hosted_go=true; local_go=true; contracts=true
                               why="hosted_go -> hosted_go, local_go (compatibility), contracts, integration" ;;
    cmd/*|internal/localstore/*|internal/mcpadapter/*)
                               local_go=true; cli=true
                               why="cli -> local_go, contracts (compatibility), integration" ;;
    internal/core/*)           core=true;       why="core -> hosted_go, local_go, contracts, integration" ;;
    contracts/*)               contracts=true; mark_all; why="contracts -> everything" ;;
    go.mod|go.sum|.github/*)   toolchain=true;  why="toolchain -> everything" ;;
    *)
      if [[ "$p" != */* && "$p" == *.md ]]; then
        docs=true; why="docs (root markdown)"
      else
        unknown=true; why="unknown path -> everything"
      fi ;;
  esac
  echo "# $p: $why" >&2
done

if [[ "$toolchain" == true || "$unknown" == true ]]; then mark_all; fi
if [[ "$core" == true ]]; then hosted_go=true local_go=true contracts=true; fi

integration=false
if [[ "$hosted_go" == true || "$core" == true || "$contracts" == true || "$toolchain" == true || "$unknown" == true || "$cli" == true ]]; then
  integration=true
fi

hosted_preview=false
if [[ ( "$web" == true || "$hosted_go" == true || "$contracts" == true ) \
   && "${PREVIEW_TRUSTED:-false}" == "true" && "${PREVIEW_ENABLED:-false}" == "true" ]]; then
  hosted_preview=true
fi

# CLI changes run contracts as a compatibility check. Applied after the preview
# decision so a CLI-only change never selects Preview.
if [[ "$cli" == true ]]; then contracts=true; fi

# Reserved for the CLI release workflow; currently mirrors toolchain/unknown changes.
release_metadata=false
if [[ "$toolchain" == true || "$unknown" == true ]]; then release_metadata=true; fi

if [[ "$seen" == false ]]; then echo "# no changed paths; nothing selected" >&2; fi

cat <<OUT
docs=$docs
web=$web
hosted_go=$hosted_go
local_go=$local_go
contracts=$contracts
integration=$integration
hosted_preview=$hosted_preview
release_metadata=$release_metadata
OUT
