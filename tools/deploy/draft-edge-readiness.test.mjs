import {test} from 'node:test'
import assert from 'node:assert/strict'
import {draftEdgeRules,draftFunctionLimits,verifyDraftEdgeObservation} from './draft-edge-readiness.mjs'

const now=1800000000000
const approved={projectId:'prj_test',sourceSha:'a'.repeat(40)}
const fixture=()=>({version:1,target:'preview',projectId:approved.projectId,sourceSha:approved.sourceSha,hostname:'athome-meals-dev.vercel.app',productionAffected:false,rules:structuredClone(draftEdgeRules),otherIntersectingRules:[],functions:structuredClone(draftFunctionLimits),observedAtMs:now-1})

test('accept exact Preview-only pairing edge observation',()=>{
 assert.match(verifyDraftEdgeObservation(fixture(),approved,now),/^[a-f0-9]{64}$/)
})

for(const [name,mutate] of [
 ['production target',o=>o.target='production'],
 ['wrong project',o=>o.projectId='prj_other'],
 ['wrong source',o=>o.sourceSha='b'.repeat(40)],
 ['wrong hostname',o=>o.hostname='meal-planner.example.com'],
 ['Production affected',o=>o.productionAffected=true],
 ['missing create burst',o=>o.rules.shift()],
 ['extra broad rule',o=>o.otherIntersectingRules.push({path:'/api/:path*'})],
 ['client header key',o=>o.rules[0].sourceKey='x-forwarded-for'],
 ['poll path broadened',o=>o.rules[2].path='/api/:path*'],
 ['poll budget raised',o=>o.rules[2].limit=1500],
 ['poll window changed',o=>o.rules[2].windowSeconds=60],
 ['non-POST method',o=>o.rules[0].method='*'],
 ['non-429 action',o=>o.rules[0].action='log'],
 ['after-function throttle',o=>o.rules[0].beforeFunction=false],
 ['duration raised',o=>o.functions[0].maxDurationSeconds=300],
 ['extra function',o=>o.functions.push({source:'api/**/*.go',maxDurationSeconds:300})],
 ['future observation',o=>o.observedAtMs=now+1],
 ['expired observation',o=>o.observedAtMs=now-15*60*1000],
 ['extra assertion',o=>o.approved=true],
])test(`reject ${name}`,()=>{const o=fixture();mutate(o);assert.throws(()=>verifyDraftEdgeObservation(o,approved,now))})
