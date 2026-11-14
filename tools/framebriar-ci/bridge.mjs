// Read-only CI evidence verifier. No package credentials, target execution or deployment artifacts.
import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { sha256, validateBridge, workflowPath, manifestEntries, validateManifests, validateIncludedBase } from './policy.mjs'
const MAX_ARCHIVE=131072, MAX_API=1048576
const sha = value => assert.match(value ?? '', /^[a-f0-9]{40}$/)
const integer = value => assert.ok(Number.isSafeInteger(value) && value > 0)

export async function boundedBody(response, limit) {
  const length=response.headers.get('content-length')
  if(length !== null) assert.ok(/^\d+$/.test(length) && Number(length) <= limit, 'Response too large')
  assert.ok(response.body, 'Missing response body')
  const reader=response.body.getReader(), chunks=[]
  let size=0
  try {
    for (;;) {
      const {done,value}=await reader.read()
      if(done) break
      size+=value.byteLength
      assert.ok(size <= limit, 'Response exceeded byte limit')
      chunks.push(Buffer.from(value))
    }
  } catch(error) { await reader.cancel().catch(()=>{}); throw error }
  return Buffer.concat(chunks,size)
}
export function parseArchive(bytes, digest) {
  assert.ok(bytes.length <= MAX_ARCHIVE)
  assert.match(digest ?? '', /^sha256:[a-f0-9]{64}$/)
  assert.equal(`sha256:${sha256(bytes)}`,digest,'Artifact digest mismatch')
  const parsed=spawnSync('python3',[fileURLToPath(new URL('./read-evidence-zip.py',import.meta.url))], {input:bytes, timeout:3000,maxBuffer:32768,env:{PATH:process.env.PATH ?? '/usr/bin:/bin'},encoding:'utf8'})
  assert.equal(parsed.status,0,'Archive rejected')
  return JSON.parse(parsed.stdout)
}
function relevantRuns(runs, prNumber) {
  return runs.flatMap(run=>{
    const title=run.display_title ?? ''
    if(prNumber===0) {
      if(!title.startsWith('Reviewed develop')) return []
      const match=/^Reviewed develop at ([a-f0-9]{40})$/.exec(title)
      assert.ok(match,'Malformed develop subject title'); integer(run.id);integer(run.run_attempt)
      const created=Date.parse(run.created_at);assert.ok(Number.isFinite(created))
      return [{run,pr:0,target:match[1],created}]
    }
    // Attribute first. A malformed unrelated title must not poison every PR.
    const identity=/^Reviewed PR ([0-9]+)/.exec(title)
    const associated=run.pull_requests?.some(pr=>pr.number===prNumber) ?? false
    if ((!identity || Number(identity[1])!==prNumber) && !associated) return []
    const match=/^Reviewed PR ([1-9][0-9]*) at ([a-f0-9]{40})$/.exec(title)
    assert.ok(match && Number(match[1])===prNumber,'Malformed title for target PR')
    integer(run.id);integer(run.run_attempt)
    const created=Date.parse(run.created_at)
    assert.ok(Number.isFinite(created),'Missing dispatch creation time')
    return [{run,pr:prNumber,target:match[2],created}]
  })
}
export function selectLatest(runs, prNumber, attempts=new Map()) {
  const titles=relevantRuns(runs,prNumber)
  titles.sort((a,b)=>b.run.id-a.run.id)
  assert.ok(titles.length,'No reviewed run for this PR')
  const newest=titles[0]
  for(const older of titles.slice(1)) {
    assert.ok(older.created<=newest.created,'Dispatch chronology is inconsistent')
    if(older.run.run_attempt===1) continue
    // A rerun never revives an older dispatch as successful evidence. Require
    // attempt-specific API chronology to prove it predates the newest dispatch.
    const attempt=attempts.get(older.run.id)
    assert.ok(attempt,'Older rerun chronology must be verified')
    assert.equal(attempt.id,older.run.id);assert.equal(attempt.run_attempt,older.run.run_attempt)
    assert.equal(attempt.status,'completed','An older rerun is still pending')
    const started=Date.parse(attempt.run_started_at)
    assert.ok(Number.isFinite(started) && started>=older.created && started<newest.created,
      'An older rerun started after/with the newest dispatch, or chronology is unknown; start a fresh dispatch')
  }
  // Dispatch initiation order dominates completion order. Never fall back from
  // a newer failed/pending/different-head dispatch to an older success.
  return newest
}
async function selectVerified(client,runs,prNumber) {
  const relevant=relevantRuns(runs,prNumber).sort((a,b)=>b.run.id-a.run.id)
  const attempts=new Map()
  for(const item of relevant.slice(1)) {
    if(item.run.run_attempt>1) attempts.set(item.run.id,await client.json(`actions/runs/${item.run.id}/attempts/${item.run.run_attempt}`))
  }
  return selectLatest(runs,prNumber,attempts)
}
export function createClient(repository, token, fetcher=fetch) {
  assert.match(repository,/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/)
  assert.ok(token,'Ephemeral workflow GitHub token required')
  let remaining=40
  const deadline=Date.now()+90000
  async function request(url, headers) {
    assert.ok(remaining-- > 0 && Date.now()<deadline,'Request/time budget exhausted')
    return fetcher(url,{headers,redirect:'manual',signal:AbortSignal.timeout(Math.max(1,Math.min(10000,deadline-Date.now())))})
  }
  const headers={Authorization:`Bearer ${token}`,Accept:'application/vnd.github+json','X-GitHub-Api-Version':'2022-11-28'}
  const url=route=>`https://api.github.com/repos/${repository}${route ? `/${route}`:''}`
  return {
    async json(route) {
      const response=await request(url(route),headers)
      assert.equal(response.status,200,'Required GitHub metadata unavailable')
      return JSON.parse((await boundedBody(response,MAX_API)).toString('utf8'))
    },
    async archive(id) {
      integer(id)
      const response=await request(url(`actions/artifacts/${id}/zip`),headers)
      if(response.status===200) return boundedBody(response,MAX_ARCHIVE)
      assert.equal(response.status,302,'Expected GitHub artifact redirect')
      const location=new URL(response.headers.get('location'))
      assert.equal(location.protocol,'https:');assert.equal(location.port,'')
      assert.equal(location.username,'');assert.equal(location.password,'')
      assert.ok(location.hostname.endsWith('.blob.core.windows.net') || location.hostname.endsWith('.actions.githubusercontent.com'),'Unknown artifact storage host')
      // Never forward the GitHub Authorization header to signed storage URLs.
      const storage=await request(location.href,{Accept:'application/octet-stream'})
      assert.equal(storage.status,200,'Artifact storage must not redirect again')
      return boundedBody(storage,MAX_ARCHIVE)
    },
  }
}
async function list(client, route, field, maximum=300) {
  const items=[]; let total
  for(let page=1;page<=3;page++) {
    const response=await client.json(`${route}${route.includes('?')?'&':'?'}per_page=100&page=${page}`)
    assert.ok(Number.isInteger(response.total_count) && response.total_count>=0 && response.total_count<=maximum,'Inventory exceeds safe verification bound')
    if(total===undefined) total=response.total_count
    assert.equal(response.total_count,total,'Inventory changed while reading')
    assert.ok(Array.isArray(response[field]))
    items.push(...response[field])
    if(items.length>=total) {assert.equal(items.length,total);assert.equal(new Set(items.map(item=>item.id)).size,items.length,'Duplicate inventory entries');return items}
    assert.ok(response[field].length>0,'Incomplete inventory')
  }
  throw new Error('Incomplete bounded inventory')
}
export async function verifyEvidence({repository,prNumber,targetSha,trustedWorkflowSha,token,subject='pr',requirePreview=false}, fetcher=fetch) {
  assert.ok(['pr','develop'].includes(subject))
  if(subject==='pr') integer(prNumber); else assert.equal(prNumber,0)
  sha(targetSha);sha(trustedWorkflowSha)
  const client=createClient(repository,token,fetcher)
  const repo=await client.json('')
  assert.equal(repo.full_name,repository);assert.equal(repo.owner.type,'User');integer(repo.id)
  const pr=subject==='pr' ? await client.json(`pulls/${prNumber}`) : null
  if(subject==='pr') {
    assert.equal(pr.number,prNumber);assert.equal(pr.state,'open')
    assert.equal(pr.head.repo?.full_name,repository);assert.equal(pr.base.repo?.full_name,repository)
    assert.ok(['main','develop'].includes(pr.base.ref));assert.equal(pr.head.sha,targetSha)
  }
  const main=await client.json('branches/main')
  assert.equal(main.commit.sha,trustedWorkflowSha,'Main changed: explicitly review/update trusted pin')
  const branch=subject==='pr' ? pr.base.ref : 'develop'
  const currentBase=await client.json(`branches/${branch}`)
  if(subject==='develop') assert.equal(currentBase.commit.sha,targetSha,'Develop evidence cannot reuse a PR/ancestor SHA')
  else validateIncludedBase(await client.json(`compare/${currentBase.commit.sha}...${targetSha}`),currentBase.commit.sha,targetSha)
  const runs=await list(client,`actions/workflows/framebriar-reviewed-ci.yml/runs?event=workflow_dispatch&branch=main`,'workflow_runs')
  const selected=await selectVerified(client,runs,prNumber)
  assert.equal(selected.target,targetSha,'Latest reviewed attempt targets a different head')
  const run=await client.json(`actions/runs/${selected.run.id}`)
  assert.equal(run.id,selected.run.id)
  assert.equal(run.run_attempt,selected.run.run_attempt,'Run attempt moved')
  assert.equal(run.status,'completed');assert.equal(run.conclusion,'success','Latest run has not passed; do not reuse an older success')
  assert.equal(run.path,workflowPath);assert.equal(run.head_sha,trustedWorkflowSha)
  const jobs=await list(client,`actions/runs/${run.id}/attempts/${run.run_attempt}/jobs`,'jobs')
  const artifacts=await list(client,`actions/runs/${run.id}/artifacts`,'artifacts')
  const name=`framebriar-evidence-${run.id}-${run.run_attempt}`
  const matches=artifacts.filter(item=>item.name===name)
  assert.equal(matches.length,1,'Missing/duplicate evidence artifact')
  const artifact=matches[0]
  assert.ok(Number.isInteger(artifact.size_in_bytes) && artifact.size_in_bytes>0 && artifact.size_in_bytes<=MAX_ARCHIVE,'Artifact too large')
  assert.equal(artifact.expired,false)
  assert.equal(artifact.workflow_run?.repository_id,repo.id)
  assert.equal(artifact.workflow_run?.head_repository_id,repo.id)
  assert.equal(artifact.workflow_run?.head_branch,'main')
  const bytes=await client.archive(artifact.id)
  const evidence=parseArchive(bytes,artifact.digest)
  // Strict schema: reject ambiguous extra content as well as missing metadata.
  assert.deepEqual(Object.keys(evidence).filter(key=>key!=='subject').sort(),['sha','workflow_sha','package_sha256','lock_sha256','outcome','informational_only','repository','pr','base_sha','run_id','run_attempt','coverage'].sort())
  validateBridge({repository,owner:repo.owner.login,pr,currentBase,trustedWorkflowSha,run,jobs,artifact,evidence,subject})
  if(requirePreview) {
    assert.equal(subject,'develop','Deployment evidence is develop-only')
    for(const name of ['Reviewed complete backend checks','Deploy / preview-pull','Deploy / preview']) {
      const matches=jobs.filter(job=>job.name===name)
      assert.equal(matches.length,1,'Missing or duplicated protected deployment job')
      const job=matches[0]
      assert.equal(job.run_id,run.id);assert.equal(job.run_attempt,run.run_attempt)
      assert.equal(job.status,'completed');assert.equal(job.conclusion,'success','Protected deployment/checks not successful')
    }
  }
  // Bind reported manifest hashes to the target's actual inert regular blobs.
  const commit=await client.json(`git/commits/${targetSha}`)
  sha(commit.tree?.sha)
  const tree=await client.json(`git/trees/${commit.tree.sha}?recursive=1`)
  const entries=manifestEntries(tree)
  const blobs=[]
  for(const entry of entries) {
    const blob=await client.json(`git/blobs/${entry.sha}`)
    assert.equal(blob.encoding,'base64');assert.ok(typeof blob.content==='string')
    blobs.push(Buffer.from(blob.content,'base64'))
  }
  const hashes=validateManifests(...blobs)
  assert.equal(hashes.package_sha256,evidence.package_sha256);assert.equal(hashes.lock_sha256,evidence.lock_sha256)
  // Close head/base/pin/run/artifact races before returning any success.
  const freshPr=subject==='pr' ? await client.json(`pulls/${prNumber}`) : null
  const freshBase=await client.json(`branches/${branch}`),freshMain=await client.json('branches/main')
  if(subject==='pr') {
    assert.equal(freshPr.state,'open');assert.equal(freshPr.head.sha,targetSha);assert.equal(freshPr.head.repo?.full_name,repository)
    assert.equal(freshPr.base.ref,pr.base.ref);assert.equal(freshPr.base.repo?.full_name,repository)
  }
  assert.equal(freshBase.commit.sha,currentBase.commit.sha);assert.equal(freshMain.commit.sha,trustedWorkflowSha)
  const freshRun=await client.json(`actions/runs/${run.id}`)
  assert.equal(freshRun.run_attempt,run.run_attempt);assert.equal(freshRun.status,'completed');assert.equal(freshRun.conclusion,'success')
  const latest=await selectVerified(client,await list(client,'actions/workflows/framebriar-reviewed-ci.yml/runs?event=workflow_dispatch&branch=main','workflow_runs'),prNumber)
  assert.equal(latest.run.id,run.id);assert.equal(latest.run.run_attempt,run.run_attempt)
  const freshArtifacts=await list(client,`actions/runs/${run.id}/artifacts`,'artifacts')
  const fresh=freshArtifacts.filter(item=>item.name===name)
  assert.equal(fresh.length,1);assert.equal(fresh[0].id,artifact.id);assert.equal(fresh[0].digest,artifact.digest);assert.equal(fresh[0].expired,false)
  return {subject,sha:targetSha,workflow_sha:trustedWorkflowSha,run_id:run.id,run_attempt:run.run_attempt,artifact_id:artifact.id}
}
if(process.argv[1] && fileURLToPath(import.meta.url)===process.argv[1]) {
  if(process.env.PREBUILD_REQUIRED==='1') {
    console.error('::error::Private Preview export is not implemented')
    process.exit(1)
  }
  try {
    const result=await verifyEvidence({repository:process.env.GITHUB_REPOSITORY,subject:process.env.SUBJECT ?? 'pr',prNumber:process.env.SUBJECT==='develop' ? 0 : Number(process.env.PR_NUMBER),targetSha:process.env.TARGET_SHA,trustedWorkflowSha:process.env.TRUSTED_WORKFLOW_SHA,token:process.env.GH_TOKEN,requirePreview:process.env.REQUIRE_PREVIEW==='1'})
    console.log(`Verified exact-SHA frontend evidence for ${result.sha}; trusted run ${result.run_id}, attempt ${result.run_attempt}.`)
  } catch {
    // Never print tokens, signed URLs, API bodies, ZIP bytes or assertion values.
    console.error('::error::No acceptable exact-SHA frontend evidence. Verify the explicit trusted revision, current PR/base, latest reviewed run and clean immutable reporter artifact. No older-success fallback is allowed.')
    process.exitCode=1
  }
}
