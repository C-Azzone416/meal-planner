import fs from 'node:fs'
import path from 'node:path'
import { validateSource, validateEnvironment, validateManifests, manifestEntries, validateIncludedBase } from './policy.mjs'
const input = {subject:process.env.SUBJECT ?? 'pr',sha:process.env.TARGET_SHA, pr:process.env.PR_NUMBER, workflowSha:process.env.GITHUB_WORKFLOW_SHA, approvedWorkflowSha:process.env.APPROVED_WORKFLOW_SHA, repository:process.env.GITHUB_REPOSITORY, actor:process.env.GITHUB_ACTOR, triggeringActor:process.env.GITHUB_TRIGGERING_ACTOR, event:process.env.GITHUB_EVENT_NAME, ref:process.env.GITHUB_REF, workflowRef:process.env.GITHUB_WORKFLOW_REF}
async function get(route) {
  const response = await fetch(`https://api.github.com/repos/${input.repository}${route ? `/${route}` : ""}`, {headers:{Authorization:`Bearer ${process.env.GH_TOKEN}`, Accept:'application/vnd.github+json', 'X-GitHub-Api-Version':'2022-11-28'}, redirect:'error', signal:AbortSignal.timeout(15000)})
  if (!response.ok) throw new Error('Required GitHub metadata unavailable')
  return response.json()
}
if(process.env.PREBUILD_REQUESTED==='true') {
  console.error('::error::Private Preview export is not implemented')
  process.exit(1)
}
try {
  // All URLs use validated numeric/SHA inputs, never a caller-controlled host.
  if (!/^[a-f0-9]{40}$/.test(input.sha ?? '') || !['pr','develop'].includes(input.subject) || (input.subject==='pr' && !/^[1-9][0-9]*$/.test(input.pr ?? '')) || !/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(input.repository ?? '')) throw new Error('Invalid inputs')
  const [repo, pr, main, run, environment, branches] = await Promise.all([get(''),input.subject==='pr' ? get(`pulls/${input.pr}`) : Promise.resolve(null),get('branches/main'),get(`actions/runs/${process.env.GITHUB_RUN_ID}`),get('environments/framebriar-ci'),get('environments/framebriar-ci/deployment-branch-policies')])
  if (input.subject==='pr' && !['main','develop'].includes(pr?.base?.ref)) throw new Error('Invalid base')
  const base = await get(`branches/${input.subject==='pr' ? pr.base.ref : 'develop'}`)
  const evidence = validateSource(input, repo, pr, main, run, base)
  if(input.subject==='pr') validateIncludedBase(await get(`compare/${base.commit.sha}...${input.sha}`),base.commit.sha,input.sha)
  if (process.env.EXPECTED_BASE_SHA && evidence.base_sha !== process.env.EXPECTED_BASE_SHA) throw new Error('Base branch moved')
  validateEnvironment(environment, branches, repo.owner.id)
  const commit = await get(`git/commits/${input.sha}`)
  if (!/^[a-f0-9]{40}$/.test(commit.tree?.sha ?? '')) throw new Error('Missing source tree')
  const tree = await get(`git/trees/${commit.tree.sha}?recursive=1`)
  const entries = manifestEntries(tree)
  const [pkg, lock] = await Promise.all(entries.map(entry => get(`git/blobs/${entry.sha}`)))
  for (const item of [pkg,lock]) if(item.encoding !== 'base64') throw new Error('Manifest must be a base64 blob')
  const pkgBytes=Buffer.from(pkg.content,'base64'), lockBytes=Buffer.from(lock.content,'base64')
  Object.assign(evidence, validateManifests(pkgBytes, lockBytes))
  if (process.env.EXPECTED_PACKAGE_HASH && (evidence.package_sha256 !== process.env.EXPECTED_PACKAGE_HASH || evidence.lock_sha256 !== process.env.EXPECTED_LOCK_HASH)) throw new Error('Approved manifests changed')
  if (process.env.INSTALL_ROOT) {
    fs.mkdirSync(path.join(process.env.INSTALL_ROOT, 'project'), {recursive:true})
    fs.writeFileSync(path.join(process.env.INSTALL_ROOT,'project','package.json'),pkgBytes)
    fs.writeFileSync(path.join(process.env.INSTALL_ROOT,'project','package-lock.json'),lockBytes)
  }
  if (process.env.GITHUB_OUTPUT) for(const [key,value] of Object.entries(evidence)) fs.appendFileSync(process.env.GITHUB_OUTPUT, `${key}=${value}\n`)
  fs.appendFileSync(process.env.GITHUB_STEP_SUMMARY, `Validated ${evidence.subject} subject (${evidence.pr || evidence.base})\n\nTarget SHA: ${evidence.sha}\nTrusted workflow SHA: ${evidence.workflow_sha}\nPackage manifest SHA-256: ${evidence.package_sha256}\nLockfile SHA-256: ${evidence.lock_sha256}\n\nOwner approval authorizes executing this exact source with the private SDK present. No deployment or live report is authorized.\n`)
} catch {
  // Never log API bodies, manifests, credentials or assertion values.
  console.error('::error::Reviewed CI validation failed. Verify owner, current PR/main SHA, trusted workflow revision, environment policy and locked manifests. No target substitution or access fallback is allowed.')
  process.exitCode=1
}
