import {test} from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import {spawnSync} from 'node:child_process'
import {planFor,inspect,consume} from './readiness-adapter.mjs'
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
 assert.equal(r.status,1);assert.equal(r.stdout,'');assert.equal(r.stderr,'Preview readiness inspection/verification failed; sensitive details withheld.\n')
 assert.ok(!r.stderr.includes('FAKE_SECRET'))
})
