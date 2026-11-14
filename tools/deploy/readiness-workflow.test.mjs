import {test} from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
const w=fs.readFileSync('.github/workflows/framebriar-reviewed-ci.yml','utf8')
const job=name=>w.split(`\n  ${name}:\n`)[1].split(/\n  [a-z][a-z-]*:\n/)[0]
test('fresh runtime inspection waits for build, exposes no DB credential',()=>{
 const r=job('runtime-check')
 assert.match(r,/needs: \[validate, backend, frontend\]/)
 assert.match(r,/environment: preview/)
 assert.equal((r.match(/secrets\./g)||[]).length,1)
 assert.match(r,/trap .*find .vercel/)
 assert.match(r,/readiness-runtime-config.mjs .vercel\/.env.preview.local/)
 assert.ok(r.indexOf('secrets.VERCEL_TOKEN')<r.indexOf('uses: actions\/checkout'))
})
test('clean catalog job has no target execution, Vercel token or runtime role secret',()=>{
 const r=job('readiness')
 assert.match(r,/needs: \[validate, backend, frontend, runtime-check\]/)
 assert.match(r,/environment: framebriar-ci/)
 assert.equal((r.match(/secrets\./g)||[]).length,1)
 assert.match(r,/secrets.FRAMEBRIAR_PREVIEW_CATALOG_DATABASE_URL/)
 assert.doesNotMatch(r,/VERCEL_TOKEN|FRAMEBRIAR_DATABASE_URL|npm ci|npm run|go test|psql.* -f.*source-migrations/)
 assert.match(r,/node trusted\/tools\/deploy\/readiness-adapter.mjs inspect source-migrations/)
})
test('clean deploy verifies authenticated inspector record, never builder assertion',()=>{
 const r=job('deploy')
 for(const name of ['record','record_sha256','provider_map'])assert.ok(r.includes(`needs.readiness.outputs.${name}`))
 assert.match(r,/needs.runtime-check.outputs.runtime_digest/)
 assert.ok(r.indexOf('readiness-adapter.mjs consume')<r.indexOf('secrets.VERCEL_TOKEN'))
 assert.doesNotMatch(r,/FRAMEBRIAR_PREVIEW_CATALOG_DATABASE_URL/)
})
