import { test } from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import { validateSource, validateEnvironment, validateManifests, validateResults, validateBridge, workflowPath, manifestEntries } from './policy.mjs'
const mainSha='a'.repeat(40), headSha='b'.repeat(40), baseSha='c'.repeat(40), hash='d'.repeat(64)
const source=()=>({input:{sha:headSha,pr:'54',workflowSha:mainSha,approvedWorkflowSha:mainSha,repository:'owner/repo',actor:'owner',triggeringActor:'owner',event:'workflow_dispatch',ref:'refs/heads/main',workflowRef:`owner/repo/${workflowPath}@refs/heads/main`},repo:{full_name:'owner/repo',owner:{type:'User',login:'owner',id:1}},pr:{number:54,state:'open',head:{repo:{full_name:'owner/repo'},sha:headSha},base:{repo:{full_name:'owner/repo'},ref:'develop',sha:baseSha}},main:{commit:{sha:mainSha}},base:{name:'develop',commit:{sha:baseSha}},run:{event:'workflow_dispatch',head_sha:mainSha,path:workflowPath,repository:{full_name:'owner/repo'},actor:{login:'owner'}}})
const checkSource=s=>validateSource(s.input,s.repo,s.pr,s.main,s.run,s.base)
test('accept owner main dispatch for exact reviewed same-repository head',()=>assert.equal(checkSource(source()).sha,headSha))
for (const [name,mutate] of [
  ['fork',s=>s.pr.head.repo.full_name='fork/repo'],
  ['moved head',s=>s.pr.head.sha='e'.repeat(40)],
  ['moved main',s=>s.main.commit.sha='e'.repeat(40)],
  ['unapproved workflow revision',s=>s.input.approvedWorkflowSha='e'.repeat(40)],
  ['non-owner dispatch',s=>s.input.actor='other'],
  ['non-owner rerun',s=>s.input.triggeringActor='other'],
  ['PR-controlled workflow',s=>s.input.ref='refs/heads/feature'],
  ['wrong workflow source',s=>s.run.path='.github/workflows/ci.yml'],
  ['closed PR',s=>s.pr.state='closed'],
]) test(`reject ${name}`,()=>{const s=source();mutate(s);assert.throws(()=>checkSource(s))})
const env=()=>({environment:{name:'framebriar-ci',can_admins_bypass:false,deployment_branch_policy:{protected_branches:false,custom_branch_policies:true},protection_rules:[{type:'required_reviewers',prevent_self_review:false,reviewers:[{type:'User',reviewer:{id:1}}]}]},branches:{total_count:1,branch_policies:[{name:'main',type:'branch'}]}})
test('owner self-approval is intentional; broad branch/admin bypass rejected',()=>{let x=env();validateEnvironment(x.environment,x.branches,1);x.environment.can_admins_bypass=true;assert.throws(()=>validateEnvironment(x.environment,x.branches,1));x=env();x.branches.branch_policies[0].name='*';assert.throws(()=>validateEnvironment(x.environment,x.branches,1));x=env();x.environment.protection_rules=[];assert.throws(()=>validateEnvironment(x.environment,x.branches,1))})
const sdk={version:'0.4.1',resolved:'https://npm.pkg.github.com/download/@masra91/framebriar/0.4.1/9588969af1b2529a9b0f5ec756bc4f1772b1a88b',integrity:'sha512-JxyDmwlo1I8IHqCigKH/rKwOXFSI2mimzc36u+n5l5UHkSnN5U/XCwfyy/+jU8oJs/G1DsDbl+Rztm2GZjQ1kg=='}
const deps={'@masra91/framebriar':'0.4.1'}
test('manifest registry/integrity restrictions',()=>{const pkg=JSON.stringify({dependencies:deps}),lock={packages:{'':{dependencies:deps},'node_modules/@masra91/framebriar':sdk}};assert.match(validateManifests(pkg,JSON.stringify(lock)).lock_sha256,/^[a-f0-9]{64}$/);assert.throws(()=>validateManifests(pkg,JSON.stringify({packages:{...lock.packages,'node_modules/evil':{resolved:'https://attacker.example/package',integrity:'sha512-abc='}}})));assert.throws(()=>validateManifests(pkg,JSON.stringify({packages:{...lock.packages,'node_modules/link':{link:true}}})))})
test('absent/failed/skipped frontend evidence cannot pass',()=>{for(const frontend of [undefined,'failure','skipped','cancelled']) assert.throws(()=>validateResults({validate:'success',frontend},{sha:headSha,workflow_sha:mainSha,package_sha256:hash,lock_sha256:hash}))})
const bridge=()=>({repository:'owner/repo',owner:'owner',pr:source().pr,currentBase:source().base,trustedWorkflowSha:mainSha,run:{...source().run,id:123,run_attempt:1,head_branch:'main',triggering_actor:{login:'owner'},status:'completed',conclusion:'success'},jobs:['Validate owner and immutable source','Owner-approved frontend checks','Publish informational exact-SHA evidence'].map(name=>({name,status:'completed',conclusion:'success',run_id:123,run_attempt:1,started_at:'2026-11-08T10:00:00Z',completed_at:'2026-11-08T10:01:00Z'})),artifact:{name:'framebriar-evidence-123-1',expired:false,workflow_run:{id:123,head_sha:mainSha},created_at:'2026-11-08T10:00:30Z'},evidence:{repository:'owner/repo',run_id:'123',run_attempt:'1',pr:54,sha:headSha,base_sha:baseSha,workflow_sha:mainSha,outcome:'passed',informational_only:true,package_sha256:hash,lock_sha256:hash,coverage:['lint','typecheck','unit','browser-chromium-webkit','target-artifacts']}})
test('bridge accepts complete clean-reporter evidence',()=>assert.equal(validateBridge(bridge()),true))
for(const [name,mutate] of [
 ['different head',x=>x.evidence.sha='f'.repeat(40)],['different base',x=>x.currentBase.commit.sha='f'.repeat(40)],['different workflow revision',x=>x.run.head_sha=headSha],['ordinary PR run',x=>x.run.event='pull_request'],['failed overall dispatch',x=>x.run.conclusion='failure'],['missing frontend job',x=>x.jobs.splice(1,1)],['forged check name only',x=>x.jobs=[]],['target-created early artifact',x=>x.artifact.created_at='2026-11-08T09:59:00Z'],['different attempt',x=>x.artifact.name='framebriar-evidence-123-2'],['incomplete coverage',x=>x.evidence.coverage=['unit']],['expired evidence',x=>x.artifact.expired=true]
]) test(`bridge rejects ${name}`,()=>{const x=bridge();mutate(x);assert.throws(()=>validateBridge(x))})
test('workflow keeps reader in fixed install before target checkout and clean reporter isolated',()=>{
 const w=fs.readFileSync('.github/workflows/framebriar-reviewed-ci.yml','utf8')
 const reader=w.split('      - name: Isolated registry install only')[1].split('      - uses: actions/checkout@')[0]
 assert.match(reader,/env -i PATH=/);assert.match(reader,/npm ci --ignore-scripts/);assert.match(reader,/--logs-max=0/)
 assert.doesNotMatch(reader,/npm run|node .*target|bash .*target/)
 assert.equal((w.match(/secrets.FRAMEBRIAR_PACKAGE_READ_TOKEN/g)||[]).length,1)
 const report=w.split('\n  evidence:\n')[1]
 assert.doesNotMatch(report,/path: target|secrets\.|checks: write|statuses: write/)
 assert.match(report,/overwrite: false/)
 assert.doesNotMatch(w,/pull_request_target|cache: npm/)
 assert.equal((w.match(/uses: actions\/upload-artifact/g)||[]).length,3)
})

test('artifact collision cannot pass if clean reporter upload fails', () => {
 const x=bridge()
 // A target job could race the expected name, but overwrite=false makes the
 // reporter upload fail; neither its job nor the entire run can then succeed.
 x.jobs[2].conclusion='failure';x.run.conclusion='failure'
 assert.throws(()=>validateBridge(x))
})
test('main advancement requires deliberate workflow pin update and fresh dispatch', () => {
 const s=source();s.main.commit.sha='f'.repeat(40)
 assert.throws(()=>checkSource(s))
 const x=bridge();x.trustedWorkflowSha='f'.repeat(40)
 assert.throws(()=>validateBridge(x))
})

test('current base branch wins over stale PR.base.sha',()=>{
 const s=source();s.pr.base.sha='e'.repeat(40)
 assert.equal(checkSource(s).base_sha,baseSha)
 const x=bridge();x.pr.base.sha='e'.repeat(40)
 assert.equal(validateBridge(x),true)
})
test('manifest tree mode rejects symlinks and truncated inventory',()=>{
 const tree={truncated:false,tree:[{path:'web',type:'tree',mode:'040000',sha:mainSha},...['web/package.json','web/package-lock.json'].map(path=>({path,type:'blob',mode:'100644',sha:headSha}))]}
 assert.equal(manifestEntries(tree).length,2)
 tree.tree[1].mode='120000';assert.throws(()=>manifestEntries(tree))
 tree.tree[1].mode='100644';tree.truncated=true;assert.throws(()=>manifestEntries(tree))
})
test('registry ports and noncanonical private tarball paths are rejected',()=>{
 const pkg=JSON.stringify({dependencies:deps})
 for(const resolved of [sdk.resolved.replace('github.com/','github.com:8443/'), sdk.resolved+'/extra']) {
  assert.throws(()=>validateManifests(pkg,JSON.stringify({packages:{'':{dependencies:deps},'node_modules/@masra91/framebriar':{...sdk,resolved}}})))
 }
})
test('develop source binds exact merged branch head, never PR reuse',()=>{
 const s=source();s.input.subject='develop';s.input.pr='';s.base={name:'develop',commit:{sha:headSha}}
 const result=checkSource(s);assert.equal(result.subject,'develop');assert.equal(result.pr,0);assert.equal(result.base_sha,headSha)
 s.base.commit.sha=baseSha;assert.throws(()=>checkSource(s))
 s.base.commit.sha=headSha;s.input.pr='54';assert.throws(()=>checkSource(s))
})
test('private web selection requires pinned evidence and does not replace mandatory gate',()=>{
 const w=fs.readFileSync('.github/workflows/ci.yml','utf8')
 const web=w.split('\n  web:\n')[1].split('\n  hosted-go:\n')[0]
 assert.match(web,/Require exact-SHA reviewed frontend evidence/)
 assert.match(web,/vars.FRAMEBRIAR_TRUSTED_WORKFLOW_SHA/)
 assert.match(web,/node .trusted-evidence\/tools\/framebriar-ci\/bridge.mjs/)
 assert.doesNotMatch(web,/FRAMEBRIAR_PACKAGE_READ_TOKEN|NODE_AUTH_TOKEN|continue-on-error/)
 assert.match(w,/needs: \[affected, web, hosted-go, local-go, contracts, integration, preview\]/)
 assert.match(web,/No Production fallback|no Production fallback/)
})

test('prebuild migration block and existing deployment protections remain',()=>{
 const w=fs.readFileSync('.github/workflows/framebriar-reviewed-ci.yml','utf8')
 assert.match(w,/Reject stale source and independently check committed migration readiness/)
 assert.match(w,/READINESS_JSON:/)
 assert.match(w,/FRAMEBRIAR_ENABLED: "0"/)
 assert.match(w,/FRAMEBRIAR_SCREENSHOTS: "0"/)
 const preview=fs.readFileSync('.github/workflows/preview.yml','utf8')
 const deploy=preview.split('\n  deploy:\n')[1]
 assert.match(preview,/PREBUILD_REQUIRED: '1'/)
 assert.match(preview,/artifact-ids: \$\{\{ steps.evidence.outputs.artifact_id \}\}/)
 assert.match(deploy,/safe-extract.sh/)
 assert.match(deploy,/materialize-output.sh --check/)
 assert.match(deploy,/EXPECTED_OUTPUT_SHA256/)
 assert.match(deploy,/deploy --prebuilt --target=preview/)
 const build=preview.split('\n  build:\n')[1].split('\n  deploy:\n')[0]
 assert.doesNotMatch(build,/secrets\./)
 for(const block of build.split(/(?=^      - )/m).filter(b=>b.includes("steps.dependency.outputs.private == 'true'"))) assert.doesNotMatch(block,/npm ci|vercel@.*build/)
})

test('public Preview and rollback retain their ordinary build path',()=>{
 const workflow=fs.readFileSync('.github/workflows/preview.yml','utf8')
 const build=workflow.split('\n  build:\n')[1].split('\n  deploy:\n')[0]
 assert.match(build,/ref: \$\{\{ inputs.sha \}\}/)
 assert.match(build,/ls-tree HEAD/)
 assert.match(build,/classify-dependency.mjs .target-manifests/)
 const steps=build.split(/(?=^      - )/m)
 const ordinary=steps.filter(b=>b.includes("steps.dependency.outputs.private == 'false'"))
 assert.ok(ordinary.some(b=>b.includes('name: Vercel build (preview)')))
 assert.ok(ordinary.some(b=>b.includes('id: public-pack')))
 assert.ok(ordinary.some(b=>b.includes("hashFiles('db/migrations/**')")))
 assert.ok(ordinary.some(b=>b.includes('materialize-output.sh .')))
 assert.equal(steps.filter(b=>b.includes('name: Vercel build (preview)')&&!b.includes("steps.dependency.outputs.private == 'false'")).length,0)
 assert.equal(steps.filter(b=>b.includes('PREBUILD_REQUIRED:')&&!b.includes("steps.dependency.outputs.private == 'true'")).length,0)
})

test('reusable Preview caller grants its required read-only evidence permissions',()=>{
 const caller=fs.readFileSync('.github/workflows/ci.yml','utf8').split('\n  preview:\n')[1].split('\n  required:\n')[0]
 const callee=fs.readFileSync('.github/workflows/preview.yml','utf8')
 for(const permission of ['contents','actions','pull-requests']) {
  assert.match(caller,new RegExp(`^      ${permission}: read$`,'m'))
  assert.match(callee,new RegExp(`^ {2,6}${permission}: read$`,'m'))
 }
 assert.doesNotMatch(caller,/permissions: write-all|: write\b/)
})
