import {test} from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import {spawnSync} from 'node:child_process'
const workflow=fs.readFileSync(new URL('../../.github/workflows/preview.yml',import.meta.url),'utf8')
const start='              # BEGIN SAME_SHA_ALIAS_POLICY',end='              # END SAME_SHA_ALIAS_POLICY'
assert.equal(workflow.split(start).length,2);assert.equal(workflow.split(end).length,2)
const block=workflow.split(start)[1].split(end)[0].split('\n').slice(1).join('\n')
const sha='a'.repeat(40),project='prj_synthetic'
const doc=(id,createdAt)=>({id,projectId:project,meta:{sourceSha:sha},target:null,readyState:'READY',createdAt})
function run(candidate=doc('dpl_new',200),current=doc('dpl_old',100),id='dpl_new',curid='dpl_old'){
 const r=spawnSync('/bin/bash',['-euo','pipefail','-c',block+'\nprintf "POLICY_PROMOTE\\n"'],{env:{PATH:process.env.PATH,SHA:sha,VERCEL_PROJECT_ID:project,id,curid,deployment:typeof candidate==='string'?candidate:JSON.stringify(candidate),current:JSON.stringify(current)},encoding:'utf8',timeout:5000,maxBuffer:8192})
 assert.ifError(r.error);return {status:r.status,promote:r.stdout.includes('POLICY_PROMOTE'),output:r.stdout}
}
test('strictly newer candidate promotes',()=>{const r=run();assert.equal(r.status,0);assert.equal(r.promote,true)})
for(const age of [100,99])test(`same-SHA candidate age ${age} cannot replace equal/newer deployment`,()=>{const r=run(doc('dpl_new',age));assert.equal(r.status,0);assert.equal(r.promote,false)})
test('exact deployment remains idempotent',()=>{const r=run(doc('dpl_new',200),doc('dpl_new',200),'dpl_new','dpl_new');assert.equal(r.status,0);assert.equal(r.promote,false)})
for(const side of ['candidate','current'])for(const [key,value] of [['projectId','prj_other'],['id','dpl_other'],['meta',{sourceSha:'b'.repeat(40)}],['target','production'],['readyState','BUILDING'],['createdAt',null],['createdAt','200'],['createdAt',0],['createdAt',1.5],['createdAt',9007199254740992]])test(`reject ${side} ${key}=${JSON.stringify(value)}`,()=>{
 const a=doc('dpl_new',200),b=doc('dpl_old',100);(side==='candidate'?a:b)[key]=value;const r=run(a,b);assert.equal(r.status,1);assert.equal(r.promote,false)
})
test('malformed candidate fails closed',()=>{const r=run('{');assert.equal(r.status,1);assert.equal(r.promote,false)})
test('production guard and cross-SHA ancestry remain before assignment',()=>{
 const assignment=workflow.indexOf('https://api.vercel.com/v2/deployments/${id}/aliases')
 for(const guard of ['is a production domain of this project; refusing','merge-base --is-ancestor "$SHA" "$cur"','could not compare $SHA with $cur'])assert.ok(workflow.indexOf(guard)>0&&workflow.indexOf(guard)<assignment)
 assert.ok(workflow.indexOf(start)<assignment)
})

for(const side of ['candidate','current'])test(`missing target rejected for ${side}`,()=>{const a=doc('dpl_new',200),b=doc('dpl_old',100);delete (side==='candidate'?a:b).target;const r=run(a,b);assert.equal(r.status,1);assert.equal(r.promote,false)})
test('explicit preview target accepted',()=>{const a=doc('dpl_new',200),b=doc('dpl_old',100);a.target=b.target='preview';const r=run(a,b);assert.equal(r.status,0);assert.equal(r.promote,true)})
