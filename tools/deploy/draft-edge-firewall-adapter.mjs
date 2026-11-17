// Read-only authenticated Vercel firewall evidence. This adapter issues GETs
// only, follows no redirects and emits neither provider responses nor the token.
import assert from 'node:assert/strict'
import fs from 'node:fs'
import {canonical,digest} from './migration-readiness.mjs'
import {draftEdgeRules} from './draft-edge-readiness.mjs'

const paths=Object.freeze(['/api/draft-pairings','/api/draft-pairings/poll'])
const allowedConditionKeys=new Set(['key','neg','op','type','value'])
const conditionOps=new Set(['eq','ex','gt','gte','inc','list','lt','lte','neq','nex','ninc','pre','re','sub','suf'])
const conditionTypes=new Set(['bot_category','bot_name','bot_protection','bot_status','cookie','domain_environment','environment','geo_as_number','geo_city','geo_continent','geo_country','geo_country_region','header','host','ip_address','ja3_digest','ja4_digest','method','path','protocol','query','rate_limit_api_id','raw_path','region','response_code','response_header','route','ruleset','scheme','server_action','shared_condition','target_path','traffic_source','trusted_source','user_agent'])
const boundedJSON=async response=>{
 assert.equal(response.status,200)
 const raw=await response.text();assert.ok(Buffer.byteLength(raw)>0&&Buffer.byteLength(raw)<=1024*1024)
 return JSON.parse(raw)
}
const approvedIdentity=approved=>{
 assert.deepEqual(Object.keys(approved).sort(),['accountId','projectId'])
 assert.match(approved.projectId??'',/^prj_[A-Za-z0-9]+$/)
 assert.match(approved.accountId??'',/^team_[A-Za-z0-9]+$/)
}
const exactKeys=(value,allowed)=>{
 assert.ok(value&&typeof value==='object'&&!Array.isArray(value))
 for(const key of Object.keys(value))assert.ok(allowed.has(key),`Unsupported provider field ${key}`)
}
const validProviderCondition=condition=>{
 if(!condition||typeof condition!=='object'||Array.isArray(condition))return false
 if(Object.keys(condition).some(key=>!allowedConditionKeys.has(key)))return false
 if(!conditionTypes.has(condition.type)||!conditionOps.has(condition.op))return false
 if(condition.neg!==undefined&&typeof condition.neg!=='boolean')return false
 if(condition.key!==undefined&&typeof condition.key!=='string')return false
 const value=condition.value
 return value===undefined||typeof value==='string'||(typeof value==='number'&&Number.isFinite(value))||
  (Array.isArray(value)&&value.every(item=>typeof item==='string'))
}
const values=value=>Array.isArray(value)?value:[value]
const conditionToken=condition=>{
 exactKeys(condition,allowedConditionKeys)
 assert.ok(condition.key===undefined||condition.key==='')
 assert.ok(condition.neg===undefined||condition.neg===false)
 if(condition.type==='environment'&&condition.op==='eq'&&condition.value==='preview')return 'environment'
 if(condition.type==='method'&&condition.op==='eq'&&condition.value==='POST')return 'method'
 if(condition.type==='path'&&condition.op==='inc'&&Array.isArray(condition.value)&&
    JSON.stringify([...condition.value].sort())===JSON.stringify([...paths].sort()))return 'paths'
 return null
}
const expectedRule=rule=>{
 try{
  assert.equal(rule.name,draftEdgeRules[0].name);assert.equal(rule.active,true);assert.equal(rule.valid,true)
  assert.ok(rule.validationErrors===undefined||rule.validationErrors===null)
  assert.ok(Array.isArray(rule.conditionGroup)&&rule.conditionGroup.length===1)
  const group=rule.conditionGroup[0];assert.deepEqual(Object.keys(group),['conditions'])
  assert.ok(Array.isArray(group.conditions)&&group.conditions.length===3)
  assert.deepEqual(group.conditions.map(conditionToken).sort(),['environment','method','paths'])
  const mitigate=rule.action?.mitigate;assert.ok(mitigate&&typeof mitigate==='object'&&!Array.isArray(mitigate))
  assert.equal(mitigate.action,'rate_limit');assert.ok(mitigate.bypassSystem===undefined||mitigate.bypassSystem===null||mitigate.bypassSystem===false)
  assert.ok(mitigate.redirect===undefined||mitigate.redirect===null)
  assert.ok(mitigate.logHeaders===undefined||(Array.isArray(mitigate.logHeaders)&&mitigate.logHeaders.length===0))
  const rate=mitigate.rateLimit;assert.ok(rate&&typeof rate==='object'&&!Array.isArray(rate))
  assert.equal(rate.action,'rate_limit');assert.equal(rate.algo,'fixed_window')
  assert.deepEqual(rate.keys,['ip']);assert.equal(rate.limit,150);assert.equal(rate.window,600)
  return true
 }catch{return false}
}
const disjointScalar=(condition,type,target)=>{
 if(condition.type!==type||condition.neg===true||!(condition.key===undefined||condition.key===''))return false
 const list=values(condition.value)
 if(condition.op==='eq')return list.length===1&&!list.includes(target)
 if(condition.op==='inc')return !list.includes(target)
 if(condition.op==='neq')return list.includes(target)
 if(condition.op==='ninc')return list.includes(target)
 return false
}
const disjointPaths=condition=>{
 if(condition.type!=='path'||condition.neg===true||!(condition.key===undefined||condition.key===''))return false
 const list=values(condition.value)
 if(condition.op==='eq'||condition.op==='inc')return paths.every(path=>!list.includes(path))
 if(condition.op==='neq'||condition.op==='ninc')return paths.every(path=>list.includes(path))
 return false
}
const mayIntersect=rule=>{
 if(rule.active!==true)return false
 if(!Array.isArray(rule.conditionGroup)||rule.conditionGroup.length===0)return true
 return rule.conditionGroup.some(group=>{
  if(!group||typeof group!=='object'||Array.isArray(group)||Object.keys(group).length!==1||!Object.hasOwn(group,'conditions')||!Array.isArray(group.conditions)||group.conditions.length===0)return true
  if(group.conditions.some(condition=>!validProviderCondition(condition)))return true
  return !group.conditions.some(condition=>disjointScalar(condition,'environment','preview')||disjointScalar(condition,'method','POST')||disjointPaths(condition))
 })
}
const safeAction=rule=>{
 const action=rule?.action?.mitigate?.action
 return ['allow','bypass','challenge','deny','log','rate_limit','redirect'].includes(action)?action:'unknown'
}
const conditionalRules=value=>{
 if(Array.isArray(value?.rules))return value.rules
 if(value&&typeof value==='object'&&typeof value.id==='string')return [value]
 throw new Error('Unsupported conditional bypass response')
}

export async function inspectDraftEdgeFirewall(approved,request,nowMs){
 approvedIdentity(approved);assert.equal(typeof request,'function');assert.ok(Number.isSafeInteger(nowMs)&&nowMs>0)
 const query=`projectId=${encodeURIComponent(approved.projectId)}&teamId=${encodeURIComponent(approved.accountId)}`
 const project=await boundedJSON(await request(`https://api.vercel.com/v9/projects/${encodeURIComponent(approved.projectId)}?teamId=${encodeURIComponent(approved.accountId)}`))
 assert.equal(project.id,approved.projectId);assert.equal(project.accountId,approved.accountId)
 const config=await boundedJSON(await request(`https://api.vercel.com/v1/security/firewall/config/active?${query}`))
 assert.equal(config.ownerId,approved.accountId);assert.equal(config.projectKey,approved.projectId)
 assert.equal(typeof config.firewallEnabled,'boolean');assert.ok(Array.isArray(config.rules))
 for(const rule of config.rules){
  assert.equal(typeof rule?.active,'boolean','Malformed custom-rule activity state')
  assert.equal(typeof rule?.valid,'boolean','Malformed custom-rule validity state')
 }
 const named=config.rules.filter(rule=>rule?.name===draftEdgeRules[0].name)
 const compliant=named.length===1&&expectedRule(named[0])
 const otherActions=new Set(config.rules.filter(rule=>rule!==named[0]&&mayIntersect(rule)).map(safeAction))
 if(named.length!==1||!compliant)otherActions.add('named_rule_mismatch')
 const other=[...otherActions].sort().map(action=>({action}))

 const conditional=conditionalRules(await boundedJSON(await request(`https://api.vercel.com/v1/security/firewall/bypass/rules?${query}`)))
 const bypassKinds=new Set(conditional.length?['conditional']:[])
 let offset='';let pages=0
 do{
  assert.ok(++pages<=100,'System bypass pagination exceeded bound')
  const suffix=offset?`&offset=${encodeURIComponent(offset)}`:''
  const page=await boundedJSON(await request(`https://api.vercel.com/v1/security/firewall/bypass?${query}&projectScope=true&limit=100${suffix}`))
  assert.ok(Array.isArray(page.result));if(page.result.length)bypassKinds.add('system')
  let next=''
  if(Object.hasOwn(page,'pagination')){
   assert.ok(page.pagination&&typeof page.pagination==='object'&&!Array.isArray(page.pagination))
   assert.equal(page.pagination.OwnerId,approved.accountId)
   assert.equal(typeof page.pagination.Id,'string');assert.ok(page.pagination.Id&&page.pagination.Id!==offset)
   next=page.pagination.Id
  }
  offset=next
 }while(offset)

 const bypasses=[...bypassKinds].sort().map(kind=>({kind}))
 const evidence={version:1,target:'preview',projectId:approved.projectId,accountId:approved.accountId,firewallEnabled:config.firewallEnabled,rules:compliant?[draftEdgeRules[0]]:[],otherIntersectingRules:other,bypasses,observedAtMs:nowMs}
 const serialized=canonical(evidence)
 return {evidence:serialized,sha256:digest(serialized)}
}

export function verifyDraftEdgeFirewallEvidence(raw,expectedDigest,approved,nowMs){
 approvedIdentity(approved);assert.match(expectedDigest??'',/^[a-f0-9]{64}$/)
 assert.equal(typeof raw,'string');assert.ok(Buffer.byteLength(raw)>0&&Buffer.byteLength(raw)<=32768)
 const evidence=JSON.parse(raw);assert.equal(raw,canonical(evidence),'Noncanonical firewall evidence')
 assert.equal(digest(raw),expectedDigest,'Unauthenticated firewall evidence')
 assert.deepEqual(Object.keys(evidence).sort(),['accountId','bypasses','firewallEnabled','observedAtMs','otherIntersectingRules','projectId','rules','target','version'])
 assert.equal(evidence.version,1);assert.equal(evidence.target,'preview')
 assert.equal(evidence.projectId,approved.projectId);assert.equal(evidence.accountId,approved.accountId)
 assert.equal(evidence.firewallEnabled,true);assert.deepEqual(evidence.rules,draftEdgeRules)
 assert.deepEqual(evidence.otherIntersectingRules,[]);assert.deepEqual(evidence.bypasses,[])
 assert.ok(Number.isSafeInteger(nowMs)&&Number.isSafeInteger(evidence.observedAtMs))
 assert.ok(evidence.observedAtMs>0&&evidence.observedAtMs<=nowMs&&nowMs-evidence.observedAtMs<15*60*1000,'Firewall evidence expired/future')
 return Object.freeze({rules:draftEdgeRules,otherIntersectingRules:Object.freeze([]),bypasses:Object.freeze([])})
}

if(process.argv[1]&&process.argv[1].endsWith('/draft-edge-firewall-adapter.mjs')){
 try{
  assert.equal(process.argv.length,2)
  const token=fs.readFileSync(0,'utf8');assert.match(token,/^[A-Za-z0-9._-]{16,4096}$/)
  const approved={projectId:process.env.VERCEL_PROJECT_ID,accountId:process.env.VERCEL_ORG_ID}
  const request=url=>fetch(url,{method:'GET',headers:{Authorization:`Bearer ${token}`,Accept:'application/json'},redirect:'error',signal:AbortSignal.timeout(15000)})
  const result=await inspectDraftEdgeFirewall(approved,request,Date.now())
  assert.ok(process.env.GITHUB_OUTPUT)
  fs.appendFileSync(process.env.GITHUB_OUTPUT,`firewall_evidence=${result.evidence}\nfirewall_evidence_sha256=${result.sha256}\n`)
 }catch{
  console.error('Preview edge firewall inspection failed; sensitive details withheld.')
  process.exitCode=1
 }
}
