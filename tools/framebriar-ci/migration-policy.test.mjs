import {test} from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import {migrationReadiness, validateReadiness} from './migration-policy.mjs'
const sha='a'.repeat(40),treeSha='b'.repeat(40)
const entry=(path,type='blob',mode='100644')=>({path,type,mode,sha})
const tree=(...items)=>({truncated:false,tree:items})
test('no migration sources is bound to exact committed source and tree, not live database readiness',()=>{
 const evidence=migrationReadiness(tree(entry('web/app.ts')),sha,treeSha)
 assert.deepEqual(evidence,{source_sha:sha,tree_sha:treeSha,policy:'no-migration-sources-v1'})
 validateReadiness(evidence,sha)
 for(const change of [e=>e.source_sha=treeSha,e=>e.tree_sha='bad',e=>e.policy='ready',e=>e.override=true]) {
  const value={...evidence};change(value);assert.throws(()=>validateReadiness(value,sha))
 }
})
for(const name of ['hosted/migrations/001.sql','web/migrations/change.js','db/migrate/change.rb','prisma/migrations/lock.toml','schema.sql','hosted/Migrations/change.go','Prisma/schema.prisma','nested/SCHEMA.SQL']) {
 test(`immutable inventory blocks ${name}`,()=>assert.throws(()=>migrationReadiness(tree(entry(name)),sha,treeSha)))
}
test('incomplete, opaque or malformed inventories fail closed',()=>{
 for(const value of [{truncated:true,tree:[]},tree(entry('hidden','commit','160000')),tree(entry('hidden','blob','120000')),tree(entry('../hidden')),tree(entry('a'),entry('a'))]) assert.throws(()=>migrationReadiness(value,sha,treeSha))
})
test('validation and consumer use the API committed tree rather than writable target',()=>{
 const verifier=fs.readFileSync('tools/framebriar-ci/verify.mjs','utf8')
 const consumer=fs.readFileSync('tools/framebriar-ci/bridge.mjs','utf8')
 assert.match(verifier,/migrationReadiness\(tree, input.sha, commit.tree.sha\)/)
 assert.match(consumer,/migrationReadiness\(tree,targetSha,commit.tree.sha\)/)
 assert.match(consumer,/assert.deepEqual\(evidence.prebuild.migration_readiness/)
})
