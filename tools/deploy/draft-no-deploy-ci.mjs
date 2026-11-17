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
 assert.equal((raw.match(/^\s*(?:export\s+)?PREVIEW_TRUSTED=true\s*$/gm)||[]).length,1)
 assert.equal((raw.match(/\bPREVIEW_TRUSTED\b/g)||[]).length,4)
 const preview=raw.split('\n  preview:\n')[1]?.split('\n  required:\n')[0];assert.ok(preview)
 const conditional=preview.match(/^    if: >-\n((?:^      .*\n?)+)/m);assert.ok(conditional)
 const expression=conditional[1].trim().split(/\s+/).join(' ')
 assert.equal(expression,"${{ !cancelled() && github.event_name == 'push' && github.ref == 'refs/heads/develop' && needs.affected.result == 'success' && needs.affected.outputs.hosted_preview == 'true' && !contains(needs.*.result, 'failure') && !contains(needs.*.result, 'cancelled') }}")
 assert.match(preview,/uses: \.\/\.github\/workflows\/preview\.yml/);assert.match(preview,/secrets: inherit/)
 assert.equal((raw.match(/uses: \.\/\.github\/workflows\/preview\.yml/g)||[]).length,1)
 assert.doesNotMatch(raw,/secrets\.VERCEL_TOKEN/)
 assert.match(raw,/node --test tools\/deploy\/draft-\*\.test\.mjs/)
 return Object.freeze({event:'pull_request',githubPreviewDeploy:'excluded',workflowSha256:digest(raw)})
}

export function verifyDraftNoDeployAdmission({workflow,planEvidence,planEvidenceSha256,source},approved,nowMs){
 const workflowState=inspectDraftNoDeployWorkflow(workflow)
 assert.deepEqual(Object.keys(source).sort(),['sha','workflowSha256'])
 assert.match(source.sha??'',/^[a-f0-9]{40}$/);assert.match(source.workflowSha256??'',/^[a-f0-9]{64}$/)
 assert.equal(source.workflowSha256,workflowState.workflowSha256,'Inspected workflow does not match approved source checkout')
 const provider=verifyDraftEdgePlanEvidence(planEvidence,planEvidenceSha256,approved,nowMs)
 assert.equal(provider.gitIntegration,'disconnected','Vercel Git integration can deploy the branch push')
 const admission={version:1,event:workflowState.event,sourceSha:source.sha,workflowSha256:source.workflowSha256,projectId:approved.projectId,githubPreviewDeploy:workflowState.githubPreviewDeploy,vercelGitDeploy:'disconnected',observedAtMs:JSON.parse(planEvidence).observedAtMs}
 const serialized=canonical(admission)
 return Object.freeze({admission:serialized,sha256:digest(serialized)})
}
