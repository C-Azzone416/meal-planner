// Admission contract for a source-only pull-request CI run. GitHub's workflow
// must exclude Preview deployment, and fresh authenticated Vercel project
// evidence must show that Git integration cannot deploy the branch push.
import assert from 'node:assert/strict'
import {canonical,digest} from './migration-readiness.mjs'
import {verifyDraftEdgePlanEvidence} from './draft-edge-plan-adapter.mjs'

export function inspectDraftNoDeployWorkflow(raw){
 assert.equal(typeof raw,'string');assert.ok(Buffer.byteLength(raw)>0&&Buffer.byteLength(raw)<=256*1024)
 assert.match(raw,/on:\n  pull_request:\n    branches: \[develop, main\]\n  push:\n    branches: \[develop, main\]/)
 assert.match(raw,/export PREVIEW_TRUSTED=false/)
 assert.match(raw,/if \[\[ "\$EVENT_NAME" == "push" && "\$GIT_REF" == "refs\/heads\/develop" \]\]; then\n            PREVIEW_TRUSTED=true/)
 const preview=raw.split('\n  preview:\n')[1]?.split('\n  required:\n')[0];assert.ok(preview)
 assert.match(preview,/github\.event_name == 'push'/);assert.match(preview,/github\.ref == 'refs\/heads\/develop'/)
 assert.match(preview,/needs\.affected\.outputs\.hosted_preview == 'true'/)
 assert.match(preview,/uses: \.\/\.github\/workflows\/preview\.yml/);assert.match(preview,/secrets: inherit/)
 assert.equal((raw.match(/uses: \.\/\.github\/workflows\/preview\.yml/g)||[]).length,1)
 assert.doesNotMatch(raw,/secrets\.VERCEL_TOKEN/)
 assert.match(raw,/node --test tools\/deploy\/draft-\*\.test\.mjs/)
 return Object.freeze({event:'pull_request',githubPreviewDeploy:'excluded'})
}

export function verifyDraftNoDeployAdmission({workflow,planEvidence,planEvidenceSha256,sourceSha},approved,nowMs){
 const source=inspectDraftNoDeployWorkflow(workflow)
 assert.match(sourceSha??'',/^[a-f0-9]{40}$/)
 const provider=verifyDraftEdgePlanEvidence(planEvidence,planEvidenceSha256,approved,nowMs)
 assert.equal(provider.gitIntegration,'disconnected','Vercel Git integration can deploy the branch push')
 const admission={version:1,event:source.event,sourceSha,projectId:approved.projectId,githubPreviewDeploy:source.githubPreviewDeploy,vercelGitDeploy:'disconnected',observedAtMs:JSON.parse(planEvidence).observedAtMs}
 const serialized=canonical(admission)
 return Object.freeze({admission:serialized,sha256:digest(serialized)})
}
