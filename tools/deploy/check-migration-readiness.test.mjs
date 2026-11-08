import { test } from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { spawnSync } from 'node:child_process'
const script = path.resolve('tools/deploy/check-migration-readiness.sh')
const run = root => spawnSync('bash', [script, root], {encoding:'utf8'})
function fixture(fn) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'migration-readiness-'))
  try { fn(root) } finally { fs.rmSync(root, {recursive:true, force:true}) }
}
test('no migration files makes no live readiness claim', () => fixture(root => {
  const result = run(root)
  assert.equal(result.status, 0)
  assert.match(result.stdout, /no database readiness claim/)
}))
for (const file of ['migrations/002_framebriar.sql', 'hosted/migrations/001_recipes.sql', 'db/migrations/legacy.sql']) {
  test(`blocks unverified migration readiness: ${file}`, () => fixture(root => {
    fs.mkdirSync(path.dirname(path.join(root, file)), {recursive:true})
    fs.writeFileSync(path.join(root, file), '-- synthetic fixture')
    const result = run(root)
    assert.equal(result.status, 1)
    assert.match(result.stdout, /No migration ran here/)
    assert.match(result.stdout, /do not bypass this guard/)
  }))
}
test('rejects missing source and symlinked migration directory', () => fixture(root => {
  assert.equal(run(path.join(root, 'missing')).status, 2)
  fs.symlinkSync(root, path.join(root, 'migrations'))
  assert.equal(run(root).status, 2)
}))
test('reviewed prebuild checks immutable source on clean validation and reporting runners',()=>{
 const workflow=fs.readFileSync('.github/workflows/framebriar-reviewed-ci.yml','utf8')
 const validate=workflow.split('\n  validate:\n')[1].split('\n  pull-preview:\n')[0]
 const frontend=workflow.split('\n  frontend:\n')[1].split('\n  evidence:\n')[0]
 const reporter=workflow.split('\n  evidence:\n')[1]
 assert.match(validate,/node trusted\/tools\/framebriar-ci\/verify\.mjs/)
 assert.ok(frontend.indexOf('node trusted/tools/framebriar-ci/verify.mjs')<frontend.indexOf('npm run --prefix web'))
 assert.doesNotMatch(frontend,/check-migration-readiness.sh/)
 assert.ok(reporter.indexOf('node trusted/tools/framebriar-ci/verify.mjs')<reporter.indexOf('seal-prebuild.mjs'))
 assert.match(reporter,/READINESS_JSON: \$\{\{ runner.temp \}\}\/migration-readiness.json/)
 assert.doesNotMatch(workflow,/continue-on-error/)
})
