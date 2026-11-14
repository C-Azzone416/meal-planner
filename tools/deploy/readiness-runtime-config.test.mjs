import {test} from 'node:test'
import assert from 'node:assert/strict'
import {expectedRuntime,inspectRuntimeConfig,runtimeDigest} from './readiness-runtime-config.mjs'
const dotenv=()=>Object.entries(expectedRuntime).map(([k,v])=>`${k}=${JSON.stringify(v)}`).join('\n')
test('returns bounded digest only, never values',()=>assert.equal(inspectRuntimeConfig(dotenv()+'\nVERCEL_OIDC_TOKEN="FAKE_SECRET"'),runtimeDigest))
for(const [name,mutate] of [
 ['missing',s=>s.replace(/^FRAMEBRIAR_ENABLED=.*\n/,'')],['disabled',s=>s.replace('ENABLED="1"','ENABLED="0"')],['plaintext database',s=>s.replace('FRAMEBRIAR_DATABASE_URL="[SENSITIVE]"','FRAMEBRIAR_DATABASE_URL="FAKE_PASSWORD"')],['production',s=>s.replace('ENVIRONMENT="preview"','ENVIRONMENT="production"')],['wrong origin',s=>s.replace('athome-meals-dev','other')],['duplicate',s=>s+'\nFRAMEBRIAR_ENABLED="1"'],['draft',s=>s+'\nMEAL_DRAFT_INGEST_ENABLED="true"'],['old SHA',s=>s+'\nFRAMEBRIAR_BUILD_SHA="'+ 'a'.repeat(40)+'"'],
])test(`runtime rejects ${name}`,()=>assert.throws(()=>inspectRuntimeConfig(mutate(dotenv()))))
