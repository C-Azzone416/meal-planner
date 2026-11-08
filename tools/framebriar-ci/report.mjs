import fs from 'node:fs'
import path from 'node:path'
import {createClient} from './bridge.mjs'
import {validatePrebuild} from './prebuild-policy.mjs'
import { validateResults } from './policy.mjs'
try {
  const evidence = validateResults({validate:process.env.VALIDATE_RESULT, frontend:process.env.FRONTEND_RESULT}, {sha:process.env.SOURCE_SHA,workflow_sha:process.env.WORKFLOW_SHA,package_sha256:process.env.PACKAGE_HASH,lock_sha256:process.env.LOCK_HASH})
  Object.assign(evidence, { subject:process.env.SUBJECT ?? 'pr', repository:process.env.GITHUB_REPOSITORY, pr:Number(process.env.PR_NUMBER), base_sha:process.env.BASE_SHA, run_id:process.env.GITHUB_RUN_ID, run_attempt:process.env.GITHUB_RUN_ATTEMPT, coverage:['lint','typecheck','unit','browser-chromium-webkit','target-artifacts'] })
  if (!/^[a-f0-9]{40}$/.test(evidence.base_sha ?? '')) throw new Error('Missing base provenance')
  if(process.env.PREBUILD_REQUESTED==='true') {
    if(evidence.subject!=='develop') throw new Error('Prebuild only for develop')
    const client=createClient(evidence.repository,process.env.GH_TOKEN)
    const inventory=await client.json(`actions/runs/${evidence.run_id}/artifacts?per_page=100`)
    if(inventory.total_count!==inventory.artifacts.length) throw new Error('Incomplete artifact inventory')
    const name=`framebriar-preview-output-${evidence.run_id}-${evidence.run_attempt}`
    const matches=inventory.artifacts.filter(item=>item.name===name)
    if(matches.length!==1) throw new Error('Prebuild collision')
    const sealed=JSON.parse(fs.readFileSync(process.env.SEALED_PREBUILD_JSON,'utf8'))
    evidence.prebuild={...sealed,artifact_id:matches[0].id,artifact_digest:matches[0].digest,artifact_name:name}
    validatePrebuild(evidence.prebuild,evidence.sha,Number(evidence.run_id),Number(evidence.run_attempt),inventory.artifacts)
  }
  fs.mkdirSync(process.env.EVIDENCE_DIR, {recursive:true})
  fs.writeFileSync(path.join(process.env.EVIDENCE_DIR,'evidence.json'), JSON.stringify(evidence)+'\n')
  fs.appendFileSync(process.env.GITHUB_STEP_SUMMARY, `## Informational frontend evidence\n\n${JSON.stringify(evidence,null,2)}\n\nTrusted run: https://github.com/${process.env.GITHUB_REPOSITORY}/actions/runs/${process.env.GITHUB_RUN_ID}\n\nThis is not the PR required check, not backend/contract CI, and not release/deployment authorization. Inspect this trusted workflow revision and exact input SHA; similarly named checks do not establish provenance.\n`)
} catch {
  console.error('::error::Required frontend evidence is absent or invalid')
  process.exitCode=1
}
