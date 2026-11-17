// Read-only authenticated Vercel plan evidence. This never changes firewall,
// function, deployment or project settings and never emits the bearer token.
import assert from 'node:assert/strict'
import fs from 'node:fs'
import {canonical,digest} from './migration-readiness.mjs'

const capacities=Object.freeze({
 hobby:Object.freeze({rateLimitRuleCapacity:1,maxWindowSeconds:600}),
 pro:Object.freeze({rateLimitRuleCapacity:40,maxWindowSeconds:600}),
 enterprise:Object.freeze({rateLimitRuleCapacity:250,maxWindowSeconds:3600}),
})
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

export async function inspectDraftEdgePlan(approved,request,nowMs){
 approvedIdentity(approved);assert.equal(typeof request,'function')
 assert.ok(Number.isSafeInteger(nowMs)&&nowMs>0)
 const query=`teamId=${encodeURIComponent(approved.accountId)}`
 const project=await boundedJSON(await request(`https://api.vercel.com/v9/projects/${encodeURIComponent(approved.projectId)}?${query}`))
 assert.equal(project.id,approved.projectId);assert.equal(project.accountId,approved.accountId)
 const gitIntegration=Object.hasOwn(project,'link')?'connected':'disconnected'
 if(gitIntegration==='connected')assert.ok(project.link&&typeof project.link==='object'&&!Array.isArray(project.link))
 const account=await boundedJSON(await request(`https://api.vercel.com/v2/teams/${encodeURIComponent(approved.accountId)}`))
 assert.equal(account.id,approved.accountId);assert.equal(account.limited,false)
 assert.ok(account.billing&&typeof account.billing==='object'&&!Array.isArray(account.billing))
 const plan=account.billing.plan;assert.ok(Object.hasOwn(capacities,plan),'Unsupported or unknown Vercel plan')
 const evidence={version:1,target:'preview',projectId:approved.projectId,accountId:approved.accountId,plan,...capacities[plan],gitIntegration,observedAtMs:nowMs}
 return {evidence:canonical(evidence),sha256:digest(canonical(evidence))}
}

export function verifyDraftEdgePlanEvidence(raw,expectedDigest,approved,nowMs){
 approvedIdentity(approved);assert.match(expectedDigest??'',/^[a-f0-9]{64}$/)
 assert.equal(typeof raw,'string');assert.ok(Buffer.byteLength(raw)>0&&Buffer.byteLength(raw)<=8192)
 const evidence=JSON.parse(raw);assert.equal(raw,canonical(evidence),'Noncanonical plan evidence')
 assert.equal(digest(raw),expectedDigest,'Unauthenticated plan evidence')
 assert.deepEqual(Object.keys(evidence).sort(),['accountId','gitIntegration','maxWindowSeconds','observedAtMs','plan','projectId','rateLimitRuleCapacity','target','version'])
 assert.equal(evidence.version,1);assert.equal(evidence.target,'preview')
 assert.equal(evidence.projectId,approved.projectId);assert.equal(evidence.accountId,approved.accountId)
 assert.ok(Object.hasOwn(capacities,evidence.plan),'Unsupported or unknown Vercel plan')
 assert.equal(evidence.rateLimitRuleCapacity,capacities[evidence.plan].rateLimitRuleCapacity)
 assert.equal(evidence.maxWindowSeconds,capacities[evidence.plan].maxWindowSeconds)
 assert.ok(evidence.gitIntegration==='connected'||evidence.gitIntegration==='disconnected')
 assert.ok(Number.isSafeInteger(nowMs)&&Number.isSafeInteger(evidence.observedAtMs))
 assert.ok(evidence.observedAtMs>0&&evidence.observedAtMs<=nowMs&&nowMs-evidence.observedAtMs<15*60*1000,'Plan evidence expired/future')
 return Object.freeze({plan:evidence.plan,rateLimitRuleCapacity:evidence.rateLimitRuleCapacity,maxWindowSeconds:evidence.maxWindowSeconds,gitIntegration:evidence.gitIntegration})
}

if(process.argv[1]&&process.argv[1].endsWith('/draft-edge-plan-adapter.mjs')){
 try{
  assert.equal(process.argv.length,2)
  const token=fs.readFileSync(0,'utf8')
  assert.match(token,/^[A-Za-z0-9._-]{16,4096}$/)
  const approved={projectId:process.env.VERCEL_PROJECT_ID,accountId:process.env.VERCEL_ORG_ID}
  const request=(url)=>fetch(url,{method:'GET',headers:{Authorization:`Bearer ${token}`,Accept:'application/json'},redirect:'error',signal:AbortSignal.timeout(15000)})
  const result=await inspectDraftEdgePlan(approved,request,Date.now())
  assert.ok(process.env.GITHUB_OUTPUT)
  fs.appendFileSync(process.env.GITHUB_OUTPUT,`plan_evidence=${result.evidence}\nplan_evidence_sha256=${result.sha256}\n`)
 }catch{
  console.error('Preview edge plan inspection failed; sensitive details withheld.')
  process.exitCode=1
 }
}
