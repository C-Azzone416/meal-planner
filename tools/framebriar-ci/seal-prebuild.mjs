// Validate prebuilt bytes on a clean runner; never execute target artifact code.
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import assert from 'node:assert/strict'
import {execFileSync} from 'node:child_process'
import {fileURLToPath} from 'node:url'
import {sha256} from './policy.mjs'
const tools=fileURLToPath(new URL('../',import.meta.url))
try {
  const directory=process.env.PREBUILD_DIRECTORY
  const entries=fs.readdirSync(directory)
  assert.deepEqual(entries,['build-output.tar'])
  const archive=path.join(directory,entries[0]),stat=fs.lstatSync(archive)
  assert.ok(stat.isFile()&&!stat.isSymbolicLink()&&stat.size<=268435456)
  const checksum=sha256(fs.readFileSync(archive))
  if(process.env.EXPECTED_PREBUILD_JSON) {
    const expected=JSON.parse(fs.readFileSync(process.env.EXPECTED_PREBUILD_JSON,'utf8'))
    assert.equal(expected.tar_sha256,checksum,'Prebuild checksum mismatch')
    assert.equal(expected.source_sha,process.env.TARGET_SHA)
    assert.equal(expected.target,'preview')
    assert.equal(expected.enabled,false);assert.equal(expected.screenshots,false)
  }
  assert.match(process.env.TARGET_SHA??'',/^[a-f0-9]{40}$/)
  const unpack=fs.mkdtempSync(path.join(os.tmpdir(),'framebriar-prebuilt-'))
  try {
    execFileSync('bash',[path.join(tools,'deploy/safe-extract.sh'),archive,unpack],{env:{...process.env,SAFE_EXTRACT_ALLOW_RE:'^\\.vercel/(output(/|$)|project\\.json$)'},stdio:'pipe'})
    execFileSync('bash',[path.join(tools,'deploy/materialize-output.sh'),'--check',unpack],{stdio:'pipe'})
    const project=JSON.parse(fs.readFileSync(path.join(unpack,'.vercel/project.json'),'utf8'))
    assert.ok(process.env.VERCEL_PROJECT_ID&&process.env.VERCEL_ORG_ID)
    assert.equal(project.projectId,process.env.VERCEL_PROJECT_ID);assert.equal(project.orgId,process.env.VERCEL_ORG_ID)
    function inspect(dir) {
      for(const item of fs.readdirSync(dir,{withFileTypes:true})) {
        assert.ok(!item.isSymbolicLink())
        assert.ok(!/^\.env|node_modules|\.npmrc$|\.map$|package-lock\.json$/.test(item.name),'Raw dependencies/config/source maps must not be exported')
        if(item.isDirectory())inspect(path.join(dir,item.name))
      }
    }
    inspect(path.join(unpack,'.vercel'))
    const env={...process.env,FRAMEBRIAR_BUILD_SHA:process.env.TARGET_SHA,FRAMEBRIAR_ENVIRONMENT:'preview',FRAMEBRIAR_APP_ID:'meal-planner-preview',FRAMEBRIAR_ENABLED:'0',FRAMEBRIAR_SCREENSHOTS:'0',FRAMEBRIAR_ALLOWED_ORIGIN:''}
    execFileSync(process.execPath,[path.join(tools,'framebriar-ci/artifact-tools/check-artifact.mjs'),path.join(unpack,'.vercel/output/static')],{env,stdio:'pipe'})
    execFileSync(process.execPath,[path.join(tools,'framebriar-ci/artifact-tools/check-server-artifact.mjs'),path.join(unpack,'.vercel/output/functions'),process.env.TARGET_SHA,'preview'],{stdio:'pipe'})
  } finally {fs.rmSync(unpack,{recursive:true,force:true})}
  fs.writeFileSync(process.env.SEALED_PREBUILD_JSON,JSON.stringify({source_sha:process.env.TARGET_SHA,target:'preview',enabled:false,screenshots:false,tar_sha256:checksum})+'\n')
} catch {
  console.error('::error::Prebuild rejected: checksum, project, target, SHA, archive boundary, raw dependency or provenance check failed')
  process.exitCode=1
}
