import {test} from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import {spawnSync} from 'node:child_process'
import {canonical,digest,verifyObservation} from './migration-readiness.mjs'
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
 for(const file of ['hosted/migrations/001_recipes.sql','migrations/002_framebriar.sql','hosted/migrations/004_draft_ingest.sql']) run('psql',['-X','-qAt','-v','ON_ERROR_STOP=1','-f',file])
 sql('CREATE ROLE readiness_runtime LOGIN; GRANT USAGE ON SCHEMA meal_planner TO readiness_runtime; GRANT SELECT, INSERT, UPDATE ON meal_planner.recipes TO readiness_runtime; GRANT SELECT, INSERT ON meal_planner.recipe_creates, meal_planner.recipe_updates TO readiness_runtime;')
 const observe=(user=env.PGUSER)=>JSON.parse(run('psql',['-U',user,'-X','-qAt','-v','ON_ERROR_STOP=1','-v','component=recipes','-v','runtime_role=readiness_runtime','-f','tools/deploy/inspect-migration-readiness.sql']))
 const first=observe();assert.equal(first.relations.length,4)
 const expected={databaseName:'postgres',runtimeRole:'readiness_runtime',expectedSnapshotSha256:digest(canonical(first))}
 verifyObservation('recipes',first,expected)
 sql('CREATE ROLE catalog_inspector LOGIN; GRANT CONNECT ON DATABASE postgres TO catalog_inspector;')
 assert.deepEqual(observe('catalog_inspector'),first,'catalog-only inspector yields identical snapshot')
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
 for(const [grant,revoke,pattern] of failures){sql(grant);const changed=observe('catalog_inspector');assert.notEqual(digest(canonical(changed)),expected.expectedSnapshotSha256);assert.throws(()=>verifyObservation('recipes',changed,expected),pattern);sql(revoke);verifyObservation('recipes',observe('catalog_inspector'),expected)}
 sql('GRANT DELETE ON meal_planner.recipes TO readiness_runtime');assert.throws(()=>verifyObservation('recipes',observe(),expected),/forbidden DELETE/);sql('REVOKE DELETE ON meal_planner.recipes FROM readiness_runtime')
 sql('ALTER TABLE meal_planner.recipes ADD COLUMN unexpected text');assert.throws(()=>verifyObservation('recipes',observe(),expected),/differ/)
 sql('ALTER TABLE meal_planner.recipes DROP COLUMN unexpected');verifyObservation('recipes',observe(),expected)
 sql('CREATE ROLE privileged_group; GRANT privileged_group TO readiness_runtime');assert.throws(()=>verifyObservation('recipes',observe(),expected),/inherit/)
 }finally{if(started)run('pg_ctl',['-D',data,'-m','immediate','-w','stop']);fs.rmSync(root,{recursive:true,force:true})}
})
