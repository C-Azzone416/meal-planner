import {test} from 'node:test'
import assert from 'node:assert/strict'
import {spawnSync} from 'node:child_process'
import {fileURLToPath} from 'node:url'
test('reviewed compiled output packaging',()=>{
 const r=spawnSync('python3',[fileURLToPath(new URL('./package-reviewed-output_test.py',import.meta.url))],{env:{PATH:process.env.PATH,PYTHONDONTWRITEBYTECODE:'1'},encoding:'utf8',timeout:30000,maxBuffer:1024*1024})
 assert.ifError(r.error);assert.equal(r.status,0,r.stdout+r.stderr);assert.match(r.stderr,/Ran 5 tests/)
})
