import { test } from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { spawnSync } from 'node:child_process'
import { publicConfig } from './config.mjs'
const env = { ...process.env, FRAMEBRIAR_ENABLED: '1', FRAMEBRIAR_ENVIRONMENT: 'preview', FRAMEBRIAR_APP_ID: 'meal-planner-preview', FRAMEBRIAR_ALLOWED_ORIGIN: 'https://preview.example.test', FRAMEBRIAR_BUILD_SHA: 'a'.repeat(40), FRAMEBRIAR_SCREENSHOTS: '0' }
function fixture(fn) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'framebriar-artifact-'))
  try { fn(dir) } finally { fs.rmSync(dir, {recursive:true, force:true}) }
}
const check = (dir, overrides = {}) => spawnSync(process.execPath, ['tools/feedback/check-artifact.mjs', dir], { env: { ...env, ...overrides }, encoding: 'utf8' }).status
test('artifact guard checks actual JavaScript, exact target, capture and secrets', () => fixture(dir => {
  fs.writeFileSync(path.join(dir, 'framebriar-build.json'), JSON.stringify(publicConfig(env)))
  const file = path.join(dir, 'app.js')
  const valid = `${env.FRAMEBRIAR_BUILD_SHA} meal-planner-preview`
  fs.writeFileSync(file, valid)
  assert.equal(check(dir), 0)
  for (const marker of ['FRAMEBRIAR_CAPTURE_ACTIVE', 'FRAMEBRIAR_TEST_SECRET', 'FRAMEBRIAR_FAKE_SERVICE', `ghp_${'x'.repeat(30)}`]) {
    fs.writeFileSync(file, `${valid} ${marker}`)
    assert.notEqual(check(dir), 0)
  }
  fs.writeFileSync(file, 'wrong SHA')
  assert.notEqual(check(dir), 0)
  fs.writeFileSync(file, valid)
  assert.notEqual(check(dir, { FRAMEBRIAR_ENVIRONMENT: 'production' }), 0)
  assert.notEqual(check(dir, { FRAMEBRIAR_ENABLED: '0' }), 0)
}))
test('server guard requires real materialized feedback binary provenance', () => fixture(dir => {
  const func = path.join(dir, 'api', 'feedback', 'status.func')
  fs.mkdirSync(func, { recursive: true })
  const file = path.join(func, 'bootstrap')
  const run = () => spawnSync(process.execPath, ['tools/feedback/check-server-artifact.mjs', dir, env.FRAMEBRIAR_BUILD_SHA, 'preview']).status
  fs.writeFileSync(file, `${env.FRAMEBRIAR_BUILD_SHA} preview meal-planner-preview`)
  assert.equal(run(), 0)
  fs.writeFileSync(file, 'no provenance')
  assert.notEqual(run(), 0)
}))
