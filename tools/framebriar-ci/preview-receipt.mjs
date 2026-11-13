// Pure, inert Preview receipt policy. No network, filesystem, credentials,
// artifact download, extraction, upload or deployment. Caller-supplied evidence
// is authoritative ONLY after retrieval/authentication by a separately reviewed
// GitHub adapter. A receipt, digest or successful return is not deployment approval.
const MAX_BYTES=32768
const SHA=/^[a-f0-9]{40}$/
const DIGEST=/^[a-f0-9]{64}$/
const fail=message=>{throw new Error(message)}
const object=value=>value!==null&&typeof value==='object'&&!Array.isArray(value)&&[Object.prototype,null].includes(Object.getPrototypeOf(value))
const exact=(value,keys,label)=>{if(!object(value)||Object.keys(value).sort().join('\0')!==[...keys].sort().join('\0'))fail(`${label} fields`)}
const positive=value=>Number.isSafeInteger(value)&&value>0
const sha=value=>typeof value==='string'&&SHA.test(value)
const digest=value=>typeof value==='string'&&DIGEST.test(value)
const timestamp=value=>typeof value==='string'&&/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/.test(value)&&Number.isFinite(Date.parse(value))&&new Date(Date.parse(value)).toISOString()===value.replace('Z','.000Z')
export const producerWorkflowPath='.github/workflows/framebriar-preview-producer.yml'
export const reporterJobName='clean-preview-reporter'
export const outputArtifactName='framebriar-preview-output-v1'
export const bindingKeys=Object.freeze([
 'repositoryId','repository','sourceSha','baseSha','baseRef','manifestSha256','lockSha256',
 'workflowPath','workflowSha','workflowId','runId','runAttempt','actorId',
 'projectId','target','origin','appId','enabled','screenshots',
 'vercelVersion','goVersion','goBuilderVersion','staticBuilderVersion',
 'readinessId','readinessSha256','migrationSha256','schemaPolicySha256','routesSha256',
])
const receiptKeys=[...bindingKeys,'version','reporterJobId','artifactId','artifactName','artifactDigest','archiveSha256','inventorySha256','fileCount','byteCount','issuedAt','expiresAt']

// A small bounded JSON parser rejects ambiguity BEFORE JSON.parse could discard
// duplicate keys, including escaped-equivalent and nested keys. No eval.
export function parseReceipt(raw){
 if(typeof raw!=='string'||Buffer.byteLength(raw,'utf8')>MAX_BYTES)fail('receipt byte bound')
 let i=0
 const space=()=>{while(/[\x20\t\r\n]/.test(raw[i]??'x'))i++}
 function string(){
  const start=i
  if(raw[i++]!=='"')fail('JSON string')
  while(i<raw.length){const c=raw[i++];if(c==='"'){let v;try{v=JSON.parse(raw.slice(start,i))}catch{fail('JSON string')};if(/[\uD800-\uDFFF]/u.test(v))fail('unpaired surrogate');return v}if(c==='\\'){if(i>=raw.length)fail('JSON escape');i++}else if(c<' ')fail('JSON control')}
  fail('JSON unterminated string')
 }
 function value(depth=0){
  if(depth>8)fail('JSON nesting bound');space()
  if(raw[i]==='"')return string()
  if(raw[i]==='{'){
   i++;space();const result=Object.create(null);if(raw[i]==='}'){i++;return result}
   while(true){space();const key=string();if(Object.hasOwn(result,key))fail('duplicate JSON key');space();if(raw[i++]!==':')fail('JSON colon');result[key]=value(depth+1);space();const next=raw[i++];if(next==='}')return result;if(next!==',')fail('JSON object separator')}
  }
  if(raw[i]==='['){i++;space();const result=[];if(raw[i]===']'){i++;return result}while(true){result.push(value(depth+1));space();const next=raw[i++];if(next===']')return result;if(next!==',')fail('JSON array separator')}}
  for(const [literal,v]of [['true',true],['false',false],['null',null]])if(raw.startsWith(literal,i)){i+=literal.length;return v}
  const match=/^-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?/.exec(raw.slice(i));if(!match)fail('JSON value');i+=match[0].length;const number=Number(match[0]);if(!Number.isFinite(number))fail('JSON finite number');return number
 }
 const result=value();space();if(i!==raw.length)fail('JSON trailing data');validateReceiptShape(result);return result
}

function validateBindings(v){
 for(const key of ['repositoryId','workflowId','runId','runAttempt','actorId'])if(!positive(v[key]))fail('binding identity')
 if(typeof v.repository!=='string'||!/^[-A-Za-z0-9_.]+\/[-A-Za-z0-9_.]+$/.test(v.repository)||v.repository.length>200)fail('repository name')
 for(const key of ['sourceSha','baseSha','workflowSha'])if(!sha(v[key]))fail('binding SHA')
 for(const key of ['manifestSha256','lockSha256','readinessSha256','migrationSha256','schemaPolicySha256','routesSha256'])if(!digest(v[key]))fail('binding digest')
 if(v.baseRef!=='develop'||v.workflowPath!==producerWorkflowPath||v.target!=='preview'||v.appId!=='meal-planner-preview')fail('Preview scope')
 if(typeof v.projectId!=='string'||!/^prj_[A-Za-z0-9]{1,100}$/.test(v.projectId))fail('project identity')
 if(typeof v.origin!=='string'||v.origin.length>253)fail('origin bound')
 let url;try{url=new URL(v.origin)}catch{fail('origin URL')}
 if(url.protocol!=='https:'||url.origin!==v.origin||url.username||url.password||url.hostname.includes('*')||url.hostname==='localhost'||url.hostname.endsWith('.localhost')||url.hostname==='127.0.0.1'||url.hostname==='[::1]')fail('exact HTTPS origin')
 if(typeof v.enabled!=='boolean'||typeof v.screenshots!=='boolean'||(!v.enabled&&v.screenshots))fail('feature flags')
 if(v.vercelVersion!=='65.9.0')fail('CLI version')
 for(const key of ['goVersion','goBuilderVersion','staticBuilderVersion'])if(typeof v[key]!=='string'||!/^\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(v[key]))fail('resolved toolchain version')
 if(typeof v.readinessId!=='string'||!/^[-A-Za-z0-9_]{1,100}$/.test(v.readinessId))fail('readiness identity')
}
export function validateReceiptShape(v){
 exact(v,receiptKeys,'receipt');validateBindings(v)
 if(v.version!==1)fail('receipt version')
 for(const key of ['reporterJobId','artifactId','fileCount','byteCount'])if(!positive(v[key]))fail('receipt identity/count')
 if(v.fileCount>4096||v.byteCount>128*1024*1024)fail('artifact bound')
 if(v.artifactName!==outputArtifactName||typeof v.artifactDigest!=='string'||!/^sha256:[a-f0-9]{64}$/.test(v.artifactDigest))fail('artifact identity/digest')
 if(!digest(v.archiveSha256)||!digest(v.inventorySha256))fail('content digest')
 if(!timestamp(v.issuedAt)||!timestamp(v.expiresAt)||Date.parse(v.expiresAt)<=Date.parse(v.issuedAt)||Date.parse(v.expiresAt)-Date.parse(v.issuedAt)>3600000)fail('receipt validity window')
 return v
}

/** Verify against independent approved plan and authenticated GitHub snapshots.
 * The future adapter must fetch latest run/attempt, complete jobs/artifacts,
 * and current protected refs, and authenticate the separate RECEIPT envelope
 * before this call. artifact refers to OUTPUT, not the receipt artifact.
 * Never derive `plan` or `evidence` from receipt.
 * Returns only immutable public identities, not a deploy permit.
 */
export function verifyReceipt(receipt,plan,evidence,nowMs){
 validateReceiptShape(receipt)
 exact(plan,[...bindingKeys,'maxAgeMs'],'plan');validateBindings(plan)
 if(!positive(plan.maxAgeMs)||plan.maxAgeMs>3600000||!Number.isSafeInteger(nowMs)||nowMs<=0)fail('trusted clock/age policy')
 for(const key of bindingKeys)if(receipt[key]!==plan[key])fail(`approved binding mismatch: ${key}`)
 exact(evidence,['repository','refs','run','latestRun','reporter','artifact','content','readiness','inventoryComplete'],'evidence')
 if(evidence.inventoryComplete!==true)fail('incomplete evidence inventory')
 exact(evidence.repository,['id','fullName'],'repository evidence')
 if(evidence.repository.id!==plan.repositoryId||evidence.repository.fullName!==plan.repository)fail('repository evidence mismatch')
 exact(evidence.refs,['mainSha','developSha','sourceContainsBase'],'ref evidence')
 if(evidence.refs.mainSha!==plan.workflowSha||evidence.refs.developSha!==plan.baseSha||evidence.refs.sourceContainsBase!==true)fail('protected ref/base moved')
 const runKeys=['id','attempt','workflowId','workflowPath','repositoryId','headSha','headBranch','event','actorId','triggeringActorId','status','conclusion']
 exact(evidence.run,runKeys,'run evidence');exact(evidence.latestRun,runKeys,'latest run evidence')
 const run=evidence.run
 if(run.id!==plan.runId||run.attempt!==plan.runAttempt||run.workflowId!==plan.workflowId||run.workflowPath!==plan.workflowPath||run.repositoryId!==plan.repositoryId||run.headSha!==plan.workflowSha||run.headBranch!=='main'||run.event!=='workflow_dispatch'||run.actorId!==plan.actorId||run.triggeringActorId!==plan.actorId||run.status!=='completed'||run.conclusion!=='success')fail('producer evidence mismatch')
 for(const key of runKeys)if(evidence.latestRun[key]!==run[key])fail('newer producer run or attempt exists')
 const job=evidence.reporter
 exact(job,['id','runId','attempt','name','status','conclusion','startedAt','completedAt','artifactId','artifactDigest','archiveSha256','inventorySha256'],'reporter evidence')
 if(job.id!==receipt.reporterJobId||job.runId!==run.id||job.attempt!==run.attempt||job.name!==reporterJobName||job.status!=='completed'||job.conclusion!=='success'||!timestamp(job.startedAt)||!timestamp(job.completedAt)||Date.parse(job.startedAt)>Date.parse(job.completedAt))fail('clean reporter evidence mismatch')
 // These outputs require authenticated trusted-reporter attribution; they are
 // NOT ordinary GitHub job REST fields. Timestamp overlap is insufficient.
 if(job.artifactId!==receipt.artifactId||job.artifactDigest!==receipt.artifactDigest||job.archiveSha256!==receipt.archiveSha256||job.inventorySha256!==receipt.inventorySha256)fail('trusted reporter output mismatch')
 const readiness=evidence.readiness
 exact(readiness,['id','sha256','migrationSha256','target','projectId','sourceSha','policySha256'],'readiness evidence')
 if(readiness.id!==plan.readinessId||readiness.sha256!==plan.readinessSha256||readiness.migrationSha256!==plan.migrationSha256||readiness.target!=='preview'||readiness.projectId!==plan.projectId||readiness.sourceSha!==plan.sourceSha||readiness.policySha256!==plan.schemaPolicySha256)fail('authenticated readiness mismatch')
 const content=evidence.content
 exact(content,['archiveSha256','inventorySha256','fileCount','byteCount'],'observed content evidence')
 for(const key of ['archiveSha256','inventorySha256','fileCount','byteCount'])if(content[key]!==receipt[key])fail('downloaded content mismatch')
 const artifact=evidence.artifact
 exact(artifact,['id','name','digest','runId','expired','createdAt','sizeInBytes'],'artifact evidence')
 if(artifact.id!==receipt.artifactId||artifact.name!==receipt.artifactName||artifact.digest!==receipt.artifactDigest||artifact.runId!==run.id||artifact.expired!==false||!timestamp(artifact.createdAt)||!positive(artifact.sizeInBytes)||artifact.sizeInBytes>128*1024*1024)fail('artifact evidence mismatch')
 const start=Date.parse(job.startedAt),end=Date.parse(job.completedAt),issued=Date.parse(receipt.issuedAt),created=Date.parse(artifact.createdAt)
 if(issued<start||issued>end||created<start||created>issued||end>nowMs||nowMs<issued||nowMs>=Date.parse(receipt.expiresAt)||nowMs-issued>plan.maxAgeMs)fail('evidence chronology/expiry')
 return Object.freeze({repository:plan.repository,sourceSha:plan.sourceSha,runId:run.id,runAttempt:run.attempt,artifactId:artifact.id,artifactDigest:artifact.digest,archiveSha256:receipt.archiveSha256,inventorySha256:receipt.inventorySha256})
}
