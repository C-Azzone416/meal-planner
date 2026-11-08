import assert from 'node:assert/strict'
import { validateReadiness } from './migration-policy.mjs'
export function validatePrebuild(value,sha,runId,attempt,artifacts) {
  assert.deepEqual(Object.keys(value).sort(),['migration_readiness','artifact_id','artifact_digest','artifact_name','tar_sha256','source_sha','target','enabled','screenshots'].sort())
  assert.ok(Number.isSafeInteger(value.artifact_id)&&value.artifact_id>0)
  validateReadiness(value.migration_readiness,sha)
  assert.equal(value.source_sha,sha);assert.equal(value.target,'preview')
  assert.equal(value.enabled,false);assert.equal(value.screenshots,false)
  assert.match(value.tar_sha256,/^[a-f0-9]{64}$/)
  assert.match(value.artifact_digest,/^sha256:[a-f0-9]{64}$/)
  assert.equal(value.artifact_name,`framebriar-preview-output-${runId}-${attempt}`)
  const matches=artifacts.filter(item=>item.name===value.artifact_name)
  assert.equal(matches.length,1,'Missing/duplicate prebuild artifact')
  const artifact=matches[0]
  assert.equal(artifact.id,value.artifact_id);assert.equal(artifact.digest,value.artifact_digest)
  assert.equal(artifact.expired,false);assert.equal(artifact.workflow_run.id,runId)
  assert.ok(Number.isSafeInteger(artifact.size_in_bytes)&&artifact.size_in_bytes>0&&artifact.size_in_bytes<=268435456,'Prebuild artifact exceeds 256 MiB')
  return artifact
}
