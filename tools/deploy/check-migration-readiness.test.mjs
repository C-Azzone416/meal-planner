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
for (const [name, job] of [['preview','build'], ['deploy-production','build'], ['vercel-bootstrap','build-production'], ['vercel-bootstrap','build-preview']]) {
  test(`${name}/${job} uses unconditional readiness guard before installation/build`, () => {
    const workflow = fs.readFileSync(`.github/workflows/${name}.yml`, 'utf8')
    const body = workflow.split(`\n  ${job}:\n`)[1]?.split(/\n  [a-z][a-z-]*:\n/)[0]
    assert.ok(body, 'Expected build job exists')
    const guard = '      - name: Verify manual migration readiness\n        run: bash tools/deploy/check-migration-readiness.sh .'
    assert.ok(body.includes(guard), 'Guard must be unconditional and have no ignored failure')
    assert.equal(body.split('check-migration-readiness.sh').length - 1, 1)
    // The private registry install belongs only to the protected workflow.
    assert.doesNotMatch(body, /secrets\.FRAMEBRIAR_PACKAGE_READ_TOKEN/)
    assert.ok(body.indexOf(guard) < body.indexOf('      - name: Vercel build'))
    assert.doesNotMatch(body, /skipped, no schema yet|hashFiles\('db\/migrations/)
  })
}
