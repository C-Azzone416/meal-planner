import {test} from 'node:test'
import assert from 'node:assert/strict'
import {expectedRuntime,expectedSyntheticDraftRuntime,inspectRuntimeConfig,runtimeDigest,syntheticDraftRuntimeDigest} from './readiness-runtime-config.mjs'
const dotenv=(expected=expectedRuntime)=>Object.entries(expected).map(([k,v])=>`${k}=${JSON.stringify(v)}`).join('\n')
test('returns bounded digest only, never values',()=>assert.equal(inspectRuntimeConfig(dotenv()+'\nVERCEL_OIDC_TOKEN="FAKE_SECRET"'),runtimeDigest))
for(const [name,mutate] of [
 ['missing',s=>s.replace(/^FRAMEBRIAR_ENABLED=.*\n/,'')],['disabled',s=>s.replace('ENABLED="1"','ENABLED="0"')],['plaintext database',s=>s.replace('FRAMEBRIAR_DATABASE_URL="[SENSITIVE]"','FRAMEBRIAR_DATABASE_URL="FAKE_PASSWORD"')],['production',s=>s.replace('ENVIRONMENT="preview"','ENVIRONMENT="production"')],['wrong origin',s=>s.replace('athome-meals-dev','other')],['duplicate',s=>s+'\nFRAMEBRIAR_ENABLED="1"'],
 ['draft ingest',s=>s+'\nMEAL_DRAFT_INGEST_ENABLED="true"'],['draft pairing',s=>s+'\nMEAL_DRAFT_PAIRING_ENABLED="true"'],['draft workflow',s=>s+'\nMEAL_DRAFT_WORKFLOW_ENABLED="true"'],['draft membership',s=>s+'\nMEAL_DRAFT_MEMBERSHIP_EPOCH="[SENSITIVE]"'],
 ['draft database',s=>s+'\nMEAL_DRAFTS_DATABASE_URL="[SENSITIVE]"'],['unknown plural draft setting',s=>s+'\nMEAL_DRAFTS_UNKNOWN="[SENSITIVE]"'],
 ['old SHA',s=>s+'\nFRAMEBRIAR_BUILD_SHA="'+ 'a'.repeat(40)+'"'],
])test(`runtime rejects ${name}`,()=>assert.throws(()=>inspectRuntimeConfig(mutate(dotenv()))))
test('synthetic draft mode binds the complete exact runtime surface',()=>{
 const raw=dotenv(expectedSyntheticDraftRuntime)+'\nVERCEL_OIDC_TOKEN="FAKE_SECRET"'
 assert.equal(inspectRuntimeConfig(raw,{draft:true}),syntheticDraftRuntimeDigest)
 assert.notEqual(syntheticDraftRuntimeDigest,runtimeDigest)
})
for(const [name,mutate] of [
 ['missing draft database',s=>s.replace(/^MEAL_DRAFTS_DATABASE_URL=.*$/m,'')],
 ['literal draft database',s=>s.replace('MEAL_DRAFTS_DATABASE_URL="[SENSITIVE]"','MEAL_DRAFTS_DATABASE_URL="FAKE_PASSWORD"')],
 ['pairing disabled',s=>s.replace('MEAL_DRAFT_PAIRING_ENABLED="true"','MEAL_DRAFT_PAIRING_ENABLED="false"')],
 ['workflow disabled',s=>s.replace('MEAL_DRAFT_WORKFLOW_ENABLED="true"','MEAL_DRAFT_WORKFLOW_ENABLED="false"')],
 ['pairing origin drift',s=>s.replace('MEAL_DRAFT_PAIRING_ORIGIN="https://athome-meals-dev.vercel.app"','MEAL_DRAFT_PAIRING_ORIGIN="https://other.invalid"')],
 ['workflow origin drift',s=>s.replace('MEAL_DRAFT_INGEST_ORIGIN="https://athome-meals-dev.vercel.app"','MEAL_DRAFT_INGEST_ORIGIN="https://other.invalid"')],
 ['missing membership epoch',s=>s.replace(/^MEAL_DRAFT_MEMBERSHIP_EPOCH=.*$/m,'')],
 ['legacy ingest gate',s=>s+'\nMEAL_DRAFT_INGEST_ENABLED="true"'],
 ['unknown draft setting',s=>s+'\nMEAL_DRAFT_OTHER="true"'],
 ['unknown plural draft setting',s=>s+'\nMEAL_DRAFTS_UNKNOWN="[SENSITIVE]"'],
])test(`synthetic runtime rejects ${name}`,()=>assert.throws(()=>inspectRuntimeConfig(mutate(dotenv(expectedSyntheticDraftRuntime)),{draft:true})))
