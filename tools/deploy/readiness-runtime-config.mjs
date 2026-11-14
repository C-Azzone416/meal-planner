// Reads only fixed public config and write-only secret placeholders. Never logs input.
import assert from 'node:assert/strict'
import {createHash} from 'node:crypto'
export const expectedRuntime=Object.freeze({FRAMEBRIAR_ENABLED:'1',FRAMEBRIAR_ENVIRONMENT:'preview',FRAMEBRIAR_ALLOWED_ORIGIN:'https://athome-meals-dev.vercel.app',FRAMEBRIAR_APP_ID:'meal-planner-preview',FRAMEBRIAR_SCREENSHOTS:'1',FRAMEBRIAR_DATABASE_URL:'[SENSITIVE]',FRAMEBRIAR_CREDENTIAL_KEYS:'[SENSITIVE]',MEAL_RECIPES_DATABASE_URL:'[SENSITIVE]'})
export const runtimeDigest=createHash('sha256').update(JSON.stringify(expectedRuntime)).digest('hex')
export function inspectRuntimeConfig(raw){
 assert.equal(typeof raw,'string');assert.ok(Buffer.byteLength(raw)<=1024*1024)
 const selected={};const all=new Set()
 for(const line of raw.split('\n')){
  if(!line.trim()||line.trimStart().startsWith('#'))continue
  const match=/^([A-Za-z_][A-Za-z0-9_]*)=(.*)$/.exec(line)
  assert.ok(match,'Unsupported provider dotenv format')
  const [,name,value]=match;assert.ok(!all.has(name),'Duplicate setting');all.add(name)
  // Only known public values are decoded. Unknown/private values are never copied.
  if(Object.hasOwn(expectedRuntime,name)){
   assert.ok(value.startsWith('"')&&value.endsWith('"'))
   selected[name]=JSON.parse(value)
  }
 }
 assert.deepEqual(selected,expectedRuntime)
 assert.ok(!all.has('FRAMEBRIAR_BUILD_SHA'),'Legacy runtime SHA must be removed, not reused')
 // Existing guard rejects all draft settings; absence guarantees this selected disabled mode.
 assert.ok(![...all].some(name=>name.startsWith('MEAL_DRAFT_INGEST_')))
 return runtimeDigest
}
if(process.argv[1]&&process.argv[1].endsWith('/readiness-runtime-config.mjs')){
 try{
  const fs=await import('node:fs');const path=await import('node:path');assert.equal(process.argv.length,3)
  const digest=inspectRuntimeConfig(fs.readFileSync(process.argv[2],'utf8'))
  const metadata=fs.readFileSync(path.join(path.dirname(process.argv[2]),'project.json'),'utf8')
  assert.ok(Buffer.byteLength(metadata)<=1024*1024)
  const project=JSON.parse(metadata)
  assert.match(project.projectId??'',/^prj_[A-Za-z0-9]+$/)
  assert.equal(project.projectId,process.env.VERCEL_PROJECT_ID);assert.equal(project.orgId,process.env.VERCEL_ORG_ID)
  assert.ok(process.env.GITHUB_OUTPUT)
  fs.appendFileSync(process.env.GITHUB_OUTPUT,`runtime_digest=${digest}\nobserved_at_ms=${Date.now()}\nruntime_project_id=${project.projectId}\n`)
 }catch{console.error('Preview runtime configuration is not ready; values withheld.');process.exitCode=1}
}
