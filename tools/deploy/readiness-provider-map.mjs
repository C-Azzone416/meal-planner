// No network or provider settings. An operator independently verifies this map.
import assert from 'node:assert/strict'
const closedKeys=['version','target','vercelProjectId','neonProjectId','neonBranchId','endpointHost','databaseName','inspectorRole','recipeRuntimeRole','feedbackRuntimeRole','postgresMajor','feedback','draft']
const syntheticDraftKeys=[...closedKeys,'draftRuntimeRole']
const identifier=x=>assert.match(x??'',/^[A-Za-z_][A-Za-z0-9_-]{0,62}$/)
export function validateProviderMap(map){
 assert.ok(map&&typeof map==='object'&&!Array.isArray(map))
 assert.equal(typeof map.draft,'boolean')
 const keys=map.draft?syntheticDraftKeys:closedKeys
 assert.deepEqual(Object.keys(map).sort(),keys.toSorted())
 assert.equal(map.version,1);assert.equal(map.target,'preview')
 assert.match(map.vercelProjectId??'',/^prj_[A-Za-z0-9]+$/)
 assert.match(map.neonProjectId??'',/^[a-z0-9][a-z0-9-]{1,127}$/)
 assert.match(map.neonBranchId??'',/^br-[a-z0-9-]{1,125}$/)
 assert.match(map.endpointHost??'',/^ep-[a-z0-9-]+(?:\.[a-z0-9-]+)*\.neon\.tech$/)
 const roles=['inspectorRole','recipeRuntimeRole','feedbackRuntimeRole',...(map.draft?['draftRuntimeRole']:[])]
 for(const k of ['databaseName',...roles])identifier(map[k])
 assert.equal(new Set(roles.map(k=>map[k])).size,roles.length)
 assert.equal(map.postgresMajor,18,'Only independently tested PostgreSQL major supported')
 assert.equal(map.feedback,true,'This adapter selects enabled feedback')
 return map
}
export function parseProviderMap(raw){
 assert.equal(typeof raw,'string');assert.ok(raw.length>0&&Buffer.byteLength(raw)<=8192)
 // Every allowed key and value is restricted to unescaped ASCII below. Reject
 // escapes before parsing so an escaped duplicate cannot override a visible key.
 assert.ok(!raw.includes('\\'),'Escaped provider-map tokens are not allowed')
 const map=JSON.parse(raw);validateProviderMap(map)
 // Strict duplicate-key detection without requiring operator key ordering.
 const keys=map.draft?syntheticDraftKeys:closedKeys
 const names=[...raw.matchAll(/"([A-Za-z][A-Za-z0-9]*)"\s*:/g)].map(m=>m[1])
 assert.deepEqual(names.toSorted(),keys.toSorted(),'Unexpected, escaped or duplicate keys')
 return map
}
export function inspectorEnvironment(secret,map){
 validateProviderMap(map)
 assert.equal(typeof secret,'string');assert.ok(secret.length>0&&secret.length<8192)
 const u=new URL(secret)
 assert.equal(u.protocol,'postgresql:');assert.equal(u.hostname,map.endpointHost)
 assert.ok(!u.port||u.port==='5432');assert.equal(u.hash,'')
 assert.deepEqual([...u.searchParams.entries()],[['sslmode','verify-full']])
 const decode=x=>{const value=decodeURIComponent(x);assert.ok(!/[\x00-\x20\x7f]/.test(value));return value}
 assert.equal(decode(u.username),map.inspectorRole)
 assert.equal(decode(u.pathname.slice(1)),map.databaseName)
 assert.ok(u.password)
 const password=decodeURIComponent(u.password);assert.ok(!/[\x00\r\n]/.test(password))
 return {PGHOST:map.endpointHost,PGPORT:'5432',PGDATABASE:map.databaseName,PGUSER:map.inspectorRole,PGPASSWORD:password,PGSSLMODE:'verify-full',PGSSLROOTCERT:'system',PGPASSFILE:'/dev/null',PGCONNECT_TIMEOUT:'5'}
}
export function assertServerMajor(raw,map){
 validateProviderMap(map);assert.match(raw,/^[0-9]{6}\n?$/)
 assert.equal(Math.floor(Number(raw.trim())/10000),map.postgresMajor,'Database server version differs from reviewed policy')
}
