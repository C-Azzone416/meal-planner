import { test } from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { spawnSync } from 'node:child_process'
import { classifyDependency } from './classify-dependency.mjs'
const sdk = '@masra91/framebriar'
const fixture = () => ({pkg: {dependencies: {react: '^19.0.0'}}, lock: {lockfileVersion: 3, packages: {'': {dependencies: {react: '^19.0.0'}}, 'node_modules/react': {version: '19.0.0'}}}})
const classify = ({pkg, lock}) => classifyDependency(JSON.stringify(pkg), JSON.stringify(lock))
test('valid public manifests select ordinary install', () => assert.equal(classify(fixture()), false))
for (const group of ['dependencies', 'devDependencies', 'optionalDependencies', 'peerDependencies']) {
  test(`private ${group} with matching lock selects reviewed evidence`, () => {
    const x = fixture(); x.pkg[group] = {[sdk]: '0.4.1'}; x.lock.packages[''][group] = {[sdk]: '0.4.1'}
    x.lock.packages[`node_modules/${sdk}`] = {version: '0.4.1', optional: group === 'optionalDependencies'}
    assert.equal(classify(x), true)
  })
  test(`lock root ${group} alone selects reviewed evidence`, () => {
    const x = fixture(); x.lock.packages[''][group] = {[sdk]: '0.4.1'}; assert.equal(classify(x), true)
  })
}
for (const key of [`node_modules/${sdk}`, `node_modules/parent/node_modules/${sdk}`]) {
  test(`locked-only ${key} selects reviewed evidence`, () => {
    const x = fixture(); x.lock.packages[key] = {version: '0.4.1', optional: true}; assert.equal(classify(x), true)
  })
}
test('aliases and legacy v2 lock entries cannot hide private package', () => {
  const x = fixture(); x.pkg.optionalDependencies = {alias: `npm:${sdk}@0.4.1`}; assert.equal(classify(x), true)
  const y = fixture(); y.lock.lockfileVersion = 2; y.lock.dependencies = {parent: {dependencies: {[sdk]: {version: '0.4.1'}}}}; assert.equal(classify(y), true)
  const z = fixture(); z.lock.packages['node_modules/alias'] = {name: sdk, version: '0.4.1'}; assert.equal(classify(z), true)
})
for (const [name, mutate] of [
  ['package array', x => x.pkg = []], ['package null', x => x.pkg = null],
  ['dependency array', x => x.pkg.optionalDependencies = []], ['dependency null', x => x.pkg.devDependencies = null],
  ['dependency value object', x => x.pkg.dependencies.react = {}], ['dependency value empty', x => x.pkg.dependencies.react = ''],
  ['lock array', x => x.lock = []], ['lock null', x => x.lock = null],
  ['unsupported lock version', x => x.lock.lockfileVersion = 1], ['missing lock packages', x => delete x.lock.packages],
  ['lock packages array', x => x.lock.packages = []], ['missing lock root', x => delete x.lock.packages['']],
  ['lock entry null', x => x.lock.packages['node_modules/react'] = null], ['lock entry array', x => x.lock.packages['node_modules/react'] = []],
  ['lock declaration malformed', x => x.lock.packages[''].dependencies = 'bad'], ['legacy entry malformed', x => x.lock.dependencies = {foo: null}],
]) test(`malformed ${name} fails closed`, () => { const x = fixture(); mutate(x); assert.throws(() => classify(x)) })
test('JSON parse failures fail closed', () => { assert.throws(() => classifyDependency('{', '{}')); assert.throws(() => classifyDependency('{}', '{')) })
test('CLI emits no fallback on malformed or missing manifests', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'dependency-classifier-'))
  try {
    fs.mkdirSync(path.join(dir, 'web')); const output = path.join(dir, 'output')
    fs.writeFileSync(path.join(dir, 'web/package.json'), '{}'); fs.writeFileSync(path.join(dir, 'web/package-lock.json'), '{')
    const run = () => spawnSync(process.execPath, ['tools/framebriar-ci/classify-dependency.mjs', dir], {env: {...process.env, GITHUB_OUTPUT: output}})
    assert.notEqual(run().status, 0); assert.equal(fs.existsSync(output), false)
    fs.unlinkSync(path.join(dir, 'web/package-lock.json')); assert.notEqual(run().status, 0); assert.equal(fs.existsSync(output), false)
  } finally { fs.rmSync(dir, {recursive: true, force: true}) }
})
test('CI and Preview use identical classifier from fixed workflow source', () => {
  for (const file of ['ci.yml', 'preview.yml']) {
    const workflow = fs.readFileSync(`.github/workflows/${file}`, 'utf8')
    const checkout = workflow.split('      - name: Check out fixed dependency classifier only')[1].split('      - name: ')[0]
    assert.match(checkout, /ref: \$\{\{ github.workflow_sha \}\}/)
    assert.match(checkout, /sparse-checkout: tools\/framebriar-ci\/classify-dependency.mjs/)
    assert.match(workflow, /node \.dependency-policy\/tools\/framebriar-ci\/classify-dependency.mjs /)
    assert.doesNotMatch(workflow, /Object.hasOwn\(p.dependencies/)
  }
})
