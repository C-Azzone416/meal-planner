#!/usr/bin/env bash
# Deliberately separate shell/step from the authenticated npm ci --ignore-scripts.
set -euo pipefail
if [[ -n ${NODE_AUTH_TOKEN:-}${FRAMEBRIAR_PACKAGE_READ_TOKEN:-}${NPM_TOKEN:-}${VERCEL_TOKEN:-} ]]; then
  echo '::error::postinstall must run without package or deployment credentials' >&2
  exit 1
fi
[[ -d web/node_modules/@masra91/framebriar ]] || { echo '::error::Run the documented isolated npm ci --ignore-scripts install first; private package access is required' >&2; exit 1; }
# Current lockfile only declares optional macOS fsevents, which is not needed by
# CI builds. SDK dependencies must be reviewed on every lockfile update. Never
# blanket npm rebuild: doing so would run arbitrary lifecycle scripts.
node --input-type=module <<'JS'
import fs from 'node:fs'
const lock = JSON.parse(fs.readFileSync('web/package-lock.json', 'utf8'))
for (const [name, pkg] of Object.entries(lock.packages)) {
  if (pkg.hasInstallScript && !(name.endsWith('/fsevents') && pkg.version === '2.3.3')) {
    console.error('Unreviewed lifecycle package:', name)
    process.exitCode = 1
  }
}
JS
