// Clean trusted inspector/consumer entry point. No target code or SQL executes.
import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import {spawnSync} from 'node:child_process'
import {fileURLToPath} from 'node:url'
import {canonical,digest,inventory,createReadiness,verifyReadiness,parseCanonicalRecord,verifyObservation} from './migration-readiness.mjs'
import {parseProviderMap,validateProviderMap,inspectorEnvironment,assertServerMajor} from './readiness-provider-map.mjs'
import {runtimeDigest} from './readiness-runtime-config.mjs'
const HERE=path.dirname(fileURLToPath(import.meta.url))

export function planFor(map,baseline,sourceInventory,sourceSha){
 validateProviderMap(map)
 assert.match(sourceSha??'',/^[a-f0-9]{40}$/)
 assert.deepEqual(Object.keys(baseline).sort(),['components','inspector','inventory','postgresMajor','version'])
 assert.equal(baseline.version,1);assert.equal(baseline.postgresMajor,map.postgresMajor)
 assert.equal(canonical(sourceInventory),canonical(baseline.inventory),'Reviewed migration bytes changed')
 assert.deepEqual(Object.keys(baseline.components).sort(),['feedback','recipes'])
 const components={}
 for(const component of ['recipes','feedback']){
  const expected=structuredClone(baseline.components[component])
  assert.equal(expected.databaseName,'BASELINE_DATABASE');assert.equal(expected.runtimeRole,'BASELINE_ROLE')
  expected.databaseName=map.databaseName
  expected.runtimeRole=map[component==='recipes'?'recipeRuntimeRole':'feedbackRuntimeRole']
  components[component]={databaseName:map.databaseName,runtimeRole:expected.runtimeRole,expectedSnapshotSha256:digest(canonical(expected))}
 }
 return {version:1,target:'preview',projectId:map.vercelProjectId,databaseBranchId:map.neonBranchId,sourceSha,inventorySha256:sourceInventory.sha256,policySha256:digest(canonical({baseline,map,runtimeDigest})),feedback:true,draft:false,components}
}
export function verifyRuntimeEvidence(evidence,now){
 assert.equal(evidence.digest,runtimeDigest)
 assert.ok(Number.isSafeInteger(now)&&Number.isSafeInteger(evidence.observedAtMs))
 assert.ok(evidence.observedAtMs>0&&evidence.observedAtMs<=now&&now-evidence.observedAtMs<15*60*1000,'Runtime configuration observation expired')
}
export function inspect({map,baseline,sourceInventory,sourceSha,secret,runtimeEvidence,now=Date.now()},execute){
 verifyRuntimeEvidence(runtimeEvidence,now)
 assert.equal(runtimeEvidence.projectId,map.vercelProjectId,'Runtime observation is for another Vercel project')
 const plan=planFor(map,baseline,sourceInventory,sourceSha)
 const connection=inspectorEnvironment(secret,map)
 const major=execute(connection,['-c','SHOW server_version_num'])
 assertServerMajor(major,map)
 const inspectorExpected=structuredClone(baseline.inspector)
 assert.equal(inspectorExpected.databaseName,'BASELINE_DATABASE');assert.equal(inspectorExpected.runtimeRole,'BASELINE_ROLE')
 inspectorExpected.databaseName=map.databaseName;inspectorExpected.runtimeRole=map.inspectorRole
 const inspectorRaw=execute(connection,['-v','component=recipes','-v',`runtime_role=${map.inspectorRole}`,'-f',path.join(HERE,'inspect-migration-readiness.sql')])
 assert.ok(Buffer.byteLength(inspectorRaw)<=256*1024)
 verifyObservation('recipes',JSON.parse(inspectorRaw),{databaseName:map.databaseName,runtimeRole:map.inspectorRole,expectedSnapshotSha256:digest(canonical(inspectorExpected))})
 const observations={}
 for(const component of ['recipes','feedback']){
  const raw=execute(connection,['-v',`component=${component}`,'-v',`runtime_role=${plan.components[component].runtimeRole}`,'-f',path.join(HERE,'inspect-migration-readiness.sql')])
  assert.ok(Buffer.byteLength(raw)<=256*1024)
  observations[component]=JSON.parse(raw)
 }
 const record=createReadiness(plan,observations,{projectId:map.vercelProjectId,databaseBranchId:map.neonBranchId},now)
 return {record:canonical(record),sha256:digest(canonical(record))}
}
export function consume({map,baseline,sourceInventory,sourceSha,record,expectedDigest,runtimeEvidence,now=Date.now()}){
 verifyRuntimeEvidence(runtimeEvidence,now)
 assert.equal(runtimeEvidence.projectId,map.vercelProjectId,'Runtime observation is for another Vercel project')
 const plan=planFor(map,baseline,sourceInventory,sourceSha)
 return verifyReadiness(parseCanonicalRecord(record),plan,expectedDigest,sourceInventory,now)
}

if(process.argv[1]&&fileURLToPath(import.meta.url)===process.argv[1]){
 let home
 try{
  const [mode,sourceRoot]=process.argv.slice(2);assert.ok(['inspect','consume'].includes(mode));assert.equal(process.argv.length,4)
  const secret=process.env.FRAMEBRIAR_PREVIEW_CATALOG_DATABASE_URL
  delete process.env.FRAMEBRIAR_PREVIEW_CATALOG_DATABASE_URL
  const map=parseProviderMap(process.env.FRAMEBRIAR_PREVIEW_PROVIDER_MAP)
  assert.equal(map.vercelProjectId,process.env.VERCEL_PROJECT_ID)
  const baseline=JSON.parse(fs.readFileSync(path.join(HERE,'readiness-baseline-pg18.json'),'utf8'))
  const input={map,baseline,sourceInventory:inventory(sourceRoot),sourceSha:process.env.SOURCE_SHA,runtimeEvidence:{digest:process.env.RUNTIME_DIGEST,observedAtMs:Number(process.env.RUNTIME_OBSERVED_AT_MS),projectId:process.env.RUNTIME_PROJECT_ID}}
  if(mode==='inspect'){
   home=fs.mkdtempSync(path.join(os.tmpdir(),'meal-readiness-'))
   const execute=(connection,args)=>{
    const result=spawnSync('/usr/bin/psql',['-X','-qAt','-v','ON_ERROR_STOP=1',...args],{env:{PATH:'/usr/bin:/bin',HOME:home,LANG:'C.UTF-8',...connection},encoding:'utf8',timeout:20000,maxBuffer:256*1024,killSignal:'SIGKILL'})
    assert.equal(result.status,0,'Catalog inspection failed');assert.ok(!result.error)
    return result.stdout.trim()
   }
   const result=inspect({...input,secret},execute)
   assert.ok(process.env.GITHUB_OUTPUT)
   fs.appendFileSync(process.env.GITHUB_OUTPUT,`record=${result.record}\nrecord_sha256=${result.sha256}\nprovider_map=${canonical(map)}\n`)
  }else{
   consume({...input,record:process.env.READINESS_RECORD,expectedDigest:process.env.READINESS_DIGEST})
   console.log('Verified fresh Preview schema and restricted runtime readiness.')
  }
 }catch{
  console.error('Preview readiness inspection/verification failed; sensitive details withheld.')
  process.exitCode=1
 }finally{if(home)fs.rmSync(home,{recursive:true,force:true})}
}
