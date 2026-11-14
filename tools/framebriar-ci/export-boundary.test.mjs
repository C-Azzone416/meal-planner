import {test} from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import {spawnSync} from 'node:child_process'

const workflow=fs.readFileSync('.github/workflows/preview.yml','utf8')
const selector=workflow.split('\n  select-source:\n')[1].split('\n  pull:\n')[0]
const pull=workflow.split('\n  pull:\n')[1].split('\n  build:\n')[0]
const build=workflow.split('\n  build:\n')[1].split('\n  deploy:\n')[0]
const deploy=workflow.split('\n  deploy:\n')[1]
const guard=selector.split('      - name: Require supported public Preview dependencies')[1].split('        run: |\n')[1].split('\n').map(line=>line.replace(/^          /,'')).join('\n')
for(const value of ['false','true','','unknown','FALSE','false\ntrue']) {
 test(`actual Preview guard permits only exact public classification: ${JSON.stringify(value)}`,()=>{
  const result=spawnSync('bash',['-c',guard],{env:{...process.env,PRIVATE_DEPENDENCY:value},encoding:'utf8'})
  assert.equal(result.status===0,value==='false',result.stdout+result.stderr)
  if(value!=='false') assert.match(result.stdout,/No direct install\/build fallback/)
 })
}
test('private or malformed manifests stop Preview before token use, build or deployment',()=>{
 assert.match(selector,/ref: \$\{\{ inputs.sha \}\}/)
 assert.match(selector,/ref: \$\{\{ github.workflow_sha \}\}/)
 assert.match(selector,/git -C .target-manifests rev-parse HEAD/)
 assert.match(selector,/set -euo pipefail[\s\S]*classify-dependency.mjs .target-manifests/)
 assert.doesNotMatch(selector,/secrets\.|VERCEL_TOKEN|NODE_AUTH_TOKEN|npm ci|vercel@|continue-on-error/)
 assert.match(pull,/needs: select-source/)
 assert.match(build,/needs: \[select-source, pull\]/)
 for(const job of [pull,build]) assert.match(job,/if: needs.select-source.outputs.private == 'false'/)
 assert.match(deploy,/needs: build/)
 assert.doesNotMatch(workflow,/continue-on-error|PREBUILD_REQUIRED|reviewed-prebuild|artifact-ids:/)
})
test('optional exporter implementation and upload paths are absent',()=>{
 for(const file of ['artifact-tools','seal-prebuild.mjs','prebuild-policy.mjs','prebuild.test.mjs','asset-policy.test.mjs','migration-policy.mjs','migration-policy.test.mjs']) {
  assert.equal(fs.existsSync(path.join('tools/framebriar-ci',file)),false,file)
 }
 // Reconciled feedback adds a fail-closed readiness guard, not a private exporter.
 assert.match(fs.readFileSync('tools/deploy/check-migration-readiness.sh','utf8'), /do not bypass this guard/)
 const reviewed=fs.readFileSync('.github/workflows/framebriar-reviewed-ci.yml','utf8')
 assert.doesNotMatch(reviewed,/prebuild|Vercel|VERCEL_|download-artifact|pull-preview|build-output\.tar/)
 assert.equal((reviewed.match(/uses: actions\/upload-artifact/g)||[]).length,1)
 assert.match(reviewed,/path: \$\{\{ runner.temp \}\}\/framebriar-evidence\/evidence.json/)
 assert.match(reviewed,/needs: validate/)
})
for(const [file,name,value] of [
 ['bridge.mjs','PREBUILD_REQUIRED','1'],
 ['verify.mjs','PREBUILD_REQUESTED','true'],
 ['report.mjs','PREBUILD_REQUESTED','true'],
]) {
 test(`stale exporter caller fails without evidence or output: ${file}`,()=>{
  const root=fs.mkdtempSync(path.join(os.tmpdir(),'no-private-export-'))
  try {
   const result=spawnSync(process.execPath,[`tools/framebriar-ci/${file}`],{env:{PATH:process.env.PATH,[name]:value,EVIDENCE_DIR:root,PREBUILD_METADATA_PATH:path.join(root,'unexpected.json'),GITHUB_OUTPUT:path.join(root,'output')},encoding:'utf8',timeout:5000})
   assert.notEqual(result.status,0)
   assert.match(result.stderr,/Private Preview export is not implemented/)
   assert.deepEqual(fs.readdirSync(root),[])
  } finally {fs.rmSync(root,{recursive:true,force:true})}
 })
}
