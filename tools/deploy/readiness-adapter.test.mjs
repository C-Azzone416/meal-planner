import {test} from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import {inventory} from './migration-readiness.mjs'
import {spawnSync} from 'node:child_process'
import {planFor,inspect,consume,diagnostic,psqlFailureStage} from './readiness-adapter.mjs'
import {runtimeDigest} from './readiness-runtime-config.mjs'
const baseline=JSON.parse(fs.readFileSync(new URL('./readiness-baseline-pg18.json',import.meta.url),'utf8'))
const map=()=>({version:1,target:'preview',vercelProjectId:'prj_Synthetic',neonProjectId:'synthetic-project',neonBranchId:'br-synthetic',endpointHost:'ep-synthetic.us-east-1.aws.neon.tech',databaseName:'testdb',inspectorRole:'catalog_inspector',recipeRuntimeRole:'recipe_runtime',feedbackRuntimeRole:'feedback_runtime',postgresMajor:18,feedback:true,draft:false})
const secret='postgresql://catalog_inspector:FAKE_SECRET_NOT_FOR_LOGS@ep-synthetic.us-east-1.aws.neon.tech/testdb?sslmode=verify-full'
const input=()=>({map:map(),baseline,sourceInventory:baseline.inventory,sourceSha:'a'.repeat(40),secret,runtimeEvidence:{digest:runtimeDigest,observedAtMs:10000,projectId:'prj_Synthetic'},now:10000})
const execute=(connection,args)=>{
 assert.equal(connection.PGPASSWORD,'FAKE_SECRET_NOT_FOR_LOGS')
 assert.ok(!args.some(x=>x.includes('FAKE_SECRET')))
 if(args[0]==='-c')return '180001'
 const component=args.find(x=>x.startsWith('component=')).split('=')[1]
 const role=args.find(x=>x.startsWith('runtime_role=')).split('=')[1]
 const snapshot=structuredClone(role==='catalog_inspector'?baseline.inspector:baseline.components[component]);snapshot.databaseName='testdb';snapshot.runtimeRole=role
 return JSON.stringify(snapshot)
}
test('synthetic clean observer to authenticated digest to consumer',()=>{
 const i=input(),r=inspect(i,execute)
 assert.ok(!r.record.includes('FAKE_SECRET'));assert.equal(r.sha256.length,64)
 assert.equal(consume({...i,record:r.record,expectedDigest:r.sha256,now:11000}).sourceSha,i.sourceSha)
})
test('recognized draft migration bytes remain checksum-only in closed mode',()=>{
 const i=input(),plan=planFor(i.map,i.baseline,i.sourceInventory,i.sourceSha)
 assert.deepEqual(i.sourceInventory.files.map(file=>file.path),[
  'hosted/migrations/001_recipes.sql','hosted/migrations/004_draft_ingest.sql',
  'hosted/migrations/005_draft_pairing.sql','hosted/migrations/006_draft_review_workflow.sql',
  'migrations/002_framebriar.sql',
 ])
 assert.deepEqual(Object.keys(plan.components).sort(),['feedback','recipes'])
 assert.equal(plan.draft,false)
})
for(const [name,mutate] of [
 ['runtime other project',i=>i.runtimeEvidence.projectId='prj_Other'],['wrong branch',i=>i.map.neonBranchId='br-other'],['wrong host',i=>i.map.endpointHost='ep-other.us-east-1.aws.neon.tech'],['wrong project',i=>i.map.vercelProjectId='prj_Other'],['wrong source',i=>i.sourceSha='b'.repeat(40)],['expired record',i=>i.now=10000+15*60*1000],['future runtime',i=>i.runtimeEvidence.observedAtMs=10001],['wrong runtime',i=>i.runtimeEvidence.digest='0'.repeat(64)],
])test(`consumer rejects ${name}`,()=>{
 const i=input(),r=inspect(i,execute);mutate(i)
 assert.throws(()=>consume({...i,record:r.record,expectedDigest:r.sha256}))
})
test('runtime observation before long build expires rather than approving deployment',()=>{
 const i=input();i.now+=40*60*1000
 assert.throws(()=>inspect(i,execute))
})
test('stale migration inventory is rejected before any catalog connection',()=>{
 const i=input();i.sourceInventory={...i.sourceInventory,sha256:'0'.repeat(64)}
 assert.throws(()=>inspect(i,()=>assert.fail('must not connect')))
})
test('wrong database server version fails inspection',()=>assert.throws(()=>inspect(input(),()=> '170001')))
test('changed effective grants fail expected baseline comparison',()=>{
 assert.throws(()=>inspect(input(),(env,args)=>{
  const raw=execute(env,args);if(args[0]==='-c')return raw
  const value=JSON.parse(raw);value.relations[0].privileges.DELETE=true;return JSON.stringify(value)
 }))
})
test('CLI failure is generic and never echoes credential/environment',()=>{
 const r=spawnSync(process.execPath,['tools/deploy/readiness-adapter.mjs','invalid','/nonexistent'],{env:{PATH:process.env.PATH,FRAMEBRIAR_PREVIEW_CATALOG_DATABASE_URL:secret},encoding:'utf8',timeout:5000})
 assert.equal(r.status,1);assert.equal(r.stdout,'');assert.equal(r.stderr,'Preview readiness inspection/verification failed [init]; sensitive details withheld.\n')
 assert.ok(!r.stderr.includes('FAKE_SECRET'))
})

test('fixed diagnostics never serialize attacker-controlled exceptions or stderr',()=>{
 const attack='postgresql://SECRET_PASSWORD@secret.example/db\n::error::LEAK'
 assert.equal(diagnostic(attack),'Preview readiness inspection/verification failed [init]; sensitive details withheld.')
 for(const result of [{status:null,error:new Error(attack),stderr:attack},{status:1,stderr:attack},{status:1,stderr:'password authentication failed '+attack},{status:1,stderr:'SSL error '+attack}]){
  const message=diagnostic(psqlFailureStage(result,'db-connect'))
  assert.ok(!message.includes('SECRET'));assert.ok(!message.includes('secret.example'));assert.ok(!message.includes('::error::'))
 }
 assert.equal(psqlFailureStage({status:null,error:new Error(attack)},'db-connect'),'db-client')
 assert.equal(psqlFailureStage({status:1,stderr:'password authentication failed '+attack},'db-connect'),'db-auth')
 assert.equal(psqlFailureStage({status:1,stderr:'SSL error '+attack},'db-connect'),'db-tls')
})
test('inspection reports each fixed stage and attributes each unchanged gate',()=>{
 const seen=[];inspect(input(),execute,s=>seen.push(s))
 assert.deepEqual(seen,['runtime-evidence','inventory','dsn','db-connect','server-major','inspector-policy','recipes','feedback','readiness-record'])
 for(const wanted of ['inspector-policy','recipes','feedback']){
  let stage
  assert.throws(()=>inspect(input(),(connection,args)=>{
   if(stage===wanted)throw new Error('FAKE_SECRET_NOT_FOR_LOGS')
   return execute(connection,args)
  },s=>{stage=s}))
  assert.equal(stage,wanted)
  assert.ok(!diagnostic(stage).includes('FAKE_SECRET'))
 }
 const i=input();i.secret='INVALID_SECRET';let stage
 assert.throws(()=>inspect(i,execute,s=>{stage=s}));assert.equal(stage,'dsn')
})
test('CLI provider map errors emit only their fixed stage',()=>{
 const r=spawnSync(process.execPath,['tools/deploy/readiness-adapter.mjs','inspect','/nonexistent'],{env:{PATH:process.env.PATH,FRAMEBRIAR_PREVIEW_CATALOG_DATABASE_URL:secret,FRAMEBRIAR_PREVIEW_PROVIDER_MAP:'SECRET_MALFORMED_MAP'},encoding:'utf8',timeout:5000})
 assert.equal(r.status,1);assert.equal(r.stdout,'')
 assert.equal(r.stderr,diagnostic('provider-map')+'\n')
})
test('real CLI distinguishes inventory, runtime evidence and DSN without connecting',()=>{
 // Main intentionally lacks the feature migration. Materialize reviewed bytes
 // in an isolated fixture, never an application migration directory or database.
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'readiness-cli-inventory-'))
 try{
  const fixture=JSON.parse(fs.readFileSync(new URL('./fixtures/readiness-cli-inventory.json',import.meta.url),'utf8'))
  assert.equal(fixture.version,1)
  assert.deepEqual(Object.keys(fixture.files).sort(),baseline.inventory.files.map(f=>f.path).sort())
  for(const f of baseline.inventory.files){
   const target=path.join(root,f.path);fs.mkdirSync(path.dirname(target),{recursive:true});fs.writeFileSync(target,fixture.files[f.path])
  }
  assert.deepEqual(inventory(root),baseline.inventory,'CLI fixture must preserve exact reviewed migration hashes')
  const env={PATH:process.env.PATH,FRAMEBRIAR_PREVIEW_CATALOG_DATABASE_URL:'SECRET_INVALID_DSN',FRAMEBRIAR_PREVIEW_PROVIDER_MAP:JSON.stringify(map()),VERCEL_PROJECT_ID:'prj_Synthetic',SOURCE_SHA:'a'.repeat(40),RUNTIME_DIGEST:runtimeDigest,RUNTIME_OBSERVED_AT_MS:String(Date.now()),RUNTIME_PROJECT_ID:'prj_Synthetic'}
  for(const [source,overrides,expected] of [['/nonexistent',{},'inventory'],[root,{RUNTIME_DIGEST:'SECRET_BAD_DIGEST'},'runtime-evidence'],[root,{},'dsn']]){
   const r=spawnSync(process.execPath,['tools/deploy/readiness-adapter.mjs','inspect',source],{env:{...env,...overrides},encoding:'utf8',timeout:5000})
   assert.equal(r.status,1);assert.equal(r.stdout,'');assert.equal(r.stderr,diagnostic(expected)+'\n')
  }
 }finally{fs.rmSync(root,{recursive:true,force:true})}
})
test('actual recipe or feedback policy mismatch is assigned to correct fixed stage',()=>{
 for(const wanted of ['recipes','feedback']){
  let stage
  assert.throws(()=>inspect(input(),(connection,args)=>{
   const raw=execute(connection,args)
   if(stage!==wanted)return raw
   const value=JSON.parse(raw);value.runtimeRole='SECRET_WRONG_ROLE';return JSON.stringify(value)
  },s=>{stage=s}))
  assert.equal(stage,wanted);assert.ok(!diagnostic(stage).includes('SECRET'))
 }
})
