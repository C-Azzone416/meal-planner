// Trusted-runner library only. Does not contact a DB, authenticate an observation,
// alter the default deployment guard, or make target-supplied JSON authoritative.
import fs from 'node:fs'
import path from 'node:path'
import { createHash } from 'node:crypto'
import assert from 'node:assert/strict'
export const migrations = Object.freeze({
 recipes: 'hosted/migrations/001_recipes.sql',
 feedback: 'migrations/002_framebriar.sql',
 draft: 'hosted/migrations/004_draft_ingest.sql',
})
export const relations = Object.freeze({
 recipes: ['meal_planner.recipe_creates','meal_planner.recipe_updates','meal_planner.recipes','meal_planner.schema_migrations'],
 feedback: ['public.framebriar_enrollments'],
 draft: ['public.draft_ingest_grants','public.draft_ingest_submissions'],
})
const exact = (o, keys) => { assert.ok(o && typeof o === 'object' && !Array.isArray(o)); assert.deepEqual(Object.keys(o).sort(), [...keys].sort()) }
export function canonical(value) {
 if (value === null || ['boolean','string'].includes(typeof value)) return JSON.stringify(value)
 if (typeof value === 'number') { assert.ok(Number.isSafeInteger(value)); return String(value) }
 if (Array.isArray(value)) return '[' + value.map(canonical).join(',') + ']'
 assert.ok(value && Object.getPrototypeOf(value) === Object.prototype)
 return '{' + Object.keys(value).sort().map(k => JSON.stringify(k)+':'+canonical(value[k])).join(',') + '}'
}
export const digest = bytes => createHash('sha256').update(bytes).digest('hex')
const hash = s => assert.match(s, /^[a-f0-9]{64}$/)
const sha = s => assert.match(s, /^[a-f0-9]{40}$/)
const identity = s => { assert.equal(typeof s,'string'); assert.match(s,/^[A-Za-z0-9_.-]{1,128}$/) }
export function inventory(root) {
 const found = []
 for (const dir of ['db/migrations','migrations','hosted/migrations']) {
  let base = root
  for (const part of dir.split('/')) { base = path.join(base,part); if (!fs.existsSync(base)) break; assert.ok(fs.lstatSync(base).isDirectory(), 'migration ancestor must be directory, never symlink') }
  const target = path.join(root,dir)
  if (!fs.existsSync(target)) continue
  function walk(p,relative) { for (const entry of fs.readdirSync(p).sort()) {
   const full=path.join(p,entry), name=relative+'/'+entry, stat=fs.lstatSync(full)
   if(stat.isDirectory()) walk(full,name)
   else { assert.ok(stat.isFile() && stat.nlink===1,'regular unlinked migration required'); assert.ok(stat.size>0 && stat.size<=1024*1024); found.push({path:name,sha256:digest(fs.readFileSync(full))}) }
  } }
  walk(target,dir)
 }
 found.sort((a,b)=>a.path.localeCompare(b.path,'en'))
 assert.deepEqual(found.map(x=>x.path).sort(),Object.values(migrations).sort(),'unknown/missing migration: review inventory policy')
 return {files:found,sha256:digest(canonical(found))}
}
export function validatePlan(plan) {
 exact(plan,['version','target','projectId','databaseBranchId','sourceSha','inventorySha256','policySha256','feedback','draft','components'])
 assert.equal(plan.version,1); assert.equal(plan.target,'preview'); identity(plan.projectId); identity(plan.databaseBranchId); sha(plan.sourceSha); hash(plan.inventorySha256); hash(plan.policySha256)
 assert.equal(typeof plan.feedback,'boolean'); assert.equal(typeof plan.draft,'boolean')
 const required=['recipes',...(plan.feedback?['feedback']:[]),...(plan.draft?['draft']:[])].sort()
 exact(plan.components,required)
 for(const component of required) {
  const p=plan.components[component]; exact(p,['databaseName','runtimeRole','expectedSnapshotSha256']); identity(p.databaseName); identity(p.runtimeRole); hash(p.expectedSnapshotSha256)
 }
 return required
}
// Called only with an observation obtained directly by the clean trusted inspector.
// Provider branch identity is authenticated independently: SQL current_database()
// is NOT a Neon branch ID. Exact reviewed schema/grants snapshot is mandatory.
export function verifyObservation(component, observation, expected) {
 assert.ok(Object.hasOwn(relations,component)); exact(observation,['serverMajor','databaseName','runtimeRole','role','relations','unrelatedAccess','securityDefinerAccess','schemaCreateAccess','databasePrivileges','databaseGrantOptions'])
 assert.ok([17,18].includes(observation.serverMajor),'unsupported PostgreSQL major');
 assert.deepEqual(observation.schemaCreateAccess,[],'schema CREATE access');
 assert.deepEqual(observation.unrelatedAccess,[],'unrelated database access'); assert.deepEqual(observation.securityDefinerAccess,[],'security-definer function access');
 assert.equal(observation.databaseName,expected.databaseName); assert.equal(observation.runtimeRole,expected.runtimeRole)
 exact(observation.role,['superuser','createRole','createDatabase','replication','bypassRls','login','memberships','adminOptions'])
 for(const key of ['superuser','createRole','createDatabase','replication','bypassRls']) assert.equal(observation.role[key],false,`unsafe role: ${key}`)
 assert.deepEqual(observation.role.adminOptions,[],'role ADMIN OPTION');
 exact(observation.databasePrivileges,['CONNECT','CREATE','TEMPORARY']); exact(observation.databaseGrantOptions,['CONNECT','CREATE','TEMPORARY']);
 assert.equal(observation.databasePrivileges.CONNECT,true); assert.equal(observation.databasePrivileges.CREATE,false,'database CREATE'); for(const v of Object.values(observation.databaseGrantOptions)) assert.equal(v,false,'database GRANT OPTION');
 assert.equal(observation.role.login,true); assert.deepEqual(observation.role.memberships,[],'runtime role must not inherit/SET ROLE into other roles')
 assert.ok(Array.isArray(observation.relations)); assert.deepEqual(observation.relations.map(r=>r.name),relations[component])
 for(const r of observation.relations) {
  exact(r,['name','kind','ownerIsRuntime','rowSecurity','forceRowSecurity','triggers','rules','policies','columns','constraints','indexes','privileges','grantOptions','schemaGrantOptions','schemaPrivileges','publicPrivileges','schemaPublicPrivileges'])
  assert.deepEqual(r.rules,[],'unreviewed rewrite rule'); assert.deepEqual(r.triggers,[],'unreviewed trigger'); assert.ok(Array.isArray(r.policies)); assert.equal(typeof r.forceRowSecurity,'boolean');
  assert.equal(r.kind,'r'); assert.equal(r.ownerIsRuntime,false)
  assert.ok(Array.isArray(r.columns)&&r.columns.length>0); for(const column of r.columns) { exact(column.grantOptions,['SELECT','INSERT','UPDATE','REFERENCES']); for(const v of Object.values(column.grantOptions)) assert.equal(v,false,'column GRANT OPTION'); assert.ok(column.publicPrivileges); for(const v of Object.values(column.publicPrivileges)) assert.equal(v,false,'PUBLIC column access'); assert.equal(column.privileges.REFERENCES,false,'forbidden column REFERENCES') } assert.ok(Array.isArray(r.constraints)); assert.ok(Array.isArray(r.indexes))
  for(const key of ['DELETE','TRUNCATE','REFERENCES','TRIGGER','MAINTAIN']) assert.equal(r.privileges[key],false,`forbidden ${key}`)
  exact(r.grantOptions,['SELECT','INSERT','UPDATE','DELETE','TRUNCATE','REFERENCES','TRIGGER','MAINTAIN']); exact(r.schemaGrantOptions,['USAGE','CREATE']); for(const v of Object.values(r.grantOptions)) assert.equal(v,false,'table GRANT OPTION'); for(const v of Object.values(r.schemaGrantOptions)) assert.equal(v,false,'schema GRANT OPTION');
  assert.equal(r.schemaPrivileges.CREATE,false)
  for(const v of Object.values(r.publicPrivileges)) assert.equal(v,false,'PUBLIC table access')
  // PUBLIC schema USAGE is not table access; CREATE must remain denied.
  assert.equal(r.schemaPublicPrivileges.CREATE,false)
 }
 assert.equal(digest(canonical(observation)),expected.expectedSnapshotSha256,'schema/effective grants differ from reviewed policy snapshot')
 return digest(canonical(observation))
}
// Produces bounded evidence only after the trusted caller has independently
// authenticated provider identity and directly inspected each selected DB.
export function createReadiness(plan, observations, providerIdentity, nowMs) {
 const components=validatePlan(plan); exact(providerIdentity,['projectId','databaseBranchId']); assert.equal(providerIdentity.projectId,plan.projectId); assert.equal(providerIdentity.databaseBranchId,plan.databaseBranchId)
 exact(observations,components); assert.ok(Number.isSafeInteger(nowMs)&&nowMs>0)
 const snapshots={}; for(const c of components) snapshots[c]=verifyObservation(c,observations[c],plan.components[c])
 return {version:1,planSha256:digest(canonical(plan)),sourceSha:plan.sourceSha,inventorySha256:plan.inventorySha256,policySha256:plan.policySha256,target:'preview',projectId:plan.projectId,databaseBranchId:plan.databaseBranchId,snapshots,issuedAtMs:nowMs,expiresAtMs:nowMs+15*60*1000}
}
// expectedRecordSha256 comes from authenticated trusted-inspector output, never
// the producer/record itself. Caller rechecks provider/runtime configuration at
// deployment; this verifier cannot detect later schema or secret rotation.
export function verifyReadiness(record, plan, expectedRecordSha256, observedInventory, nowMs) {
 const components=validatePlan(plan); hash(expectedRecordSha256)
 exact(record,['version','planSha256','sourceSha','inventorySha256','policySha256','target','projectId','databaseBranchId','snapshots','issuedAtMs','expiresAtMs'])
 assert.equal(digest(canonical(record)),expectedRecordSha256,'unauthenticated readiness record')
 assert.equal(record.version,1); assert.equal(record.planSha256,digest(canonical(plan)))
 for(const k of ['sourceSha','inventorySha256','policySha256','target','projectId','databaseBranchId']) assert.equal(record[k],plan[k],`readiness mismatch: ${k}`)
 assert.equal(observedInventory.sha256,plan.inventorySha256); assert.equal(digest(canonical(observedInventory.files)),observedInventory.sha256)
 exact(record.snapshots,components); for(const c of components) assert.equal(record.snapshots[c],plan.components[c].expectedSnapshotSha256)
 assert.ok(Number.isSafeInteger(nowMs)&&Number.isSafeInteger(record.issuedAtMs)&&Number.isSafeInteger(record.expiresAtMs))
 assert.ok(record.issuedAtMs>0&&record.issuedAtMs<=nowMs&&record.expiresAtMs>nowMs&&record.expiresAtMs-record.issuedAtMs===15*60*1000,'expired/future readiness')
 return Object.freeze({sourceSha:record.sourceSha,sha256:expectedRecordSha256,target:'preview',projectId:record.projectId,databaseBranchId:record.databaseBranchId})
}

export function parseCanonicalRecord(raw) {
 assert.equal(typeof raw,'string'); assert.ok(Buffer.byteLength(raw)>0 && Buffer.byteLength(raw)<=128*1024,'record byte bound')
 const value=JSON.parse(raw)
 // Canonical equality rejects duplicate keys, alternate key encoding, trailing
 // input and noncanonical numeric ambiguity instead of silently dropping them.
 assert.equal(raw,canonical(value),'noncanonical/ambiguous readiness JSON')
 return value
}
