#!/usr/bin/env bash
# Local evidence only. No pull/login/deploy, credentials, private SDK or real app data.
# Usage: bash .../generate-synthetic-cli-fixture.sh /absolute/path/to/vercel/dist/vc.js
set -euo pipefail
umask 077
cli=${1:?Pass the already-installed Vercel 65.9.0 dist/vc.js path}
[[ "$cli" = /* && -f "$cli" && ! -L "$cli" ]] || { echo 'Expected an absolute regular CLI entry point' >&2; exit 2; }
node - "$cli" <<'JS'
const fs = require('node:fs'), path = require('node:path');
const entry = process.argv[2];
const pkg = JSON.parse(fs.readFileSync(path.join(path.dirname(entry), '..', 'package.json'), 'utf8'));
if (pkg.name !== 'vercel' || pkg.version !== '65.9.0') throw Error('Expected installed vercel@65.9.0');
JS
repo=$(cd "$(dirname "$0")/../.." && pwd -P)
root=$(mktemp -d "${TMPDIR:-/tmp}/framebriar-synthetic-cli.XXXXXX")
root=$(cd "$root" && pwd -P)
mkdir -p "$root/home" "$root/builder-tmp" "$root/project/.vercel" "$root/project/api"
cat > "$root/project/package.json" <<'JSON'
{"name":"synthetic-feedback-boundary","version":"1.0.0","private":true}
JSON
cat > "$root/project/build.mjs" <<'JS'
import fs from 'node:fs';
fs.mkdirSync('web/dist/assets', {recursive:true});
fs.writeFileSync('web/dist/index.html', '<!doctype html><h1>Synthetic fixture only</h1>');
fs.writeFileSync('web/dist/assets/app-12345678.js', 'console.log("synthetic fixture");');
JS
node - "$repo/vercel.json" "$root/project/vercel.json" <<'JS'
const fs = require('node:fs');
const input = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
if (!Array.isArray(input.rewrites)) throw Error('Expected reviewed public route rules');
fs.writeFileSync(process.argv[3], JSON.stringify({installCommand:'echo synthetic-no-dependencies',buildCommand:'node build.mjs',outputDirectory:'web/dist',rewrites:input.rewrites},null,2));
JS
cat > "$root/project/.vercel/project.json" <<'JSON'
{"projectId":"prj_syntheticFixtureOnly","orgId":"team_syntheticFixtureOnly","settings":{"framework":null,"devCommand":null,"installCommand":"echo synthetic-no-dependencies","buildCommand":"node build.mjs","outputDirectory":"web/dist","rootDirectory":null,"nodeVersion":"24.x"}}
JSON
printf 'VERCEL_ENV=preview\n' > "$root/project/.vercel/.env.preview.local"
cat > "$root/project/go.mod" <<'GO'
module example.invalid/synthetic-framebriar-fixture

go 1.26
GO
cat > "$root/project/api/health.go" <<'GO'
package handler
import "net/http"
func Handler(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Type", "application/json"); w.Write([]byte(`{"status":"synthetic"}`)) }
GO
# An empty HOME/XDG prevents credential discovery; env -i drops inherited tokens.
# Build tooling may fetch official Go/builder dependencies. No --offline flag exists.
(cd "$root/project" && env -i PATH="$PATH" HOME="$root/home" XDG_CONFIG_HOME="$root/home" TMPDIR="$root/builder-tmp" CI=1 DO_NOT_TRACK=1 VERCEL_TELEMETRY_DISABLED=1 GOMAXPROCS=2 node "$cli" build --target=preview --yes) > "$root/build.log" 2>&1
bash "$repo/tools/deploy/materialize-output.sh" "$root/project"
printf 'Synthetic local output: %s\n' "$root/project/.vercel/output"
printf 'Review local build log: %s\n' "$root/build.log"
printf '%s\n' 'NOT deployment approval. Do not export diagnostics/builds.json or treat CLI pin as a whole-toolchain pin.'
