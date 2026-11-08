import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
export const workflowPath = '.github/workflows/framebriar-reviewed-ci.yml'
export const sha256 = value => createHash('sha256').update(value).digest('hex')
const sha = value => assert.match(value ?? '', /^[a-f0-9]{40}$/, 'Full lowercase SHA required')
export function validateSource(input, repo, pr, main, run, base) {
  sha(input.sha); sha(input.workflowSha); sha(input.approvedWorkflowSha)
  const subject=input.subject ?? 'pr'
  assert.ok(['pr','develop'].includes(subject),'Unsupported subject')
  assert.equal(repo.full_name, input.repository)
  assert.equal(repo.owner.type, 'User', 'This policy is for a sole-owner personal repository')
  assert.equal(input.actor, repo.owner.login, 'Only the owner may dispatch')
  assert.equal(input.triggeringActor, repo.owner.login, 'Only the owner may rerun')
  assert.equal(input.event, 'workflow_dispatch')
  assert.equal(input.ref, 'refs/heads/main')
  assert.equal(input.workflowRef, `${input.repository}/${workflowPath}@refs/heads/main`)
  assert.equal(input.approvedWorkflowSha, input.workflowSha, 'Unapproved workflow revision')
  assert.equal(main.commit.sha, input.workflowSha, 'Trusted main moved; dispatch and approve a new run')
  assert.equal(run.event, 'workflow_dispatch')
  assert.equal(run.head_sha, input.workflowSha)
  assert.equal(run.path, workflowPath)
  assert.equal(run.repository.full_name, input.repository)
  assert.equal(run.actor.login, repo.owner.login)
  if(subject==='develop') {
    assert.equal(input.pr ?? '', '', 'Develop subject must not reuse PR identity')
    assert.equal(base.name,'develop'); assert.equal(base.commit.sha,input.sha,'Develop head moved')
    return {subject,sha:input.sha,workflow_sha:input.workflowSha,pr:0,base:'develop',base_sha:input.sha}
  }
  assert.match(input.pr ?? '', /^[1-9][0-9]*$/, 'PR number required')
  assert.equal(pr.number, Number(input.pr))
  assert.equal(pr.state, 'open')
  assert.equal(pr.head.repo?.full_name, input.repository, 'Fork PRs are not eligible')
  assert.equal(pr.base.repo?.full_name, input.repository)
  assert.ok(['main', 'develop'].includes(pr.base.ref), 'Unexpected target branch')
  assert.equal(pr.head.sha, input.sha, 'PR head moved; approval is not transferable')
  assert.equal(base.name, pr.base.ref)
  sha(base.commit.sha)
  return { subject, sha: input.sha, workflow_sha: input.workflowSha, pr: pr.number, base: pr.base.ref, base_sha: base.commit.sha }
}
export function validateEnvironment(environment, branches, ownerId) {
  assert.equal(environment.name, 'framebriar-ci')
  assert.equal(environment.can_admins_bypass, false, 'Environment approval must not be bypassable')
  assert.equal(environment.deployment_branch_policy?.protected_branches, false)
  assert.equal(environment.deployment_branch_policy?.custom_branch_policies, true)
  assert.equal(branches.total_count, 1)
  assert.deepEqual(branches.branch_policies.map(({name,type}) => ({name,type})), [{name:'main',type:'branch'}])
  const reviewers = environment.protection_rules.filter(rule => rule.type === 'required_reviewers')
  assert.equal(reviewers.length, 1)
  assert.equal(reviewers[0].prevent_self_review, false, 'Owner must be able to approve own dispatch')
  assert.equal(reviewers[0].reviewers.length, 1)
  assert.equal(reviewers[0].reviewers[0].type, 'User')
  assert.equal(reviewers[0].reviewers[0].reviewer.id, ownerId)
}
export function validateManifests(pkgBytes, lockBytes) {
  const pkg = JSON.parse(pkgBytes), lock = JSON.parse(lockBytes)
  assert.equal(pkg.dependencies?.['@masra91/framebriar'], '0.4.1')
  assert.equal(lock.packages?.['']?.dependencies?.['@masra91/framebriar'], '0.4.1')
  const sdk = lock.packages?.['node_modules/@masra91/framebriar']
  assert.equal(sdk?.version, '0.4.1')
  assert.equal(sdk?.integrity, 'sha512-JxyDmwlo1I8IHqCigKH/rKwOXFSI2mimzc36u+n5l5UHkSnN5U/XCwfyy/+jU8oJs/G1DsDbl+Rztm2GZjQ1kg==')
  for (const [name, entry] of Object.entries(lock.packages)) {
    if (!name) continue
    assert.ok(!entry.link, 'Workspace/link dependencies are not allowed in credential-bearing installation')
    assert.ok(entry.resolved, 'Every dependency must have a locked registry URL')
    const url = new URL(entry.resolved)
    assert.equal(url.protocol, 'https:'); assert.equal(url.username, ''); assert.equal(url.password, '')
    assert.equal(url.search, ''); assert.equal(url.hash, '')
    if (name === 'node_modules/@masra91/framebriar') {
      assert.equal(url.host, 'npm.pkg.github.com')
      assert.equal(url.href, 'https://npm.pkg.github.com/download/@masra91/framebriar/0.4.1/9588969af1b2529a9b0f5ec756bc4f1772b1a88b')
    } else assert.equal(url.host, 'registry.npmjs.org', 'Only reviewed package may use the private registry')
    assert.match(entry.integrity ?? '', /^sha512-[A-Za-z0-9+/]+={0,2}$/, 'Integrity required')
  }
  return { package_sha256: sha256(pkgBytes), lock_sha256: sha256(lockBytes) }
}
export function validateResults(results, evidence) {
  assert.deepEqual(results, {validate:'success', frontend:'success'}, 'Missing or failed frontend evidence')
  sha(evidence.sha); sha(evidence.workflow_sha)
  for (const key of ['package_sha256','lock_sha256']) assert.match(evidence[key] ?? '', /^[a-f0-9]{64}$/)
  return {...evidence, outcome:'passed', informational_only:true}
}

// Pure verification for a future no-secret PR gate. Check names alone are never evidence.
export function validateBridge({repository, owner, pr, currentBase, trustedWorkflowSha, run, jobs, artifact, evidence, subject='pr'}) {
  sha(trustedWorkflowSha)
  assert.ok(['pr','develop'].includes(subject))
  assert.equal(evidence.subject ?? 'pr',subject,'Subject mismatch')
  if(subject==='pr') {
    assert.equal(pr.state, 'open')
    assert.equal(pr.head.repo?.full_name, repository)
    assert.equal(pr.base.repo?.full_name, repository)
  }
  assert.equal(run.event, 'workflow_dispatch')
  assert.equal(run.path, workflowPath)
  assert.equal(run.repository.full_name, repository)
  assert.equal(run.head_branch, 'main')
  assert.equal(run.head_sha, trustedWorkflowSha, 'Untrusted workflow revision')
  assert.equal(run.actor.login, owner)
  assert.equal(run.triggering_actor.login, owner)
  assert.equal(run.status, 'completed')
  assert.equal(run.conclusion, 'success', 'Entire dispatch must succeed')
  const expectedJobs = ['Validate owner and immutable source', 'Owner-approved frontend checks', 'Publish informational exact-SHA evidence']
  for (const name of expectedJobs) {
    const matches = jobs.filter(job => job.name === name)
    assert.equal(matches.length, 1, 'Required trusted job missing or duplicated')
    assert.equal(matches[0].status, 'completed'); assert.equal(matches[0].conclusion, 'success')
    assert.equal(matches[0].run_id, run.id); assert.equal(matches[0].run_attempt, run.run_attempt)
  }
  const reporter = jobs.find(job => job.name === expectedJobs[2])
  assert.equal(artifact.name, `framebriar-evidence-${run.id}-${run.run_attempt}`)
  assert.equal(artifact.expired, false)
  assert.equal(artifact.workflow_run.id, run.id)
  assert.equal(artifact.workflow_run.head_sha, trustedWorkflowSha)
  const created=Date.parse(artifact.created_at), started=Date.parse(reporter.started_at), completed=Date.parse(reporter.completed_at)
  assert.ok(Number.isFinite(created) && created >= started && created <= completed, 'Artifact must originate during clean reporter execution')
  assert.equal(evidence.repository, repository)
  assert.equal(evidence.run_id, String(run.id)); assert.equal(evidence.run_attempt, String(run.run_attempt))
  if(subject==='pr') {
    assert.equal(evidence.pr, pr.number)
    assert.equal(evidence.sha, pr.head.sha, 'Frontend evidence is for a different PR head')
    assert.equal(currentBase.name, pr.base.ref)
  } else {
    assert.equal(evidence.pr,0)
    assert.equal(currentBase.name,'develop')
    assert.equal(evidence.sha,currentBase.commit.sha,'Develop head moved')
  }
  sha(currentBase.commit.sha)
  assert.equal(evidence.base_sha, currentBase.commit.sha, 'Base moved; rerun against current context')
  assert.equal(evidence.workflow_sha, trustedWorkflowSha)
  assert.equal(evidence.outcome, 'passed'); assert.equal(evidence.informational_only, true)
  assert.deepEqual(evidence.coverage, ['lint','typecheck','unit','browser-chromium-webkit','target-artifacts'])
  validateResults({validate:'success',frontend:'success'}, evidence)
  return true
}

export function manifestEntries(tree) {
  assert.equal(tree.truncated, false, 'Truncated tree cannot prove manifest file modes')
  const web = tree.tree.filter(item => item.path === 'web')
  assert.equal(web.length, 1); assert.equal(web[0].type, 'tree'); assert.equal(web[0].mode, '040000')
  return ['web/package.json','web/package-lock.json'].map(name => {
    const matches=tree.tree.filter(item => item.path === name)
    assert.equal(matches.length, 1)
    assert.equal(matches[0].type, 'blob'); assert.equal(matches[0].mode, '100644', 'Manifest symlinks/nonregular files are forbidden')
    sha(matches[0].sha)
    return matches[0]
  })
}

export function validateIncludedBase(comparison, baseSha, headSha) {
  sha(baseSha);sha(headSha)
  assert.ok(['ahead','identical'].includes(comparison.status),'PR must incorporate current base')
  assert.equal(comparison.merge_base_commit?.sha,baseSha,'Current base is not in reviewed head')
  assert.equal(comparison.base_commit?.sha,baseSha)
}
