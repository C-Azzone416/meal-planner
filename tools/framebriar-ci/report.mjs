import fs from 'node:fs'
import path from 'node:path'
import { validateResults } from './policy.mjs'
try {
  const evidence = validateResults({validate:process.env.VALIDATE_RESULT, frontend:process.env.FRONTEND_RESULT}, {sha:process.env.SOURCE_SHA,workflow_sha:process.env.WORKFLOW_SHA,package_sha256:process.env.PACKAGE_HASH,lock_sha256:process.env.LOCK_HASH})
  Object.assign(evidence, { repository:process.env.GITHUB_REPOSITORY, pr:Number(process.env.PR_NUMBER), base_sha:process.env.BASE_SHA, run_id:process.env.GITHUB_RUN_ID, run_attempt:process.env.GITHUB_RUN_ATTEMPT, coverage:['lint','typecheck','unit','browser-chromium-webkit','target-artifacts'] })
  if (!/^[a-f0-9]{40}$/.test(evidence.base_sha ?? '')) throw new Error('Missing base provenance')
  fs.mkdirSync(process.env.EVIDENCE_DIR, {recursive:true})
  fs.writeFileSync(path.join(process.env.EVIDENCE_DIR,'evidence.json'), JSON.stringify(evidence)+'\n')
  fs.appendFileSync(process.env.GITHUB_STEP_SUMMARY, `## Informational frontend evidence\n\n${JSON.stringify(evidence,null,2)}\n\nTrusted run: https://github.com/${process.env.GITHUB_REPOSITORY}/actions/runs/${process.env.GITHUB_RUN_ID}\n\nThis is not the PR required check, not backend/contract CI, and not release/deployment authorization. Inspect this trusted workflow revision and exact input SHA; similarly named checks do not establish provenance.\n`)
} catch {
  console.error('::error::Required frontend evidence is absent or invalid')
  process.exitCode=1
}
