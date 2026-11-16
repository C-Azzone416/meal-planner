// Offline policy source for disposable rehearsal and reviewed readiness baselines.
// It grants no credential, creates no role, and is never run by application startup.
import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import {fileURLToPath} from 'node:url'

export const draftRuntimeRelations = Object.freeze([
 'public.draft_ingest_grants',
 'public.draft_ingest_submissions',
 'public.draft_membership_epochs',
 'public.draft_pairings',
 'public.draft_reviews',
 'public.draft_send_grants',
 'public.draft_send_intents',
])

export function draftRuntimeGrantSQL(role) {
 assert.equal(typeof role,'string')
 assert.match(role,/^[a-z][a-z0-9_]{0,62}$/,'reviewed PostgreSQL role name required')
 const q='"'+role+'"'
 const template=fs.readFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)),'draft-runtime-grants.sql'),'utf8')
 const marker=':"runtime_role"'
 assert.ok(template.split(marker).length>2,'runtime role marker required')
 const rendered=template.split(marker).join(q)
 assert.ok(!rendered.includes(marker))
 return rendered
}
