import {test} from 'node:test'
import assert from 'node:assert/strict'
import {validateProviderMap,parseProviderMap,inspectorEnvironment,assertServerMajor} from './readiness-provider-map.mjs'
const map=()=>({version:1,target:'preview',vercelProjectId:'prj_Synthetic',neonProjectId:'synthetic-project',neonBranchId:'br-synthetic',endpointHost:'ep-synthetic.us-east-1.aws.neon.tech',databaseName:'testdb',inspectorRole:'catalog_inspector',recipeRuntimeRole:'recipe_runtime',feedbackRuntimeRole:'feedback_runtime',postgresMajor:18,feedback:true,draft:false})
const url='postgresql://catalog_inspector:FAKE%3Atest@ep-synthetic.us-east-1.aws.neon.tech/testdb?sslmode=verify-full'
test('strict independently verified provider map and matching connection',()=>{
 const m=parseProviderMap(JSON.stringify(map()));const env=inspectorEnvironment(url,m)
 assert.equal(env.PGPASSWORD,'FAKE:test');assert.equal(env.PGSSLROOTCERT,'system');assert.equal(env.PGPASSFILE,'/dev/null')
 assertServerMajor('180001\n',m)
})
for(const [name,mutate] of [
 ['production',m=>m.target='production'],['unverified major',m=>m.postgresMajor=17],['draft enabled',m=>m.draft=true],['inspector reused runtime',m=>m.inspectorRole=m.recipeRuntimeRole],['arbitrary host',m=>m.endpointHost='attacker.invalid'],['extra key',m=>m.extra=true],
])test(`map rejects ${name}`,()=>{const m=map();mutate(m);assert.throws(()=>validateProviderMap(m))})
for(const [name,bad] of [
 ['wronghost',url.replace('ep-synthetic','ep-other')],['wrongdb',url.replace('/testdb','/other')],['wrongrole',url.replace('catalog_inspector','recipe_runtime')],['sslweak',url.replace('verify-full','require')],['option',url+'&options=x'],['fragment',url+'#x'],['duplicate ssl',url+'&sslmode=verify-full'],['encoded slash',url.replace('/testdb','/testdb%2Fextra')],['malformed escape',url.replace('catalog_inspector','%xx')],['control',url.replace('FAKE%3Atest','FAKE%0Atest')],
])test(`connection rejects ${name}`,()=>assert.throws(()=>inspectorEnvironment(bad,map())))
test('duplicate and escaped map keys rejected',()=>{
 const raw=JSON.stringify(map());assert.throws(()=>parseProviderMap(raw.replace('"version":1','"version":1,"version":1')))
 assert.throws(()=>parseProviderMap(raw.replace('version','vers\\u0069on')))
})
test('actual server major cannot be asserted by connection identity',()=>assert.throws(()=>assertServerMajor('170005',map())))
