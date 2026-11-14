import {test} from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import {spawnSync} from 'node:child_process'
test('private workflow invokes dedicated deployment capability, never optional legacy flag',()=>{
 const w=fs.readFileSync('.github/workflows/preview.yml','utf8').split('\n  private-proof:\n')[1].split('\n  pull:\n')[0]
 assert.match(w,/run: node trusted\/tools\/framebriar-ci\/verify-preview.mjs/)
 assert.doesNotMatch(w,/REQUIRE_PREVIEW|run:.*bridge.mjs/)
 const source=fs.readFileSync('tools/framebriar-ci/verify-preview.mjs','utf8')
 assert.match(source,/\.\.\.input,subject:'develop',prNumber:0,requirePreview:true/)
})
test('older trusted revision lacking deployment entrypoint fails closed',()=>{
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'legacy-trusted-preview-'))
 try{
  const dir=path.join(root,'trusted/tools/framebriar-ci');fs.mkdirSync(dir,{recursive:true})
  fs.writeFileSync(path.join(dir,'bridge.mjs'),'console.log("legacy frontend-only success")\n')
  const r=spawnSync(process.execPath,['trusted/tools/framebriar-ci/verify-preview.mjs'],{cwd:root,env:{PATH:process.env.PATH},encoding:'utf8',timeout:5000})
  assert.notEqual(r.status,0);assert.equal(r.stdout,'');assert.match(r.stderr,/MODULE_NOT_FOUND/)
 }finally{fs.rmSync(root,{recursive:true,force:true})}
})
