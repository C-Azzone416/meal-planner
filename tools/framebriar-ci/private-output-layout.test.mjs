import {test} from 'node:test'
import assert from 'node:assert/strict'
import {spawnSync} from 'node:child_process'
import {fileURLToPath} from 'node:url'
test('closed full-output inventory groundwork', () => {
 const result=spawnSync('python3',[fileURLToPath(new URL('./private-output-layout_test.py',import.meta.url))],{env:{PATH:process.env.PATH,PYTHONDONTWRITEBYTECODE:'1'},encoding:'utf8',timeout:30_000,maxBuffer:1024*1024})
 assert.ifError(result.error)
 assert.equal(result.status,0,result.stdout+result.stderr)
 assert.match(result.stderr,/Ran 22 tests/)
})
