import {test} from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import {canonical,digest} from './migration-readiness.mjs'
import {inspectDraftNoDeployWorkflow,verifyDraftNoDeployAdmission} from './draft-no-deploy-ci.mjs'

const workflow=fs.readFileSync('.github/workflows/ci.yml','utf8')
const approved={projectId:'prj_Synthetic',accountId:'team_Synthetic'}
const now=1800000000000
const plan=gitIntegration=>{
 const evidence=canonical({version:1,target:'preview',projectId:approved.projectId,accountId:approved.accountId,plan:'hobby',rateLimitRuleCapacity:1,maxWindowSeconds:600,gitIntegration,observedAtMs:now})
 return {evidence,sha256:digest(evidence)}
}

test('pull-request workflow excludes both trusted Preview selectors',()=>{
 assert.deepEqual(inspectDraftNoDeployWorkflow(workflow),{event:'pull_request',githubPreviewDeploy:'excluded'})
})

test('fresh authenticated disconnected-project evidence admits exact source SHA',()=>{
 const evidence=plan('disconnected')
 const result=verifyDraftNoDeployAdmission({workflow,planEvidence:evidence.evidence,planEvidenceSha256:evidence.sha256,sourceSha:'a'.repeat(40)},approved,now+1)
 assert.deepEqual(JSON.parse(result.admission),{event:'pull_request',githubPreviewDeploy:'excluded',observedAtMs:now,projectId:approved.projectId,sourceSha:'a'.repeat(40),vercelGitDeploy:'disconnected',version:1})
 assert.match(result.sha256,/^[a-f0-9]{64}$/)
})

test('connected, stale, altered and wrong-project evidence are rejected',()=>{
 const connected=plan('connected')
 assert.throws(()=>verifyDraftNoDeployAdmission({workflow,planEvidence:connected.evidence,planEvidenceSha256:connected.sha256,sourceSha:'a'.repeat(40)},approved,now+1))
 const disconnected=plan('disconnected')
 assert.throws(()=>verifyDraftNoDeployAdmission({workflow,planEvidence:disconnected.evidence,planEvidenceSha256:disconnected.sha256,sourceSha:'a'.repeat(40)},approved,now+15*60*1000))
 assert.throws(()=>verifyDraftNoDeployAdmission({workflow,planEvidence:disconnected.evidence.replace('hobby','pro'),planEvidenceSha256:disconnected.sha256,sourceSha:'a'.repeat(40)},approved,now+1))
 assert.throws(()=>verifyDraftNoDeployAdmission({workflow,planEvidence:disconnected.evidence,planEvidenceSha256:disconnected.sha256,sourceSha:'a'.repeat(40)},{...approved,projectId:'prj_Other'},now+1))
})

for(const [name,mutate] of [
 ['PR trigger',raw=>raw.replace('pull_request:','issues:')],
 ['trusted event',raw=>raw.replace("github.event_name == 'push'","github.event_name == 'pull_request'")],
 ['trusted ref',raw=>raw.replace("github.ref == 'refs/heads/develop'","github.ref == 'refs/pull/1/merge'")],
 ['deploy call count',raw=>`${raw}\n# uses: ./.github/workflows/preview.yml`],
 ['Vercel secret isolation',raw=>`${raw}\n# secrets.VERCEL_TOKEN`],
 ['draft test coverage',raw=>raw.replace('node --test tools/deploy/draft-*.test.mjs','node --test tools/deploy/readiness-*.test.mjs')],
])test(`workflow inspection fails closed on ${name} drift`,()=>assert.throws(()=>inspectDraftNoDeployWorkflow(mutate(workflow))))
