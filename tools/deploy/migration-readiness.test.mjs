import {test} from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import {canonical,digest,inventory,migrations,relations,createReadiness,verifyReadiness,verifyObservation,parseCanonicalRecord} from './migration-readiness.mjs'
const clone=x=>JSON.parse(JSON.stringify(x))
const now=1800000000000
function fixture(){
 const files=Object.values(migrations).sort().map(p=>({path:p,sha256:'a'.repeat(64)})), inv={files,sha256:digest(canonical(files))}
 const observations={}
 for(const c of Object.keys(migrations)) observations[c]={serverMajor:18,databaseName:'preview',runtimeRole:c+'_runtime',databasePrivileges:{CONNECT:true,CREATE:false,TEMPORARY:true},databaseGrantOptions:{CONNECT:false,CREATE:false,TEMPORARY:false},unrelatedAccess:[],securityDefinerAccess:[],schemaCreateAccess:[],role:{superuser:false,createRole:false,createDatabase:false,replication:false,bypassRls:false,login:true,memberships:[],adminOptions:[]},relations:relations[c].map(name=>({name,kind:'r',ownerIsRuntime:false,rowSecurity:false,forceRowSecurity:false,triggers:[],rules:[],policies:[],columns:[{name:'id',type:'text',grantOptions:{SELECT:false,INSERT:false,UPDATE:false,REFERENCES:false},privileges:{SELECT:true,INSERT:true,UPDATE:true,REFERENCES:false},publicPrivileges:{SELECT:false,INSERT:false,UPDATE:false,REFERENCES:false}}],constraints:[],indexes:[],privileges:{SELECT:true,INSERT:true,UPDATE:true,DELETE:false,TRUNCATE:false,REFERENCES:false,TRIGGER:false,MAINTAIN:false},grantOptions:{SELECT:false,INSERT:false,UPDATE:false,DELETE:false,TRUNCATE:false,REFERENCES:false,TRIGGER:false,MAINTAIN:false},schemaGrantOptions:{USAGE:false,CREATE:false},schemaPrivileges:{USAGE:true,CREATE:false},publicPrivileges:{SELECT:false,INSERT:false,UPDATE:false,DELETE:false,TRUNCATE:false,REFERENCES:false,TRIGGER:false,MAINTAIN:false},schemaPublicPrivileges:{CREATE:false}}))}
 const plan={version:1,target:'preview',projectId:'prj_test',databaseBranchId:'br_test',sourceSha:'b'.repeat(40),inventorySha256:inv.sha256,policySha256:'c'.repeat(64),feedback:true,draft:true,components:{}}
 for(const c of Object.keys(observations)) plan.components[c]={databaseName:'preview',runtimeRole:c+'_runtime',expectedSnapshotSha256:digest(canonical(observations[c]))}
 const provider={projectId:plan.projectId,databaseBranchId:plan.databaseBranchId}
 return {plan,observations,provider,inv}
}
test('positive inspection and immutable readiness; all enabled components',()=>{
 const {plan,observations,provider,inv}=fixture(); const r=createReadiness(plan,observations,provider,now)
 assert.equal(verifyReadiness(r,plan,digest(canonical(r)),inv,now+1).target,'preview')
})
test('disabled feedback/draft need no observation, but full inventory remains bound',()=>{
 const f=fixture(); f.plan.feedback=false;f.plan.draft=false;delete f.plan.components.feedback;delete f.plan.components.draft;delete f.observations.feedback;delete f.observations.draft
 const r=createReadiness(f.plan,f.observations,f.provider,now);assert.deepEqual(Object.keys(r.snapshots),['recipes'])
 assert.equal(verifyReadiness(r,f.plan,digest(canonical(r)),f.inv,now+1).target,'preview')
 const altered=clone(f.inv);altered.files[0].sha256='d'.repeat(64);assert.throws(()=>verifyReadiness(r,f.plan,digest(canonical(r)),altered,now+1))
})
for(const [name,mutate] of [
 ['unsupported server',o=>o.serverMajor=19],['MAINTAIN',o=>o.relations[0].privileges.MAINTAIN=true],['MAINTAIN grant option',o=>o.relations[0].grantOptions.MAINTAIN=true],['wrong database',o=>o.databaseName='production'],['wrong role',o=>o.runtimeRole='owner'],['superuser',o=>o.role.superuser=true],['role membership',o=>o.role.memberships=['owner']],['missing table',o=>o.relations.pop()],['table ownership',o=>o.relations[0].ownerIsRuntime=true],['DELETE',o=>o.relations[0].privileges.DELETE=true],['PUBLIC SELECT',o=>o.relations[0].publicPrivileges.SELECT=true],['schema CREATE',o=>o.relations[0].schemaPrivileges.CREATE=true],['schema drift',o=>o.relations[0].columns[0].type='integer'],['constraint drift',o=>o.relations[0].constraints.push('drift')],['extra assertion',o=>o.approved=true]
])test(`reject ${name}`,()=>{const f=fixture();mutate(f.observations.recipes);assert.throws(()=>verifyObservation('recipes',f.observations.recipes,f.plan.components.recipes))})
for(const [name,mutate] of [['source',r=>r.sourceSha='d'.repeat(40)],['target',r=>r.target='production'],['branch',r=>r.databaseBranchId='br_other'],['inventory',r=>r.inventorySha256='d'.repeat(64)],['policy',r=>r.policySha256='d'.repeat(64)],['future',r=>r.issuedAtMs=now+5],['long expiry',r=>r.expiresAtMs=now+3600000],['missing schema',r=>delete r.snapshots.recipes]])test(`reject record ${name} even with recomputed digest`,()=>{const f=fixture();const r=createReadiness(f.plan,f.observations,f.provider,now);mutate(r);assert.throws(()=>verifyReadiness(r,f.plan,digest(canonical(r)),f.inv,now+1))})
test('reject untrusted digest, expiry, wrong provider',()=>{const f=fixture();const r=createReadiness(f.plan,f.observations,f.provider,now);assert.throws(()=>verifyReadiness(r,f.plan,'f'.repeat(64),f.inv,now+1));assert.throws(()=>verifyReadiness(r,f.plan,digest(canonical(r)),f.inv,now+900000));assert.throws(()=>createReadiness(f.plan,f.observations,{...f.provider,databaseBranchId:'other'},now))})
test('inventory exact, checksum bound, rejects unknown/missing/link sources',()=>{
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'readiness-'))
 try {
 for(const p of Object.values(migrations)){fs.mkdirSync(path.dirname(path.join(root,p)),{recursive:true});fs.writeFileSync(path.join(root,p),'-- synthetic\n')}
 const first=inventory(root);fs.appendFileSync(path.join(root,migrations.feedback),'-- changed\n');assert.notEqual(inventory(root).sha256,first.sha256)
 fs.writeFileSync(path.join(root,'migrations/999_unknown.sql'),'x');assert.throws(()=>inventory(root));fs.unlinkSync(path.join(root,'migrations/999_unknown.sql'))
 fs.unlinkSync(path.join(root,migrations.feedback));assert.throws(()=>inventory(root));fs.symlinkSync(path.join(root,migrations.recipes),path.join(root,migrations.feedback));assert.throws(()=>inventory(root))
 }finally{fs.rmSync(root,{recursive:true,force:true})}
})
test('default guard remains closed; inspector read-only and no target SQL',()=>{
 const sql=fs.readFileSync('tools/deploy/inspect-migration-readiness.sql','utf8');assert.match(sql,/BEGIN READ ONLY;/);assert.match(sql,/ROLLBACK;/);assert.doesNotMatch(sql,/^\s*(?:\\i\b|ALTER |GRANT |INSERT |UPDATE |DELETE )/m)
 const guard=fs.readFileSync('tools/deploy/check-migration-readiness.sh','utf8');assert.match(guard,/do not bypass this guard/);assert.doesNotMatch(guard,/migration-readiness.mjs/)
})

test('canonical record parsing rejects ambiguity and oversize',()=>{assert.deepEqual(parseCanonicalRecord('{"a":1}'),{a:1});for(const raw of ['{"a":1,"a":1}','{ "a":1}','{"a":1}x','x'.repeat(131073)])assert.throws(()=>parseCanonicalRecord(raw))})
