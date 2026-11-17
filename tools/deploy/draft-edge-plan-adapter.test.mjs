import {test} from 'node:test'
import assert from 'node:assert/strict'
import {spawnSync} from 'node:child_process'
import {inspectDraftEdgePlan,verifyDraftEdgePlanEvidence} from './draft-edge-plan-adapter.mjs'

const approved={projectId:'prj_Synthetic',accountId:'team_Synthetic'}
const response=value=>({status:200,text:async()=>JSON.stringify(value)})
const requestFor=plan=>async url=>{
 if(url===`https://api.vercel.com/v9/projects/${approved.projectId}?teamId=${approved.accountId}`)return response({id:approved.projectId,accountId:approved.accountId})
 if(url===`https://api.vercel.com/v2/teams/${approved.accountId}`)return response({id:approved.accountId,limited:false,billing:{plan}})
 assert.fail(`unexpected URL ${url}`)
}

for(const [plan,rules,window] of [['hobby',1,600],['pro',40,600],['enterprise',250,3600]])test(`authenticated ${plan} plan evidence is bounded`,async()=>{
 const result=await inspectDraftEdgePlan(approved,requestFor(plan),1800000000000)
 const evidence=JSON.parse(result.evidence)
 assert.equal(evidence.plan,plan);assert.equal(evidence.rateLimitRuleCapacity,rules);assert.equal(evidence.maxWindowSeconds,window)
 assert.match(result.sha256,/^[a-f0-9]{64}$/)
 assert.equal(evidence.gitIntegration,'disconnected')
 assert.deepEqual(verifyDraftEdgePlanEvidence(result.evidence,result.sha256,approved,1800000000001),{plan,rateLimitRuleCapacity:rules,maxWindowSeconds:window,gitIntegration:'disconnected'})
})

test('authenticated project evidence reports a connected Git integration without exposing repository metadata',async()=>{
 const request=async url=>{
  if(url.includes('/v9/projects/'))return response({id:approved.projectId,accountId:approved.accountId,link:{type:'github',org:'private-owner',repo:'private-repo'}})
  return response({id:approved.accountId,limited:false,billing:{plan:'hobby'}})
 }
 const result=await inspectDraftEdgePlan(approved,request,1800000000000)
 assert.equal(JSON.parse(result.evidence).gitIntegration,'connected')
 assert.doesNotMatch(result.evidence,/private-owner|private-repo|github/)
})

for(const [name,request] of [
 ['wrong project',async url=>response(url.includes('/v9/projects/')?{id:'prj_Other',accountId:approved.accountId}:{id:approved.accountId,limited:false,billing:{plan:'pro'}})],
 ['wrong account',async url=>response(url.includes('/v9/projects/')?{id:approved.projectId,accountId:'team_Other'}:{id:approved.accountId,limited:false,billing:{plan:'pro'}})],
 ['limited team response',async url=>response(url.includes('/v9/projects/')?{id:approved.projectId,accountId:approved.accountId}:{id:approved.accountId,limited:true,billing:{plan:'pro'}})],
 ['unknown plan',requestFor('future-plan')],
 ['provider failure',async()=>({status:403,text:async()=>'{"token":"FAKE_SECRET"}'})],
 ['oversize response',async()=>({status:200,text:async()=>'{'.repeat(1024*1024+1)})],
 ['malformed Git link',async url=>response(url.includes('/v9/projects/')?{id:approved.projectId,accountId:approved.accountId,link:null}:{id:approved.accountId,limited:false,billing:{plan:'pro'}})],
])test(`plan adapter rejects ${name}`,async()=>assert.rejects(()=>inspectDraftEdgePlan(approved,request,1800000000000)))

test('CLI failure is fixed and never emits token or provider response',()=>{
 const token='FAKE_SECRET_NOT_FOR_LOGS'
 const r=spawnSync(process.execPath,['tools/deploy/draft-edge-plan-adapter.mjs'],{env:{PATH:process.env.PATH,VERCEL_PROJECT_ID:approved.projectId,VERCEL_ORG_ID:'user_not_a_team'},input:token,encoding:'utf8',timeout:5000})
 assert.equal(r.status,1);assert.equal(r.stdout,'')
 assert.equal(r.stderr,'Preview edge plan inspection failed; sensitive details withheld.\n')
 assert.ok(!r.stderr.includes(token))
})

test('consumer rejects stale, future, altered and cross-project plan evidence',async()=>{
 const result=await inspectDraftEdgePlan(approved,requestFor('pro'),1800000000000)
 assert.throws(()=>verifyDraftEdgePlanEvidence(result.evidence,result.sha256,approved,1800000000000+15*60*1000))
 assert.throws(()=>verifyDraftEdgePlanEvidence(result.evidence,result.sha256,approved,1799999999999))
 const altered=result.evidence.replace('"plan":"pro"','"plan":"enterprise"')
 assert.throws(()=>verifyDraftEdgePlanEvidence(altered,result.sha256,approved,1800000000001))
 assert.throws(()=>verifyDraftEdgePlanEvidence(result.evidence,result.sha256,{...approved,projectId:'prj_Other'},1800000000001))
})
