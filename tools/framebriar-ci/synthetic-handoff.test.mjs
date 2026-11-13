// Nonoperational end-to-end contract demo. All identities and authority are fixtures.
import {test} from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import {fork} from 'node:child_process'
import {createHash} from 'node:crypto'
import {bindingKeys,parseReceipt,verifyReceipt,producerWorkflowPath,reporterJobName,outputArtifactName} from './preview-receipt.mjs'
const hash=b=>createHash('sha256').update(b).digest('hex')
const wait=ms=>new Promise(r=>setTimeout(r,ms))
const producer=new URL('./fixtures/synthetic-handoff/producer.mjs',import.meta.url)
const names=['fake-sdk.txt','index.html']
function scan(root,source=false){
 const entries=fs.readdirSync(root).sort()
 if(source&&entries.includes('diagnostics.json')){const d=fs.lstatSync(path.join(root,'diagnostics.json'));assert.ok(d.isFile()&&!d.isSymbolicLink()&&d.nlink===1&&d.size<=4096);entries.splice(entries.indexOf('diagnostics.json'),1)}
 assert.deepEqual(entries,names,'closed synthetic inventory')
 return names.map(name=>{
  const p=path.join(root,name),before=fs.lstatSync(p)
  assert.ok(before.isFile()&&!before.isSymbolicLink()&&before.nlink===1&&before.size<=4096,'bounded regular independent file')
  const fd=fs.openSync(p,fs.constants.O_RDONLY|fs.constants.O_NOFOLLOW)
  try{
   const opened=fs.fstatSync(fd);assert.equal(opened.ino,before.ino);assert.equal(opened.dev,before.dev)
   const bytes=Buffer.alloc(before.size+1),count=fs.readSync(fd,bytes,0,bytes.length,0),after=fs.fstatSync(fd)
   assert.equal(count,before.size);assert.equal(after.size,before.size);assert.equal(after.mtimeMs,before.mtimeMs);assert.equal(after.nlink,1)
   return {name,bytes:bytes.subarray(0,count),sha256:hash(bytes.subarray(0,count))}
  }finally{fs.closeSync(fd)}
 })
}
function inspect(root,source=false){
 const files=scan(root,source),inventory=JSON.stringify(files.map(({name,sha256,bytes})=>({name,sha256,size:bytes.length})))
 // Deterministic JSON package for this demo, NOT the production archive format.
 const archive=JSON.stringify(files.map(({name,bytes})=>({name,base64:bytes.toString('base64')})))
 return {archiveSha256:hash(archive),inventorySha256:hash(inventory),fileCount:files.length,byteCount:files.reduce((n,f)=>n+f.bytes.length,0)}
}
async function terminateGroup(child,closed){
 if(!Number.isSafeInteger(child.pid)||child.pid<=0)throw Error('producer did not obtain a process group')
 try{process.kill(-child.pid,'SIGTERM');await wait(25)}catch(e){if(e.code!=='ESRCH')throw e}
 try{process.kill(-child.pid,'SIGKILL')}catch(e){if(e.code!=='ESRCH')throw e}
 let closeTimer
 try{await Promise.race([closed,new Promise((_,reject)=>{closeTimer=setTimeout(()=>reject(Error('producer close timeout')),2000)})])}finally{clearTimeout(closeTimer)}
 // Group must be gone before any trusted read/copy. Failure is not a fallback.
 for(let i=0;i<200;i++){
  try{process.kill(-child.pid,0)}catch(e){if(e.code==='ESRCH')return;throw e}
  await wait(10)
 }
 throw Error('producer group not confirmed gone')
}
async function produceAndSeal(root,mode,onStopped=()=>{}){
 const scratch=path.join(root,'scratch'),sealed=path.join(root,'sealed')
 fs.mkdirSync(scratch,{mode:0o700})
 const child=fork(producer,[scratch,mode],{detached:true,env:{},execArgv:[],stdio:['ignore','ignore','ignore','ipc']})
 const closed=new Promise(resolve=>child.once('close',resolve))
 let timer
 try{
  await new Promise((resolve,reject)=>{
   timer=setTimeout(()=>reject(Error('producer readiness timeout')),mode==='timeout'?100:3000)
   child.once('error',reject)
   child.once('message',m=>m&&Object.keys(m).length===1&&m.ready===true?resolve():reject(Error('unexpected readiness message')))
   child.once('exit',()=>reject(Error('producer exited before sealing')))
  })
 }finally{clearTimeout(timer);await terminateGroup(child,closed);onStopped(child.pid)}
 fs.mkdirSync(sealed,{mode:0o700})
 for(const f of scan(scratch,true))fs.writeFileSync(path.join(sealed,f.name),f.bytes,{flag:'wx',mode:0o600})
 assert.deepEqual(inspect(sealed),inspect(scratch,true))
 return {scratch,sealed}
}
function authorityFixture(content){
 const sha='a'.repeat(40),d='b'.repeat(64)
 const plan={repositoryId:1,repository:'example/synthetic',sourceSha:sha,baseSha:sha,baseRef:'develop',manifestSha256:d,lockSha256:d,workflowPath:producerWorkflowPath,workflowSha:sha,workflowId:2,runId:3,runAttempt:1,actorId:4,projectId:'prj_synthetic',target:'preview',origin:'https://preview.example.test',appId:'meal-planner-preview',enabled:true,screenshots:false,vercelVersion:'65.9.0',goVersion:'1.26.11',goBuilderVersion:'27.0.1',staticBuilderVersion:'26.0.1',readinessId:'synthetic-ready',readinessSha256:d,migrationSha256:d,schemaPolicySha256:d,routesSha256:d,maxAgeMs:600000}
 const receipt={...Object.fromEntries(bindingKeys.map(k=>[k,plan[k]])),version:1,reporterJobId:5,artifactId:6,artifactName:outputArtifactName,artifactDigest:`sha256:${content.archiveSha256}`, ...content,issuedAt:'2026-11-13T01:00:25Z',expiresAt:'2026-11-13T01:10:10Z'}
 const run={id:3,attempt:1,workflowId:2,workflowPath:producerWorkflowPath,repositoryId:1,headSha:sha,headBranch:'main',event:'workflow_dispatch',actorId:4,triggeringActorId:4,status:'completed',conclusion:'success'}
 const evidence={repository:{id:1,fullName:plan.repository},refs:{mainSha:sha,developSha:sha,sourceContainsBase:true},run,latestRun:{...run},reporter:{id:5,runId:3,attempt:1,name:reporterJobName,status:'completed',conclusion:'success',startedAt:'2026-11-13T01:00:00Z',completedAt:'2026-11-13T01:00:30Z',artifactId:6,artifactDigest:receipt.artifactDigest,archiveSha256:content.archiveSha256,inventorySha256:content.inventorySha256},artifact:{id:6,name:outputArtifactName,digest:receipt.artifactDigest,runId:3,expired:false,createdAt:'2026-11-13T01:00:20Z',sizeInBytes:content.byteCount},content,readiness:{id:plan.readinessId,sha256:d,migrationSha256:d,target:'preview',projectId:plan.projectId,sourceSha:sha,policySha256:d},inventoryComplete:true}
 return {plan,receipt,evidence}
}
function consume(sealed,f,wire=JSON.stringify(f.receipt)){
 return verifyReceipt(parseReceipt(wire),f.plan,{...f.evidence,content:inspect(sealed)},Date.parse('2026-11-13T01:01:00Z'))
}
async function fixture(fn){const root=fs.mkdtempSync(path.join(os.tmpdir(),'framebriar-synthetic-'));try{return await fn(root)}finally{fs.rmSync(root,{recursive:true,force:true})}}
test('synthetic producer group terminates before seal, receipt and consumer agree',()=>fixture(async root=>{
 const {sealed,scratch}=await produceAndSeal(root,'good'),f=authorityFixture(inspect(sealed))
 assert.equal(consume(sealed,f).artifactId,6)
 fs.writeFileSync(path.join(scratch,'fake-sdk.txt'),'changed original')
 assert.equal(consume(sealed,f).artifactId,6,'sealed copy independent of original')
 assert.equal(fs.statSync(path.join(sealed,'fake-sdk.txt')).nlink,1)
}))
for(const [mode,expected] of [
 ['symlink',/bounded regular independent file/],['hardlink',/bounded regular independent file/],
 ['fifo',/bounded regular independent file/],['extra',/closed synthetic inventory/],
 ['oversize',/bounded regular independent file/],['missing',/closed synthetic inventory/],
 ['background',/closed synthetic inventory/],['timeout',/producer readiness timeout/],
])test(`synthetic handoff rejects ${mode}`,()=>fixture(async root=>{
 let stoppedPid
 await assert.rejects(produceAndSeal(root,mode,pid=>{stoppedPid=pid}),expected)
 assert.ok(Number.isSafeInteger(stoppedPid),'cleanup confirmed independently of rejection')
 assert.throws(()=>process.kill(-stoppedPid,0),e=>e.code==='ESRCH','producer group must no longer exist')
 assert.equal(fs.existsSync(path.join(root,'sealed','fake-sdk.txt')),false)
}))
test('consumer rejects receipt tampering and sealed output tampering',()=>fixture(async root=>{
 const {sealed}=await produceAndSeal(root,'good'),f=authorityFixture(inspect(sealed))
 assert.throws(()=>consume(sealed,f,JSON.stringify({...f.receipt,sourceSha:'c'.repeat(40)})))
 assert.throws(()=>consume(sealed,f,'{"version":1,"version":1}'))
 fs.writeFileSync(path.join(sealed,'fake-sdk.txt'),'tampered')
 assert.throws(()=>consume(sealed,f))
}))
test('OS isolation NOT validated: prior canary probe FAILED; network untested',t=>{
 t.skip('NOT a passing isolation check. Historical canary read-denial FAILED; network probe never reached. Default-context initialization separately denied. No retry/fallback.')
})
test('known synthetic diagnostics remain outside sealed output',()=>fixture(async root=>{
 const {scratch,sealed}=await produceAndSeal(root,'diagnostics')
 assert.equal(fs.existsSync(path.join(scratch,'diagnostics.json')),true)
 assert.equal(fs.existsSync(path.join(sealed,'diagnostics.json')),false)
 assert.equal(consume(sealed,authorityFixture(inspect(sealed))).artifactId,6)
}))
test('valid encoded fake source passes shape checks, demonstrating secrecy limit',()=>fixture(async root=>{
 const {sealed}=await produceAndSeal(root,'encoded')
 assert.equal(consume(sealed,authorityFixture(inspect(sealed))).artifactId,6)
 assert.equal(Buffer.from(fs.readFileSync(path.join(sealed,'fake-sdk.txt'),'utf8'),'base64').toString(),'FAKE PRIVATE SOURCE CANARY')
}))
