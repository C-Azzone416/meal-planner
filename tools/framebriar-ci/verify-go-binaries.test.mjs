import { test } from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { spawnSync } from 'node:child_process'
const helper = path.resolve('tools/framebriar-ci/verify-go-binaries.sh')
function fixture(run) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'go-binary-check-'))
  const bin = path.join(root, 'bin'), functions = path.join(root, 'functions'), log = path.join(root, 'calls')
  fs.mkdirSync(bin); fs.mkdirSync(functions)
  for (const name of ['one.func', 'space name.func']) {
    fs.mkdirSync(path.join(functions,name)); fs.writeFileSync(path.join(functions,name,'bootstrap'),'synthetic')
  }
  fs.writeFileSync(path.join(bin,'go'), `#!/usr/bin/env python3
import os,sys,signal
signal.signal(signal.SIGPIPE, signal.SIG_DFL)
with open(os.environ['TEST_LOG'],'a') as f: f.write(sys.argv[1]+'\\n')
if sys.argv[1]=='version':
 print(sys.argv[-1]+': '+os.environ.get('TEST_VERSION','go1.26.11'),flush=True)
 # Larger than a pipe buffer, reliably exposing the previous head SIGPIPE.
 for i in range(128): sys.stdout.write('synthetic module metadata '+('x'*16384)+'\\n')
 sys.stdout.flush()
 sys.exit(int(os.environ.get('TEST_METADATA_EXIT','0')))
assert sys.argv[1:4]==['run','golang.org/x/vuln/cmd/govulncheck@v1.9.0','-mode=binary']
sys.exit(int(os.environ.get('TEST_SCAN_EXIT','0')))
`, {mode:0o755})
  fs.writeFileSync(path.join(bin,'find'), `#!/usr/bin/env python3
import os,sys
if os.environ.get('TEST_FIND_FAIL'):
 sys.stdout.write(os.path.join(sys.argv[1],'one.func','bootstrap')+'\\0');sys.stdout.flush();sys.exit(23)
os.execv('/usr/bin/find',['find']+sys.argv[1:])
`, {mode:0o755})
  const env = {...process.env, PATH:bin+path.delimiter+process.env.PATH,TEST_LOG:log}
  const check = overrides => spawnSync('bash',[helper,functions],{env:{...env,...overrides},encoding:'utf8',timeout:15000})
  const calls = () => fs.existsSync(log) ? fs.readFileSync(log,'utf8').trim().split('\n') : []
  try { run({check,calls,env,functions,log}) } finally { fs.rmSync(root,{recursive:true,force:true}) }
}
test('reproduces old pipeline SIGPIPE141; complete metadata collection scans every binary', () => fixture(({check,calls,env,functions,log}) => {
  const old = spawnSync('bash',['-o','pipefail','-c',`go version -m "$1" | head -n1 | grep -E 'go1\\.26\\.11$' >/dev/null`,'test',path.join(functions,'one.func','bootstrap')],{env,encoding:'utf8'})
  assert.equal(old.status,141)
  fs.writeFileSync(log,'')
  assert.equal(check().status,0)
  assert.deepEqual(calls(),['version','run','version','run'])
}))
test('producer failure cannot be hidden by a valid first line', () => fixture(({check,calls}) => {
  assert.equal(check({TEST_METADATA_EXIT:'22'}).status,22)
  assert.deepEqual(calls(),['version'])
}))
test('find partial output failure is propagated before scanning anything', () => fixture(({check,calls}) => {
  assert.equal(check({TEST_FIND_FAIL:'1'}).status,23)
  assert.deepEqual(calls(),[])
}))
test('exact compiler check and vulnerability scanner still fail closed', () => {
  for (const version of ['go1.26.10','go1.26.12','go1.26.11 extra','']) fixture(({check,calls}) => {
    assert.notEqual(check({TEST_VERSION:version}).status,0)
    assert.deepEqual(calls(),['version'])
  })
  fixture(({check,calls}) => { assert.equal(check({TEST_SCAN_EXIT:'24'}).status,24); assert.deepEqual(calls(),['version','run']) })
})
test('empty or absent functions cannot pass verification', () => fixture(({check,functions}) => {
  fs.rmSync(functions,{recursive:true}); fs.mkdirSync(functions)
  assert.notEqual(check().status,0)
  fs.rmdirSync(functions)
  assert.notEqual(check().status,0)
}))
