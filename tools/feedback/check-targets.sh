#!/usr/bin/env bash
set -euo pipefail
export FRAMEBRIAR_BUILD_SHA=${FRAMEBRIAR_BUILD_SHA:-$(git rev-parse HEAD)}
export FRAMEBRIAR_SCREENSHOTS=0
# Credential and fake-data sentinels must not be copied by Vite.
export FRAMEBRIAR_CREDENTIAL_KEYS=FRAMEBRIAR_TEST_SECRET
export VITE_SECRET=FRAMEBRIAR_TEST_SECRET
for target in preview production; do
  export FRAMEBRIAR_ENVIRONMENT=$target
  export FRAMEBRIAR_APP_ID=meal-planner
  [[ $target != preview ]] || export FRAMEBRIAR_APP_ID=meal-planner-preview
  export FRAMEBRIAR_ALLOWED_ORIGIN="https://$target.example.test"
  for capture in 0 1; do
    export FRAMEBRIAR_SCREENSHOTS=$capture
    for enabled in 0 1; do
    export FRAMEBRIAR_ENABLED=$enabled
    npm run --prefix web build
    node tools/feedback/check-artifact.mjs web/dist
    done
  done
done
