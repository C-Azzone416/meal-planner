import {test} from 'node:test'
import assert from 'node:assert/strict'
import {spawnSync} from 'node:child_process'
import {inspectDraftEdgeFirewall,verifyDraftEdgeFirewallEvidence} from './draft-edge-firewall-adapter.mjs'
import {draftEdgeRules} from './draft-edge-readiness.mjs'

const approved={projectId:'prj_Synthetic',accountId:'team_Synthetic'}
const now=1800000000000
const response=value=>({status:200,text:async()=>JSON.stringify(value)})
const expectedRule=()=>({
 id:'rule_provider_id',name:'draft-pairing-shared-budget',description:'provider metadata is omitted',active:true,valid:true,validationErrors:null,
 conditionGroup:[{conditions:[
  {type:'environment',op:'eq',value:'preview'},
  {type:'method',op:'eq',value:'POST',neg:false},
  {type:'path',op:'inc',value:['/api/draft-pairings/poll','/api/draft-pairings']},
 ]}],
 action:{mitigate:{action:'rate_limit',actionDuration:'15m',bypassSystem:false,logHeaders:[],redirect:null,rateLimit:{action:'rate_limit',algo:'fixed_window',keys:['ip'],limit:150,window:600}}},
})
const requestFor=({rule=expectedRule(),rules=[],conditional={rules:[]},system=[{result:[]}],enabled=true,project={}}={})=>{
 let page=0
 return async url=>{
  if(url.includes('/v9/projects/'))return response({id:approved.projectId,accountId:approved.accountId,...project})
  if(url.includes('/config/active?'))return response({ownerId:approved.accountId,projectKey:approved.projectId,firewallEnabled:enabled,rules:[rule,...rules]})
  if(url.includes('/bypass/rules?'))return response(conditional)
  if(url.includes('/bypass?'))return response(system[page++]??assert.fail('unexpected system bypass page'))
  assert.fail(`unexpected URL ${url}`)
 }
}

test('authenticated adapter normalizes only the exact active rule',async()=>{
 const result=await inspectDraftEdgeFirewall(approved,requestFor(),now)
 const evidence=JSON.parse(result.evidence)
 assert.deepEqual(evidence.rules,draftEdgeRules);assert.deepEqual(evidence.otherIntersectingRules,[]);assert.deepEqual(evidence.bypasses,[])
 assert.doesNotMatch(result.evidence,/rule_provider_id|provider metadata/)
 assert.deepEqual(verifyDraftEdgeFirewallEvidence(result.evidence,result.sha256,approved,now+1),{rules:draftEdgeRules,otherIntersectingRules:[],bypasses:[]})
})

test('inactive and provably disjoint rules do not block readiness',async()=>{
 const inactive={...expectedRule(),id:'inactive',name:'unrelated',active:false}
 const production={...expectedRule(),id:'production',name:'unrelated-production',conditionGroup:[{conditions:[{type:'environment',op:'eq',value:'production'}]}]}
 const get={...expectedRule(),id:'get',name:'unrelated-get',conditionGroup:[{conditions:[{type:'method',op:'eq',value:'GET'}]}]}
 const otherPath={...expectedRule(),id:'path',name:'unrelated-path',conditionGroup:[{conditions:[{type:'path',op:'eq',value:'/api/other'}]}]}
 const result=await inspectDraftEdgeFirewall(approved,requestFor({rules:[inactive,production,get,otherPath]}),now)
 assert.deepEqual(JSON.parse(result.evidence).otherIntersectingRules,[])
})

for(const [name,mutate] of [
 ['missing rule',()=>({rule:{...expectedRule(),name:'other'}})],
 ['disabled firewall',()=>({enabled:false})],
 ['log-only limit',()=>{const rule=expectedRule();rule.action.mitigate.rateLimit.action='log';return {rule}}],
 ['stable-host-only rule',()=>{const rule=expectedRule();rule.conditionGroup[0].conditions.push({type:'host',op:'eq',value:'athome-meals-dev.vercel.app'});return {rule}}],
 ['wrong window',()=>{const rule=expectedRule();rule.action.mitigate.rateLimit.window=60;return {rule}}],
 ['system bypass',()=>({system:[{result:[{Id:'bypass-sensitive-id'}]}]})],
 ['conditional bypass',()=>({conditional:{rules:[{id:'conditional-sensitive-id'}]}})],
 ['intersecting bypass rule',()=>({rules:[{...expectedRule(),id:'other',name:'other-bypass',action:{mitigate:{action:'bypass'}},conditionGroup:[{conditions:[{type:'method',op:'eq',value:'POST'}]}]}]})],
])test(`verifier rejects ${name}`,async()=>{
 const result=await inspectDraftEdgeFirewall(approved,requestFor(mutate()),now)
 assert.throws(()=>verifyDraftEdgeFirewallEvidence(result.evidence,result.sha256,approved,now+1))
 assert.doesNotMatch(result.evidence,/sensitive-id/)
})

test('system bypass pagination is bounded and redacts bypass counts and identifiers',async()=>{
 const first=Array.from({length:100},(_,i)=>({Id:`b${i}`}))
 const result=await inspectDraftEdgeFirewall(approved,requestFor({system:[{result:first,pagination:{Id:'next',OwnerId:approved.accountId}},{result:[{Id:'last'}]}]}),now)
 assert.deepEqual(JSON.parse(result.evidence).bypasses,[{kind:'system'}])
 assert.doesNotMatch(result.evidence,/b99|last|next/)
})

test('short system-bypass page follows its authenticated continuation cursor',async()=>{
 const result=await inspectDraftEdgeFirewall(approved,requestFor({system:[{result:[],pagination:{Id:'next',OwnerId:approved.accountId}},{result:[{Id:'hidden'}]}]}),now)
 assert.deepEqual(JSON.parse(result.evidence).bypasses,[{kind:'system'}])
 assert.throws(()=>verifyDraftEdgeFirewallEvidence(result.evidence,result.sha256,approved,now+1))
})

for(const [name,system] of [
 ['repeated cursor',[{result:[],pagination:{Id:'same',OwnerId:approved.accountId}},{result:[],pagination:{Id:'same',OwnerId:approved.accountId}}]],
 ['null pagination',[{result:[],pagination:null}]],
 ['wrong pagination owner',[{result:[],pagination:{Id:'next',OwnerId:'team_Other'}}]],
 ['empty cursor',[{result:[],pagination:{Id:'',OwnerId:approved.accountId}}]],
])test(`adapter rejects ${name}`,async()=>assert.rejects(()=>inspectDraftEdgeFirewall(approved,requestFor({system}),now)))

test('system-bypass pagination has a hard page bound',async()=>{
 const system=Array.from({length:100},(_,i)=>({result:[],pagination:{Id:`page-${i+1}`,OwnerId:approved.accountId}}))
 await assert.rejects(()=>inspectDraftEdgeFirewall(approved,requestFor({system}),now),/pagination exceeded bound/)
})

for(const [name,request] of [
 ['wrong project',requestFor({project:{id:'prj_Other'}})],
 ['wrong firewall owner',async url=>response(url.includes('/v9/projects/')?{id:approved.projectId,accountId:approved.accountId}:{ownerId:'team_Other',projectKey:approved.projectId,firewallEnabled:true,rules:[]})],
 ['provider failure',async()=>({status:403,text:async()=>'FAKE_SECRET_PROVIDER_RESPONSE'})],
 ['oversize response',async()=>({status:200,text:async()=>'{'.repeat(1024*1024+1)})],
 ['malformed custom-rule activity',requestFor({rules:[{...expectedRule(),name:'schema-drift',active:undefined}]})],
])test(`adapter rejects ${name}`,async()=>assert.rejects(()=>inspectDraftEdgeFirewall(approved,request,now)))

test('consumer rejects stale, future, altered and cross-project evidence',async()=>{
 const result=await inspectDraftEdgeFirewall(approved,requestFor(),now)
 assert.throws(()=>verifyDraftEdgeFirewallEvidence(result.evidence,result.sha256,approved,now+15*60*1000))
 assert.throws(()=>verifyDraftEdgeFirewallEvidence(result.evidence,result.sha256,approved,now-1))
 assert.throws(()=>verifyDraftEdgeFirewallEvidence(result.evidence.replace('150','151'),result.sha256,approved,now+1))
 assert.throws(()=>verifyDraftEdgeFirewallEvidence(result.evidence,result.sha256,{...approved,projectId:'prj_Other'},now+1))
})

test('CLI failure is fixed and never emits token or provider response',()=>{
 const token='FAKE_SECRET_NOT_FOR_LOGS'
 const r=spawnSync(process.execPath,['tools/deploy/draft-edge-firewall-adapter.mjs'],{env:{PATH:process.env.PATH,VERCEL_PROJECT_ID:approved.projectId,VERCEL_ORG_ID:'user_not_a_team'},input:token,encoding:'utf8',timeout:5000})
 assert.equal(r.status,1);assert.equal(r.stdout,'')
 assert.equal(r.stderr,'Preview edge firewall inspection failed; sensitive details withheld.\n')
 assert.ok(!r.stderr.includes(token))
})
