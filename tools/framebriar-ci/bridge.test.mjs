import {test} from 'node:test'
import assert from 'node:assert/strict'
import {spawnSync} from 'node:child_process'
import fs from 'node:fs'
import {createClient,boundedBody,parseArchive,selectLatest,verifyEvidence} from './bridge.mjs'
import {sha256,validateManifests,workflowPath} from './policy.mjs'
const main='a'.repeat(40),head='b'.repeat(40),base='c'.repeat(40),pkgSha='e'.repeat(40),lockSha='f'.repeat(40)
function zip(entries, options={}) {
 const script=`import sys,json,io,zipfile\nx=json.load(sys.stdin); b=io.BytesIO()\nwith zipfile.ZipFile(b,'w',compression=zipfile.ZIP_DEFLATED) as z:\n for e in x['entries']:\n  i=zipfile.ZipInfo(e['name']); i.compress_type=zipfile.ZIP_DEFLATED; i.external_attr=e.get('mode',0)<<16; z.writestr(i,e['data'])\n z.comment=x['options'].get('comment','').encode()\nsys.stdout.buffer.write(b.getvalue())`
 const r=spawnSync('python3',['-c',script],{input:JSON.stringify({entries,options}),maxBuffer:262144})
 assert.equal(r.status,0);return r.stdout
}
const packed=data=>zip([{name:'evidence.json',data:JSON.stringify(data)}])
const digest=b=>`sha256:${sha256(b)}`
function fixture() {
 const deps={'@masra91/framebriar':'0.4.1'}
 const pkg=JSON.stringify({dependencies:deps}),lock=JSON.stringify({packages:{'':{dependencies:deps},'node_modules/@masra91/framebriar':{version:'0.4.1',resolved:'https://npm.pkg.github.com/download/@masra91/framebriar/0.4.1/9588969af1b2529a9b0f5ec756bc4f1772b1a88b',integrity:'sha512-JxyDmwlo1I8IHqCigKH/rKwOXFSI2mimzc36u+n5l5UHkSnN5U/XCwfyy/+jU8oJs/G1DsDbl+Rztm2GZjQ1kg=='}}})
 const evidence={sha:head,workflow_sha:main,...validateManifests(pkg,lock),outcome:'passed',informational_only:true,repository:'owner/repo',pr:54,base_sha:base,run_id:'123',run_attempt:'1',coverage:['lint','typecheck','unit','browser-chromium-webkit','target-artifacts']}
 const bytes=packed(evidence)
 const run={id:123,run_attempt:1,created_at:'2026-11-08T10:00:00Z',updated_at:'2026-11-08T10:01:00Z',display_title:`Reviewed PR 54 at ${head}`,event:'workflow_dispatch',path:workflowPath,repository:{full_name:'owner/repo'},head_branch:'main',head_sha:main,actor:{login:'owner'},triggering_actor:{login:'owner'},status:'completed',conclusion:'success'}
 const jobs=['Validate owner and immutable source','Owner-approved frontend checks','Publish informational exact-SHA evidence'].map((name,i)=>({id:i+1,name,run_id:123,run_attempt:1,status:'completed',conclusion:'success',started_at:'2026-11-08T10:00:00Z',completed_at:'2026-11-08T10:01:00Z'}))
 const artifact={id:99,name:'framebriar-evidence-123-1',expired:false,size_in_bytes:bytes.length,digest:digest(bytes),workflow_run:{id:123,head_sha:main,repository_id:1,head_repository_id:1,head_branch:'main'},created_at:'2026-11-08T10:00:30Z'}
 const routes={
 '':{id:1,full_name:'owner/repo',owner:{type:'User',login:'owner'}},
 'pulls/54':{number:54,state:'open',head:{sha:head,repo:{full_name:'owner/repo'}},base:{ref:'develop',sha:'0'.repeat(40),repo:{full_name:'owner/repo'}}},
 'branches/main':{name:'main',commit:{sha:main}},'branches/develop':{name:'develop',commit:{sha:base}},
 'actions/workflows/framebriar-reviewed-ci.yml/runs':{total_count:1,workflow_runs:[run]},
 'actions/runs/123':run,'actions/runs/123/attempts/1/jobs':{total_count:3,jobs},
 'actions/runs/123/artifacts':{total_count:1,artifacts:[artifact]},
 [`git/commits/${head}`]:{tree:{sha:head}},
 [`git/trees/${head}`]:{truncated:false,tree:[{path:'web',type:'tree',mode:'040000',sha:head},{path:'web/package.json',type:'blob',mode:'100644',sha:pkgSha},{path:'web/package-lock.json',type:'blob',mode:'100644',sha:lockSha}]},
 [`git/blobs/${pkgSha}`]:{encoding:'base64',content:Buffer.from(pkg).toString('base64')},[`git/blobs/${lockSha}`]:{encoding:'base64',content:Buffer.from(lock).toString('base64')},
 }
 const calls=[], counts={}
 const f={routes,bytes,artifact,run,jobs,evidence,calls,counts,mutate:null}
 f.fetch=async(url,options)=>{
  calls.push({url,options}); const parsed=new URL(url)
  const route=parsed.pathname.replace(/^\/repos\/owner\/repo\/?/,'');counts[route]=(counts[route]??0)+1
  if(parsed.hostname==='store.blob.core.windows.net') {assert.equal(options.headers.Authorization,undefined);return new Response(f.bytes)}
  assert.equal(options.headers.Authorization,'Bearer synthetic-ephemeral-token')
  if(route==='actions/artifacts/99/zip') return new Response(null,{status:302,headers:{location:'https://store.blob.core.windows.net/signed'}})
  if(f.mutate) f.mutate(route,counts[route],f)
  assert.ok(route in routes,`Unexpected request ${route}`)
  return new Response(JSON.stringify(routes[route]),{status:200,headers:{'content-type':'application/json'}})
 }
 f.input={repository:'owner/repo',prNumber:54,targetSha:head,trustedWorkflowSha:main,token:'synthetic-ephemeral-token'}
 return f
}
test('full read-only retrieval validates current live base, digest, clean reporter and manifests',async()=>{
 const f=fixture();assert.equal((await verifyEvidence(f.input,f.fetch)).sha,head)
 assert.equal(f.counts['branches/develop'],2)
 assert.ok(f.calls.every(call=>call.options.redirect==='manual'))
})
for(const [name,mutate] of [
 ['pending latest run',f=>{f.run.status='in_progress';f.run.conclusion=null}],
 ['failed latest run',f=>{f.run.conclusion='failure'}],
 ['newer failed run rather than older pass',f=>{const newer={...f.run,id:124,conclusion:'failure'};f.routes['actions/workflows/framebriar-reviewed-ci.yml/runs']={total_count:2,workflow_runs:[newer,f.run]};f.routes['actions/runs/124']=newer}],
 ['reverse-completion older success',f=>{const older={...f.run,id:122,created_at:'2026-11-08T09:59:00Z',updated_at:'2026-11-08T10:06:00Z'};f.run.conclusion='failure';f.run.updated_at='2026-11-08T10:05:00Z';f.routes['actions/workflows/framebriar-reviewed-ci.yml/runs']={total_count:2,workflow_runs:[older,f.run]}}],
 ['wrong trusted pin',f=>f.input.trustedWorkflowSha=head],
 ['absent explicit pin',f=>delete f.input.trustedWorkflowSha],
 ['fork PR',f=>f.routes['pulls/54'].head.repo.full_name='fork/repo'],
 ['duplicate named artifact',f=>f.routes['actions/runs/123/artifacts']={total_count:2,artifacts:[f.artifact,{...f.artifact,id:100}]}],
 ['missing artifact',f=>f.routes['actions/runs/123/artifacts']={total_count:0,artifacts:[]}],
 ['foreign repository artifact',f=>f.artifact.workflow_run.repository_id=2],
 ['wrong branch artifact',f=>f.artifact.workflow_run.head_branch='feature'],
 ['expired artifact',f=>f.artifact.expired=true],
 ['digest mismatch',f=>f.artifact.digest='sha256:'+'0'.repeat(64)],
 ['oversized artifact',f=>f.artifact.size_in_bytes=131073],
 ['target-created artifact',f=>f.artifact.created_at='2026-11-08T09:59:00Z'],
 ['reporter collision failure',f=>f.jobs[2].conclusion='failure'],
 ['wrong attempt',f=>f.jobs[2].run_attempt=2],
 ['unsupported registry blob',f=>f.routes[`git/blobs/${pkgSha}`].content=Buffer.from('{}').toString('base64')],
 ['symlink manifest',f=>f.routes[`git/trees/${head}`].tree[1].mode='120000'],
 ['unbounded run inventory',f=>f.routes['actions/workflows/framebriar-reviewed-ci.yml/runs'].total_count=301],
 ['head moved during download',f=>f.mutate=(route,n)=>{if(route==='pulls/54'&&n===2)f.routes[route].head.sha=main}],
 ['base moved during download',f=>f.mutate=(route,n)=>{if(route==='branches/develop'&&n===2)f.routes[route].commit.sha=main}],
 ['main moved during download',f=>f.mutate=(route,n)=>{if(route==='branches/main'&&n===2)f.routes[route].commit.sha=head}],
 ['rerun started during download',f=>f.mutate=(route,n)=>{if(route==='actions/runs/123'&&n===2)f.routes[route].run_attempt=2}],
 ['new dispatch during download',f=>f.mutate=(route,n)=>{if(route==='actions/workflows/framebriar-reviewed-ci.yml/runs'&&n===2)f.routes[route]={total_count:1,workflow_runs:[{...f.run,id:124,status:'queued',conclusion:null}]}}],
 ['artifact replaced during verification',f=>f.mutate=(route,n)=>{if(route==='actions/runs/123/artifacts'&&n===2)f.routes[route]={total_count:1,artifacts:[{...f.artifact,id:100}]}}],
]) test(`retrieval rejects ${name}`,async()=>{const f=fixture();mutate(f);await assert.rejects(verifyEvidence(f.input,f.fetch))})
test('archive parser accepts one metadata file and rejects malformed/ambiguous contents',()=>{
 const good=packed({hello:'world'});assert.deepEqual(parseArchive(good,digest(good)),{hello:'world'})
 const bad=[
  zip([{name:'../evidence.json',data:'{}'}]),zip([{name:'/evidence.json',data:'{}'}]),
  zip([{name:'evidence.json',data:'{}'},{name:'extra.txt',data:'x'}]),
  zip([{name:'evidence.json',data:'{}'},{name:'evidence.json',data:'{}'}]),
  zip([{name:'evidence.json',data:'{}',mode:0o120777}]),
  zip([{name:'evidence.json',data:'{"x":1,"x":2}'}]),
  zip([{name:'evidence.json',data:'{"x":NaN}'}]),
  zip([{name:'evidence.json',data:'[]'}]),
  zip([{name:'evidence.json',data:' '.repeat(16385)}]),
  zip([{name:'evidence.json',data:'{}'}],{comment:'extra'}),
  Buffer.concat([good,Buffer.from('trailing')]),Buffer.concat([Buffer.from('prefix'),good]),
 ]
 for(const bytes of bad)assert.throws(()=>parseArchive(bytes,digest(bytes)))
 assert.throws(()=>parseArchive(good,'sha256:'+'0'.repeat(64)))
})
test('bounded downloads reject advertised and streamed oversized bodies',async()=>{
 await assert.rejects(boundedBody(new Response('x',{headers:{'content-length':'999'}}),10))
 await assert.rejects(boundedBody(new Response('x'.repeat(11)),10))
})
test('signed storage receives no authorization and untrusted redirects fail closed',async()=>{
 for(const location of ['http://store.blob.core.windows.net/x','https://attacker.test/x','https://store.blob.core.windows.net:444/x','https://user@store.blob.core.windows.net/x']) {
  const client=createClient('owner/repo','synthetic',async()=>new Response(null,{status:302,headers:{location}}))
  await assert.rejects(client.archive(99))
 }
 const client=createClient('owner/repo','synthetic',async(url,options)=>{
  if(url.startsWith('https://api.github.com'))return new Response(null,{status:302,headers:{location:'https://store.blob.core.windows.net/x'}})
  assert.equal(options.headers.Authorization,undefined)
  return new Response(null,{status:302,headers:{location:'https://attacker.test'}})
 })
 await assert.rejects(client.archive(99))
})
test('newer differently targeted dispatch prevents fallback to an older success',()=>{
 const f=fixture();const chosen=selectLatest([{...f.run,id:124,display_title:`Reviewed PR 54 at ${main}`},f.run],54)
 assert.equal(chosen.run.id,124);assert.notEqual(chosen.target,head)
})

test('old reruns require verified attempt initiation before newest dispatch',()=>{
 const f=fixture()
 const old={...f.run,id:122,run_attempt:2,created_at:'2026-11-08T09:00:00Z'}
 const before={id:122,run_attempt:2,status:'completed',run_started_at:'2026-11-08T09:59:00Z'}
 assert.throws(()=>selectLatest([f.run,old],54))
 assert.equal(selectLatest([f.run,old],54,new Map([[122,before]])).run.id,123)
 assert.throws(()=>selectLatest([f.run,old],54,new Map([[122,{...before,run_started_at:'2026-11-08T10:02:00Z'}]])))
 assert.throws(()=>selectLatest([f.run,old],54,new Map([[122,{...before,status:'in_progress'}]])))
})
test('reverse completion cannot replace newer failed dispatch with older success',()=>{
 const f=fixture()
 const older={...f.run,id:122,created_at:'2026-11-08T10:00:00Z',updated_at:'2026-11-08T10:06:00Z'}
 const newer={...f.run,id:123,created_at:'2026-11-08T10:04:00Z',updated_at:'2026-11-08T10:05:00Z',conclusion:'failure'}
 assert.equal(selectLatest([older,newer],54).run.conclusion,'failure')
})
test('unrelated malformed titles are ignored; identifiable target attempts block',()=>{
 const f=fixture()
 assert.equal(selectLatest([f.run,{display_title:'Reviewed PR 99 invalid'},{display_title:'Unrelated manual run'}],54).run.id,123)
 for(const display_title of ['Reviewed PR 54 invalid','Reviewed PR 054 invalid','Reviewed PR 54oops']) {
  assert.throws(()=>selectLatest([f.run,{display_title}],54))
 }
 assert.throws(()=>selectLatest([f.run,{display_title:'Broken title',pull_requests:[{number:54}]}],54))
})
test('streamed ZIP data-descriptor layout is accepted without extraction',()=>{
 const script=`import io,sys,zipfile\nclass Stream(io.BytesIO):\n def seek(self,*args): raise io.UnsupportedOperation()\nb=Stream()\nwith zipfile.ZipFile(b,'w',compression=zipfile.ZIP_DEFLATED) as z: z.writestr('evidence.json','{"ok":true}')\nsys.stdout.buffer.write(b.getvalue())`
 const result=spawnSync('python3',['-c',script],{maxBuffer:131072})
 assert.equal(result.status,0)
 assert.deepEqual(parseArchive(result.stdout,digest(result.stdout)),{ok:true})
 const corrupt=Buffer.from(result.stdout);corrupt[45]^=255
 assert.throws(()=>parseArchive(corrupt,digest(corrupt)))
})
test('API client imposes a hard request budget without retries',async()=>{
 let count=0
 const client=createClient('owner/repo','synthetic',async()=>{count++;return new Response('{}')})
 for(let i=0;i<40;i++)await client.json('')
 await assert.rejects(client.json(''))
 assert.equal(count,40)
})
test('retrieval rejects extra JSON fields even with a valid archive digest',async()=>{
 const f=fixture();f.bytes=packed({...f.evidence,unexpected:'extra'});f.artifact.digest=digest(f.bytes);f.artifact.size_in_bytes=f.bytes.length
 await assert.rejects(verifyEvidence(f.input,f.fetch))
})

test('pinned official upload-artifact v7 writer archive is accepted',()=>{
 const bytes=Buffer.from(fs.readFileSync(new URL('./fixtures/upload-artifact-v7.zip.base64',import.meta.url),'utf8').trim(),'base64')
 assert.equal(bytes.readUInt16LE(6),8) // streamed data descriptor
 assert.equal(bytes.readUInt16LE(8),8) // DEFLATE
 assert.equal(bytes.includes(Buffer.from('504b0606','hex')),false)
 assert.deepEqual(parseArchive(bytes,digest(bytes)),{synthetic:true,format:'upload-artifact-v7'})
})
test('retrieval proves older rerun chronology using attempt-specific API',async()=>{
 const f=fixture()
 const older={...f.run,id:122,run_attempt:2,created_at:'2026-11-08T09:00:00Z'}
 f.routes['actions/workflows/framebriar-reviewed-ci.yml/runs']={total_count:2,workflow_runs:[f.run,older]}
 f.routes['actions/runs/122/attempts/2']={id:122,run_attempt:2,status:'completed',run_started_at:'2026-11-08T09:59:00Z'}
 assert.equal((await verifyEvidence(f.input,f.fetch)).run_id,123)
 assert.equal(f.counts['actions/runs/122/attempts/2'],2)
})
for(const [name,attempt] of [
 ['later successful old rerun',{id:122,run_attempt:2,status:'completed',run_started_at:'2026-11-08T10:02:00Z'}],
 ['later failed old rerun',{id:122,run_attempt:2,status:'completed',conclusion:'failure',run_started_at:'2026-11-08T10:02:00Z'}],
 ['pending old rerun',{id:122,run_attempt:2,status:'queued',run_started_at:'2026-11-08T09:59:00Z'}],
 ['unknown old rerun start',{id:122,run_attempt:2,status:'completed'}],
 ['mismatched old rerun attempt',{id:122,run_attempt:1,status:'completed',run_started_at:'2026-11-08T09:59:00Z'}],
])test(`retrieval blocks ${name} rather than using old or newer success`,async()=>{
 const f=fixture();const older={...f.run,id:122,run_attempt:2,created_at:'2026-11-08T09:00:00Z'}
 f.routes['actions/workflows/framebriar-reviewed-ci.yml/runs']={total_count:2,workflow_runs:[older,f.run]}
 f.routes['actions/runs/122/attempts/2']=attempt
 await assert.rejects(verifyEvidence(f.input,f.fetch))
})
