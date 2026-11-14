import {test} from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
const w=fs.readFileSync('.github/workflows/framebriar-reviewed-ci.yml','utf8')
const job=name=>w.split(`\n  ${name}:\n`)[1].split(/\n  [a-z]+:\n/)[0]
test('Preview is explicit and develop-only, ordinary PR pipeline unchanged',()=>{
 assert.match(w,/preview:[\s\S]*default: false/)
 for(const name of ['pull','deploy'])assert.match(job(name),/if: inputs.preview && inputs.subject == 'develop'/)
 assert.match(job('validate'),/PREVIEW_REQUESTED.*true.*SUBJECT.*develop/)
 assert.doesNotMatch(w,/pull_request_target/)
})
test('build has install-only reader and no Vercel or runtime credentials',()=>{
 const f=job('frontend')
 assert.equal((f.match(/secrets\./g)||[]).length,1)
 assert.match(f,/secrets.FRAMEBRIAR_PACKAGE_READ_TOKEN/)
 assert.doesNotMatch(f,/VERCEL_TOKEN|DATABASE_URL|COOKIE_SECRET|ENCRYPTION_KEY/)
 assert.match(f,/npm ci --ignore-scripts/)
 assert.match(f,/go-version: '1.26.11'/)
 assert.match(f,/govulncheck@v1.9.0 -mode=binary/)
})
test('deploy consumes immutable same-run artifact and checks before token',()=>{
 const d=job('deploy')
 assert.match(d,/needs: \[validate, backend, frontend, evidence, pull, runtime-check, readiness\]/)
 assert.match(d,/artifact-ids: \$\{\{ needs.frontend.outputs.artifact_id \}\}/)
 assert.match(d,/ref: \$\{\{ github.workflow_sha \}\}/)
 assert.doesNotMatch(d,/ref: \$\{\{ inputs/)
 assert.match(d,/Read exact source migration bytes without execution[\s\S]*sparse-checkout: \|[\s\S]*hosted\/migrations[\s\S]*path: source-migrations/)
 assert.ok(d.indexOf('Revalidate compiled inventory')<d.indexOf('secrets.VERCEL_TOKEN'))
 assert.ok(d.indexOf('Revalidate approved exact develop immediately')<d.indexOf('secrets.VERCEL_TOKEN'))
 assert.match(d,/deploy --prebuilt --target=preview/)
 assert.doesNotMatch(d,/--prod|target=production/)
})

import {spawnSync} from 'node:child_process'
test('actual subject restriction executes fail closed',()=>{
 const text=job('validate').split('      - name: Restrict deployment to exact develop subjects')[1].split('      - name: Verify exact source')[0]
 const script=text.split('        run: |\n')[1].split('\n').map(x=>x.replace(/^          /,'')).join('\n')
 for(const [preview,subject,passes] of [['true','develop',true],['true','pr',false],['true','',false],['false','pr',true]]){
  const r=spawnSync('bash',['-e','-c',script],{env:{PATH:process.env.PATH,PREVIEW_REQUESTED:preview,SUBJECT:subject},encoding:'utf8'})
  assert.equal(r.status===0,passes,`${preview}/${subject}: ${r.stderr}`)
 }
})
test('protected build explicitly prepares provenance before Vercel build',()=>{
 const f=job('frontend')
 assert.ok(f.indexOf('node tools/feedback/prepare-build.mjs')<f.indexOf('build --target=preview --yes'))
 assert.match(f,/Isolated registry install only[\s\S]*npm ci --ignore-scripts/)
})

test('protected backend includes every affected security-tool check',()=>{
 const ci=fs.readFileSync('.github/workflows/ci.yml','utf8')
 const checks=ci.split('      - name: Test classifier, gate and deploy scripts')[1].split('      - name: Classify changed paths')[0]
 const commands=checks.split('\n').map(x=>x.trim()).filter(x=>x.startsWith('bash ')||x.startsWith('node --test '))
 for(const command of commands)assert.ok(job('backend').includes(command),`Missing required tool check: ${command}`)
 assert.match(job('deploy'),/rechecked-output\/manifest.json/)
})
