// Pure normalized-evidence verifier. It does not call Vercel, publish firewall
// rules, change function settings, or authorize activation. A future clean,
// authenticated provider adapter must produce this exact observation.
import assert from 'node:assert/strict'
import {canonical,digest} from './migration-readiness.mjs'

const exact=(value,keys)=>{assert.ok(value&&typeof value==='object'&&!Array.isArray(value));assert.deepEqual(Object.keys(value).sort(),[...keys].sort())}
const identity=value=>{assert.equal(typeof value,'string');assert.match(value,/^[A-Za-z0-9_.-]{1,128}$/)}

export const draftEdgeRules=Object.freeze([
 Object.freeze({name:'draft-pairing-create-burst',path:'/api/draft-pairings',method:'POST',sourceKey:'provider_source_ip',limit:3,windowSeconds:60,algorithm:'fixed_window',action:'rate_limit_429',beforeFunction:true}),
 Object.freeze({name:'draft-pairing-create-sustained',path:'/api/draft-pairings',method:'POST',sourceKey:'provider_source_ip',limit:10,windowSeconds:3600,algorithm:'fixed_window',action:'rate_limit_429',beforeFunction:true}),
 Object.freeze({name:'draft-pairing-poll',path:'/api/draft-pairings/poll',method:'POST',sourceKey:'provider_source_ip',limit:150,windowSeconds:900,algorithm:'fixed_window',action:'rate_limit_429',beforeFunction:true}),
])

export const draftFunctionLimits=Object.freeze([
 Object.freeze({source:'api/draft-pairings/index.go',maxDurationSeconds:15}),
 Object.freeze({source:'api/draft-pairings/poll/index.go',maxDurationSeconds:15}),
])

export function inspectDraftFunctionConfig(raw){
 assert.equal(typeof raw,'string');assert.ok(Buffer.byteLength(raw)>0&&Buffer.byteLength(raw)<=1024*1024)
 const config=JSON.parse(raw)
 assert.ok(config&&typeof config==='object'&&!Array.isArray(config))
 const functions=config.functions
 exact(functions,['api/draft-pairings/index.go','api/draft-pairings/poll/index.go','api/**/*.go'])
 // Vercel documents function-pattern order as significant. Exact matches stay
 // ahead of the fallback and therefore repeat the architecture explicitly.
 assert.deepEqual(Object.keys(functions),['api/draft-pairings/index.go','api/draft-pairings/poll/index.go','api/**/*.go'])
 for(const {source,maxDurationSeconds} of draftFunctionLimits){
  exact(functions[source],['architecture','maxDuration'])
  assert.deepEqual(functions[source],{architecture:'x86_64',maxDuration:maxDurationSeconds})
 }
 exact(functions['api/**/*.go'],['architecture'])
 assert.deepEqual(functions['api/**/*.go'],{architecture:'x86_64'})
 return draftFunctionLimits
}

export function verifyDraftEdgeObservation(observation,approved,nowMs){
 exact(approved,['projectId','sourceSha'])
 identity(approved.projectId);assert.match(approved.sourceSha,/^[a-f0-9]{40}$/)
 assert.ok(Number.isSafeInteger(nowMs)&&nowMs>0)
 exact(observation,['version','target','projectId','sourceSha','hostname','productionAffected','rules','otherIntersectingRules','functions','observedAtMs'])
 assert.equal(observation.version,1);assert.equal(observation.target,'preview')
 assert.equal(observation.projectId,approved.projectId);assert.equal(observation.sourceSha,approved.sourceSha)
 assert.equal(observation.hostname,'athome-meals-dev.vercel.app');assert.equal(observation.productionAffected,false)
 assert.deepEqual(observation.rules,draftEdgeRules);assert.deepEqual(observation.otherIntersectingRules,[])
 assert.deepEqual(observation.functions,draftFunctionLimits)
 assert.ok(Number.isSafeInteger(observation.observedAtMs)&&observation.observedAtMs<=nowMs&&nowMs-observation.observedAtMs<15*60*1000,'edge observation expired/future')
 return digest(canonical(observation))
}
