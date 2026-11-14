// Offline developer tool: disposable Unix-only PostgreSQL, exact reviewed SQL only.
// Never accepts a DSN or uses ambient PG connection variables.
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import assert from 'node:assert/strict'
import {spawnSync} from 'node:child_process'
import {fileURLToPath} from 'node:url'
import {inventory,canonical,digest,verifyObservation} from './migration-readiness.mjs'
const HERE=path.dirname(fileURLToPath(import.meta.url))
const [source,bin,out]=process.argv.slice(2)
assert.equal(process.argv.length,5);assert.ok(path.isAbsolute(source)&&path.isAbsolute(bin)&&path.isAbsolute(out))
const reviewed=JSON.parse(fs.readFileSync(path.join(HERE,'readiness-migration-hashes.json'),'utf8'))
const sourceInventory=inventory(source);assert.equal(canonical(sourceInventory),canonical(reviewed))
const root=fs.mkdtempSync(path.join(os.tmpdir(),'readiness-baseline-')),data=path.join(root,'data'),socket=path.join(root,'sock');fs.mkdirSync(socket)
const env={PATH:process.env.PATH,HOME:root,LANG:'C',PGHOST:socket,PGPORT:'5432',PGDATABASE:'postgres',PGUSER:os.userInfo().username}
const run=(name,args)=>{const r=spawnSync(path.join(bin,name),args,{env,encoding:'utf8',timeout:30000,maxBuffer:512*1024});assert.equal(r.status,0,r.stderr);return r.stdout.trim()}
const sql=text=>run('psql',['-X','-qAt','-v','ON_ERROR_STOP=1','-c',text])
let started=false
try{
 run('initdb',['-D',data,'-A','trust','--no-locale']);run('pg_ctl',['-D',data,'-l',path.join(root,'server.log'),'-o',`-k ${socket} -c listen_addresses=''`,'-w','start']);started=true
 assert.equal(Math.floor(Number(sql('SHOW server_version_num'))/10000),18)
 for(const file of reviewed.files)run('psql',['-X','-qAt','-v','ON_ERROR_STOP=1','-f',path.join(source,file.path)])
 sql(`CREATE ROLE recipe_runtime LOGIN; GRANT USAGE ON SCHEMA meal_planner TO recipe_runtime;
 GRANT SELECT,INSERT,UPDATE ON meal_planner.recipes TO recipe_runtime;
 GRANT SELECT,INSERT ON meal_planner.recipe_creates,meal_planner.recipe_updates TO recipe_runtime;
 CREATE ROLE feedback_runtime LOGIN; GRANT USAGE ON SCHEMA public TO feedback_runtime;
 GRANT SELECT,INSERT,UPDATE ON public.framebriar_enrollments TO feedback_runtime;
 CREATE ROLE catalog_inspector LOGIN; GRANT CONNECT ON DATABASE postgres TO catalog_inspector;`)
 const components={}
 for(const [component,role] of [['recipes','recipe_runtime'],['feedback','feedback_runtime']]){
  const snapshot=JSON.parse(run('psql',['-X','-qAt','-U','catalog_inspector','-v','ON_ERROR_STOP=1','-v',`component=${component}`,'-v',`runtime_role=${role}`,'-f',path.join(HERE,'inspect-migration-readiness.sql')]))
  verifyObservation(component,snapshot,{databaseName:'postgres',runtimeRole:role,expectedSnapshotSha256:digest(canonical(snapshot))})
  snapshot.databaseName='BASELINE_DATABASE';snapshot.runtimeRole='BASELINE_ROLE';components[component]=snapshot
 }
 const inspector=JSON.parse(run('psql',['-X','-qAt','-U','catalog_inspector','-v','ON_ERROR_STOP=1','-v','component=recipes','-v','runtime_role=catalog_inspector','-f',path.join(HERE,'inspect-migration-readiness.sql')]))
 verifyObservation('recipes',inspector,{databaseName:'postgres',runtimeRole:'catalog_inspector',expectedSnapshotSha256:digest(canonical(inspector))})
 inspector.databaseName='BASELINE_DATABASE';inspector.runtimeRole='BASELINE_ROLE'
 fs.writeFileSync(out,canonical({version:1,postgresMajor:18,inventory:sourceInventory,components,inspector})+'\n')
}finally{if(started)run('pg_ctl',['-D',data,'-m','immediate','-w','stop']);fs.rmSync(root,{recursive:true,force:true})}
