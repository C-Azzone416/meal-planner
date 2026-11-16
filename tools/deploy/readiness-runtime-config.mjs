// Reads only fixed public config and write-only secret placeholders. Never logs input.
import assert from 'node:assert/strict'
import {createHash} from 'node:crypto'
export const expectedRuntime=Object.freeze({FRAMEBRIAR_ENABLED:'1',FRAMEBRIAR_ENVIRONMENT:'preview',FRAMEBRIAR_ALLOWED_ORIGIN:'https://athome-meals-dev.vercel.app',FRAMEBRIAR_APP_ID:'meal-planner-preview',FRAMEBRIAR_SCREENSHOTS:'1',FRAMEBRIAR_DATABASE_URL:'[SENSITIVE]',FRAMEBRIAR_CREDENTIAL_KEYS:'[SENSITIVE]',MEAL_RECIPES_DATABASE_URL:'[SENSITIVE]'})
export const runtimeDigest=createHash('sha256').update(JSON.stringify(expectedRuntime)).digest('hex')
export const expectedSyntheticDraftRuntime=Object.freeze({...expectedRuntime,MEAL_DRAFTS_DATABASE_URL:'[SENSITIVE]',MEAL_DRAFT_PAIRING_ENABLED:'true',MEAL_DRAFT_WORKFLOW_ENABLED:'true',MEAL_DRAFT_PAIRING_ORIGIN:'https://athome-meals-dev.vercel.app',MEAL_DRAFT_INGEST_ORIGIN:'https://athome-meals-dev.vercel.app',MEAL_DRAFT_MEMBERSHIP_EPOCH:'[SENSITIVE]'})
export const syntheticDraftRuntimeDigest=createHash('sha256').update(JSON.stringify(expectedSyntheticDraftRuntime)).digest('hex')
export function runtimePolicy(draft=false){
 assert.equal(typeof draft,'boolean')
 return draft?{expected:expectedSyntheticDraftRuntime,digest:syntheticDraftRuntimeDigest}:{expected:expectedRuntime,digest:runtimeDigest}
}
export function inspectRuntimeConfig(raw,{draft=false}={}){
 assert.equal(typeof raw,'string');assert.ok(Buffer.byteLength(raw)<=1024*1024)
 const {expected,digest}=runtimePolicy(draft)
 const selected={};const all=new Set()
 for(const line of raw.split('\n')){
  if(!line.trim()||line.trimStart().startsWith('#'))continue
  const match=/^([A-Za-z_][A-Za-z0-9_]*)=(.*)$/.exec(line)
  assert.ok(match,'Unsupported provider dotenv format')
  const [,name,value]=match;assert.ok(!all.has(name),'Duplicate setting');all.add(name)
  // Only known public values are decoded. Unknown/private values are never copied.
  if(Object.hasOwn(expected,name)){
   assert.ok(value.startsWith('"')&&value.endsWith('"'))
   selected[name]=JSON.parse(value)
  }
 }
 assert.deepEqual(selected,expected)
 assert.ok(!all.has('FRAMEBRIAR_BUILD_SHA'),'Legacy runtime SHA must be removed, not reused')
 assert.ok(!all.has('MEAL_DRAFT_INGEST_ENABLED'),'Legacy ingest must stay disabled')
 // MEAL_DRAFTS_DATABASE_URL is plural and therefore outside MEAL_DRAFT_*.
 // Check both forms so closed mode cannot silently accept the draft credential.
 const draftNames=[...all].filter(name=>name==='MEAL_DRAFTS_DATABASE_URL'||name.startsWith('MEAL_DRAFT_'))
 assert.deepEqual(draftNames.toSorted(),draft?Object.keys(expectedSyntheticDraftRuntime).filter(name=>name==='MEAL_DRAFTS_DATABASE_URL'||name.startsWith('MEAL_DRAFT_')).toSorted():[])
 return digest
}
if(process.argv[1]&&process.argv[1].endsWith('/readiness-runtime-config.mjs')){
 try{
  const fs=await import('node:fs');const path=await import('node:path')
  const args=process.argv.slice(2);const draft=args[0]==='--synthetic-draft';const file=draft?args[1]:args[0]
  assert.equal(args.length,draft?2:1)
  const digest=inspectRuntimeConfig(fs.readFileSync(file,'utf8'),{draft})
  const metadata=fs.readFileSync(path.join(path.dirname(file),'project.json'),'utf8')
  assert.ok(Buffer.byteLength(metadata)<=1024*1024)
  const project=JSON.parse(metadata)
  assert.match(project.projectId??'',/^prj_[A-Za-z0-9]+$/)
  assert.equal(project.projectId,process.env.VERCEL_PROJECT_ID);assert.equal(project.orgId,process.env.VERCEL_ORG_ID)
  assert.ok(process.env.GITHUB_OUTPUT)
  fs.appendFileSync(process.env.GITHUB_OUTPUT,`runtime_digest=${digest}\nobserved_at_ms=${Date.now()}\nruntime_project_id=${project.projectId}\n`)
 }catch{console.error('Preview runtime configuration is not ready; values withheld.');process.exitCode=1}
}
