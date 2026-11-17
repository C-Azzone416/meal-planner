import {test} from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import {draftEdgeRules,draftFunctionLimits,inspectDraftFunctionConfig,verifyDraftEdgeObservation} from './draft-edge-readiness.mjs'

const now=1800000000000
const approved={projectId:'prj_test',sourceSha:'a'.repeat(40),plan:'hobby'}
const fixture=()=>({version:1,target:'preview',projectId:approved.projectId,sourceSha:approved.sourceSha,plan:'hobby',hostname:'athome-meals-dev.vercel.app',productionAffected:false,rules:structuredClone(draftEdgeRules),otherIntersectingRules:[],bypasses:[],functions:structuredClone(draftFunctionLimits),observedAtMs:now-1})

test('accept exact Preview-only pairing edge observation',()=>{
 assert.match(verifyDraftEdgeObservation(fixture(),approved,now),/^[a-f0-9]{64}$/)
})

test('source config limits only the two pairing functions before the Go fallback',()=>{
 assert.deepEqual(inspectDraftFunctionConfig(fs.readFileSync('vercel.json','utf8')),draftFunctionLimits)
})

for(const [name,mutate] of [
 ['create duration raised',c=>c.functions['api/draft-pairings/index.go'].maxDuration=16],
 ['poll duration missing',c=>delete c.functions['api/draft-pairings/poll/index.go'].maxDuration],
 ['fallback duration added',c=>c.functions['api/**/*.go'].maxDuration=15],
 ['pairing architecture drift',c=>c.functions['api/draft-pairings/index.go'].architecture='arm64'],
 ['extra function override',c=>c.functions['api/other.go']={maxDuration:15}],
 ['fallback ordered first',c=>c.functions={'api/**/*.go':c.functions['api/**/*.go'],'api/draft-pairings/index.go':c.functions['api/draft-pairings/index.go'],'api/draft-pairings/poll/index.go':c.functions['api/draft-pairings/poll/index.go']}],
])test(`function config rejects ${name}`,()=>{const c=JSON.parse(fs.readFileSync('vercel.json','utf8'));mutate(c);assert.throws(()=>inspectDraftFunctionConfig(JSON.stringify(c)))})

for(const [name,mutate] of [
 ['production target',o=>o.target='production'],
 ['wrong project',o=>o.projectId='prj_other'],
 ['wrong source',o=>o.sourceSha='b'.repeat(40)],
 ['wrong plan',o=>o.plan='pro'],
 ['wrong hostname',o=>o.hostname='meal-planner.example.com'],
 ['Production affected',o=>o.productionAffected=true],
 ['missing shared rule',o=>o.rules.shift()],
 ['second rate rule',o=>o.rules.push(structuredClone(draftEdgeRules[0]))],
 ['extra broad rule',o=>o.otherIntersectingRules.push({path:'/api/:path*'})],
 ['bypass',o=>o.bypasses.push({name:'trusted-source'})],
 ['client header key',o=>o.rules[0].sourceKey='x-forwarded-for'],
 ['rule limited to stable hostname',o=>{delete o.rules[0].hostnameScope;o.rules[0].hostname='athome-meals-dev.vercel.app'}],
 ['rule hostname scope drift',o=>o.rules[0].hostnameScope='stable_alias_only'],
 ['rule environment changed',o=>o.rules[0].environment='production'],
 ['create path missing',o=>o.rules[0].paths.shift()],
 ['path broadened',o=>o.rules[0].paths[1]='/api/:path*'],
 ['extra path',o=>o.rules[0].paths.push('/api/draft-intents')],
 ['budget raised',o=>o.rules[0].limit=1500],
 ['window changed',o=>o.rules[0].windowSeconds=60],
 ['non-POST method',o=>o.rules[0].method='*'],
 ['non-429 action',o=>o.rules[0].action='log'],
 ['after-function throttle',o=>o.rules[0].beforeFunction=false],
 ['duration raised',o=>o.functions[0].maxDurationSeconds=300],
 ['extra function',o=>o.functions.push({source:'api/**/*.go',maxDurationSeconds:300})],
 ['future observation',o=>o.observedAtMs=now+1],
 ['expired observation',o=>o.observedAtMs=now-15*60*1000],
 ['extra assertion',o=>o.approved=true],
])test(`reject ${name}`,()=>{const o=fixture();mutate(o);assert.throws(()=>verifyDraftEdgeObservation(o,approved,now))})
