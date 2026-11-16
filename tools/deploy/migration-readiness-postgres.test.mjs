import {test} from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import {spawnSync} from 'node:child_process'
import {canonical,digest,migrationFiles,verifyObservation} from './migration-readiness.mjs'
import {draftRuntimeGrantSQL} from './draft-runtime-policy.mjs'
// Opt-in, disposable local cluster only. Never consumes PGHOST/DATABASE_URL.
test('real PostgreSQL inspection detects schema and effective privilege drift',{skip:!process.env.READINESS_PG_BIN},()=>{
 const bin=process.env.READINESS_PG_BIN;assert.ok(path.isAbsolute(bin))
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'readiness-pg-')),data=path.join(root,'data'),socket=path.join(root,'sock');fs.mkdirSync(socket)
 const env={PATH:process.env.PATH,HOME:process.env.HOME,LANG:'C',PGHOST:socket,PGPORT:'5432',PGDATABASE:'postgres',PGUSER:os.userInfo().username}
 const run=(cmd,args)=>{const r=spawnSync(path.join(bin,cmd),args,{env,encoding:'utf8',timeout:30000});assert.equal(r.status,0,r.stderr||r.stdout);return r.stdout.trim()}
 const sql=s=>run('psql',['-X','-qAt','-v','ON_ERROR_STOP=1','-c',s])
 let started=false
 try {
 run('initdb',['-D',data,'-A','trust','--no-locale']);run('pg_ctl',['-D',data,'-l',path.join(root,'server.log'),'-o',`-k ${socket} -c listen_addresses=''`,'-w','start']);started=true
 for(const file of migrationFiles) run('psql',['-X','-qAt','-v','ON_ERROR_STOP=1','-f',file])
 const retention=()=>run('psql',['-X','-qAt','-v','ON_ERROR_STOP=1','-v','as_of=2030-01-01T00:00:00Z','-f','tools/deploy/inspect-draft-retention-readiness.sql'])
 const emptyRetention=JSON.parse(retention());assert.equal(emptyRetention.pairings.total,0);assert.equal(emptyRetention.integrity.accepted_without_submission,0)
 const retentionOwner='retention-owner-canary'
 sql(`INSERT INTO public.draft_pairings(id,environment,issuer,user_code,poll_token_hash,code_challenge,challenge_fingerprint,client_label,status,created_at,expires_at) VALUES('10000000-0000-4000-8000-000000000001','preview','https://issuer.example.test','23456789',decode(repeat('01',32),'hex'),repeat('a',43),'1234abcd','Retention canary','pending','2029-12-31T23:40:00Z','2029-12-31T23:50:00Z');
 INSERT INTO public.draft_send_grants(id,token_hash,environment,owner_issuer,owner_subject,email_at_approval,membership_epoch,client_label,scope,created_at,expires_at) VALUES('10000000-0000-4000-8000-000000000002',decode(repeat('02',32),'hex'),'preview','https://issuer.example.test','${retentionOwner}','owner@example.test','epoch_000000000001','Retention canary','draft:propose','2029-12-24T00:00:00Z','2029-12-31T00:00:00Z');
 INSERT INTO public.draft_send_intents(environment,owner_issuer,owner_subject,id,operation_key,originating_grant_hash,request_hash,submission,retained_bytes,state,revision,created_at,expires_at) VALUES('preview','https://issuer.example.test','${retentionOwner}','10000000-0000-4000-8000-000000000003','retention-canary-01',decode(repeat('02',32),'hex'),decode(repeat('03',32),'hex'),'{}',5000,'pending',1,'2029-12-31T23:00:00Z','2029-12-31T23:30:00Z');
 INSERT INTO public.draft_reviews(environment,owner_issuer,owner_subject,draft_id,revision,state,draft_revision,updated_at) VALUES('preview','https://issuer.example.test','${retentionOwner}','10000000-0000-4000-8000-000000000004',2,'ready',1,'2029-12-31T23:00:00Z')`)
 const observedRetentionRaw=retention(),observedRetention=JSON.parse(observedRetentionRaw)
 assert.equal(observedRetention.pairings.expired_open,1);assert.equal(observedRetention.grants.expired,1);assert.equal(observedRetention.intents.expired_pending,1);assert.equal(observedRetention.reviews.ready,1)
 assert.equal(observedRetention.integrity.accepted_without_submission,0);assert.equal(observedRetention.integrity.approved_without_recipe,0)
 assert.ok(!observedRetentionRaw.includes(retentionOwner));assert.ok(!observedRetentionRaw.includes('02020202'))
 sql(`INSERT INTO public.draft_send_intents(environment,owner_issuer,owner_subject,id,operation_key,originating_grant_hash,request_hash,submission,retained_bytes,state,revision,created_at,expires_at,decided_at,draft_id,receipt_id,accepted_revision,submitted_at) VALUES('preview','https://issuer.example.test','${retentionOwner}','10000000-0000-4000-8000-000000000005','retention-canary-02',decode(repeat('04',32),'hex'),decode(repeat('05',32),'hex'),'{}',5000,'accepted',1,'2029-12-31T23:00:00Z','2029-12-31T23:30:00Z','2029-12-31T23:05:00Z','10000000-0000-4000-8000-000000000006','10000000-0000-4000-8000-000000000007',1,'2029-12-31T23:06:00Z');
 INSERT INTO public.draft_reviews(environment,owner_issuer,owner_subject,draft_id,revision,state,draft_revision,approved_recipe_id,updated_at) VALUES('preview','https://issuer.example.test','${retentionOwner}','10000000-0000-4000-8000-000000000008',2,'approved',1,'10000000-0000-4000-8000-000000000009','2029-12-31T23:10:00Z')`)
 const orphanRetentionRaw=retention(),orphanRetention=JSON.parse(orphanRetentionRaw)
 assert.equal(orphanRetention.integrity.accepted_without_submission,1);assert.equal(orphanRetention.integrity.approved_without_recipe,1)
 assert.ok(!orphanRetentionRaw.includes(retentionOwner));assert.ok(!orphanRetentionRaw.includes('10000000-0000-4000-8000-000000000005'));assert.ok(!orphanRetentionRaw.includes('04040404'))
 sql(`INSERT INTO public.draft_ingest_submissions(environment,owner_issuer,owner_subject,operation_key,request_hash,originating_grant_hash,request,response,retained_bytes,created_at) VALUES('preview','https://issuer.example.test','${retentionOwner}','10000000-0000-4000-8000-000000000005',decode(repeat('05',32),'hex'),decode(repeat('04',32),'hex'),'{}','{}',5000,'2029-12-31T23:06:00Z');
 INSERT INTO meal_planner.recipes(id,owner_issuer,owner_subject,status,origin,content) VALUES('10000000-0000-4000-8000-000000000009','https://issuer.example.test','${retentionOwner}','draft','agent','{}')`)
 const mismatchedRetention=JSON.parse(retention());assert.equal(mismatchedRetention.integrity.accepted_without_submission,1);assert.equal(mismatchedRetention.integrity.approved_without_recipe,1)
 sql(`UPDATE public.draft_ingest_submissions SET response='{"api_version":"draft-ingest-preview-v1","draft":{"id":"10000000-0000-4000-8000-000000000006"},"receipt":{"id":"10000000-0000-4000-8000-000000000007","draft_id":"10000000-0000-4000-8000-000000000006","accepted_revision":1}}' WHERE operation_key='10000000-0000-4000-8000-000000000005';
 UPDATE meal_planner.recipes SET status='approved',origin='manual' WHERE id='10000000-0000-4000-8000-000000000009'`)
 const resolvedRetention=JSON.parse(retention());assert.equal(resolvedRetention.integrity.accepted_without_submission,0);assert.equal(resolvedRetention.integrity.approved_without_recipe,0)
 const quotaOwner='quota-owner-canary'
 sql(`INSERT INTO public.draft_send_grants(id,token_hash,environment,owner_issuer,owner_subject,email_at_approval,membership_epoch,client_label,scope,created_at,expires_at) VALUES('10000000-0000-4000-8000-000000000010',decode(repeat('06',32),'hex'),'preview','https://issuer.example.test','${quotaOwner}','quota@example.test','epoch_000000000002','Quota canary','draft:propose','2029-12-24T00:00:00Z','2029-12-31T00:00:00Z');
 INSERT INTO public.draft_ingest_submissions(environment,owner_issuer,owner_subject,operation_key,request_hash,originating_grant_hash,request,response,retained_bytes,created_at) VALUES('preview','https://issuer.example.test','${quotaOwner}','quota-retention-canary',decode(repeat('07',32),'hex'),decode(repeat('06',32),'hex'),'{}','{"canary":"quota-content-canary"}',16777000,'2029-12-31T23:06:00Z')`)
 const quotaRetentionRaw=retention(),quotaRetention=JSON.parse(quotaRetentionRaw);assert.equal(quotaRetention.submissions.owners_at_retained_cap,1);assert.ok(!quotaRetentionRaw.includes(quotaOwner));assert.ok(!quotaRetentionRaw.includes('quota-content-canary'))
 sql('CREATE ROLE readiness_runtime LOGIN; GRANT USAGE ON SCHEMA meal_planner TO readiness_runtime; GRANT SELECT, INSERT, UPDATE ON meal_planner.recipes TO readiness_runtime; GRANT SELECT, INSERT ON meal_planner.recipe_creates, meal_planner.recipe_updates TO readiness_runtime; CREATE ROLE draft_runtime LOGIN;'+draftRuntimeGrantSQL('draft_runtime'))
 const observe=(component='recipes',role='readiness_runtime',user=env.PGUSER)=>JSON.parse(run('psql',['-U',user,'-X','-qAt','-v','ON_ERROR_STOP=1','-v',`component=${component}`,'-v',`runtime_role=${role}`,'-f','tools/deploy/inspect-migration-readiness.sql']))
 const first=observe();assert.equal(first.relations.length,4)
 const expected={databaseName:'postgres',runtimeRole:'readiness_runtime',expectedSnapshotSha256:digest(canonical(first))}
 verifyObservation('recipes',first,expected)
 const draft=observe('draft','draft_runtime');assert.equal(draft.relations.length,7)
 const expectedDraft={databaseName:'postgres',runtimeRole:'draft_runtime',expectedSnapshotSha256:digest(canonical(draft))}
 verifyObservation('draft',draft,expectedDraft)
 sql('CREATE ROLE catalog_inspector LOGIN; GRANT CONNECT ON DATABASE postgres TO catalog_inspector;')
 assert.deepEqual(observe('recipes','readiness_runtime','catalog_inspector'),first,'catalog-only inspector yields identical snapshot')
 assert.deepEqual(observe('draft','draft_runtime','catalog_inspector'),draft,'catalog-only inspector yields identical draft snapshot')
 assert.equal(sql("SELECT has_table_privilege('catalog_inspector','meal_planner.recipes','SELECT')"),'f')
 const failures=[
 ['GRANT MAINTAIN ON meal_planner.recipes TO readiness_runtime','REVOKE MAINTAIN ON meal_planner.recipes FROM readiness_runtime',/forbidden MAINTAIN/],
 ['GRANT MAINTAIN ON meal_planner.recipes TO readiness_runtime WITH GRANT OPTION','REVOKE MAINTAIN ON meal_planner.recipes FROM readiness_runtime',/forbidden MAINTAIN/],
 ['GRANT MAINTAIN ON meal_planner.recipes TO PUBLIC','REVOKE MAINTAIN ON meal_planner.recipes FROM PUBLIC',/forbidden MAINTAIN/],
 ['GRANT MAINTAIN ON public.framebriar_enrollments TO readiness_runtime','REVOKE MAINTAIN ON public.framebriar_enrollments FROM readiness_runtime',/unrelated database access/],
 ['GRANT SELECT ON meal_planner.recipes TO readiness_runtime WITH GRANT OPTION','REVOKE GRANT OPTION FOR SELECT ON meal_planner.recipes FROM readiness_runtime',/GRANT OPTION/],
 ['GRANT SELECT(id) ON meal_planner.recipes TO readiness_runtime WITH GRANT OPTION','REVOKE SELECT(id) ON meal_planner.recipes FROM readiness_runtime',/GRANT OPTION/],
 ['GRANT USAGE ON SCHEMA meal_planner TO readiness_runtime WITH GRANT OPTION','REVOKE GRANT OPTION FOR USAGE ON SCHEMA meal_planner FROM readiness_runtime',/GRANT OPTION/],
 ['GRANT CONNECT ON DATABASE postgres TO readiness_runtime WITH GRANT OPTION','REVOKE CONNECT ON DATABASE postgres FROM readiness_runtime',/GRANT OPTION/],
 ['GRANT CREATE ON DATABASE postgres TO readiness_runtime','REVOKE CREATE ON DATABASE postgres FROM readiness_runtime',/database CREATE/],
 ['GRANT CREATE ON SCHEMA public TO readiness_runtime','REVOKE CREATE ON SCHEMA public FROM readiness_runtime',/schema CREATE/],
 ['CREATE RULE suppress_recipe AS ON INSERT TO meal_planner.recipes DO INSTEAD NOTHING','DROP RULE suppress_recipe ON meal_planner.recipes',/rewrite rule/],
 ['CREATE ROLE admin_group; GRANT admin_group TO readiness_runtime WITH ADMIN OPTION','REVOKE admin_group FROM readiness_runtime; DROP ROLE admin_group',/ADMIN OPTION/],
 ]
 for(const [grant,revoke,pattern] of failures){sql(grant);const changed=observe('recipes','readiness_runtime','catalog_inspector');assert.notEqual(digest(canonical(changed)),expected.expectedSnapshotSha256);assert.throws(()=>verifyObservation('recipes',changed,expected),pattern);sql(revoke);verifyObservation('recipes',observe('recipes','readiness_runtime','catalog_inspector'),expected)}
 sql('GRANT DELETE ON meal_planner.recipes TO readiness_runtime');assert.throws(()=>verifyObservation('recipes',observe(),expected),/forbidden DELETE/);sql('REVOKE DELETE ON meal_planner.recipes FROM readiness_runtime')
 sql('GRANT SELECT ON public.draft_ingest_grants TO draft_runtime');assert.throws(()=>verifyObservation('draft',observe('draft','draft_runtime'),expectedDraft),/differ/);sql('REVOKE SELECT ON public.draft_ingest_grants FROM draft_runtime');verifyObservation('draft',observe('draft','draft_runtime'),expectedDraft)
 sql('GRANT DELETE ON public.draft_reviews TO draft_runtime');assert.throws(()=>verifyObservation('draft',observe('draft','draft_runtime'),expectedDraft),/forbidden DELETE/);sql('REVOKE DELETE ON public.draft_reviews FROM draft_runtime')
 sql('ALTER TABLE meal_planner.recipes ADD COLUMN unexpected text');assert.throws(()=>verifyObservation('recipes',observe(),expected),/differ/)
 sql('ALTER TABLE meal_planner.recipes DROP COLUMN unexpected');verifyObservation('recipes',observe(),expected)
 sql('CREATE ROLE privileged_group; GRANT privileged_group TO readiness_runtime');assert.throws(()=>verifyObservation('recipes',observe(),expected),/inherit/)
 }finally{if(started)run('pg_ctl',['-D',data,'-m','immediate','-w','stop']);fs.rmSync(root,{recursive:true,force:true})}
})
