import { test } from 'node:test'
import assert from 'node:assert/strict'
import { publicConfig } from './config.mjs'
const sha = 'a'.repeat(40)
const env = { FRAMEBRIAR_ENABLED: '1', FRAMEBRIAR_ENVIRONMENT: 'preview', FRAMEBRIAR_ALLOWED_ORIGIN: 'https://preview.example.test', FRAMEBRIAR_APP_ID: 'meal-planner-preview', FRAMEBRIAR_BUILD_SHA: sha, FRAMEBRIAR_SCREENSHOTS: '0' }
test('public allowlist, isolated target and exact provenance', () => {
  assert.deepEqual(publicConfig({ ...env, FRAMEBRIAR_CREDENTIAL_KEYS: 'secret', NODE_AUTH_TOKEN: 'secret', VITE_SECRET: 'secret' }), { enabled: true, environment: 'preview', origin: env.FRAMEBRIAR_ALLOWED_ORIGIN, appId: env.FRAMEBRIAR_APP_ID, buildSha: sha, screenshots: false })
  assert.equal(publicConfig({ ...env, FRAMEBRIAR_ENVIRONMENT: 'production', FRAMEBRIAR_APP_ID: 'meal-planner' }).enabled, true)
  assert.equal(publicConfig({ ...env, VERCEL_ENV: 'production' }).enabled, false)
})
test('missing or malformed configuration fails closed', () => {
  for (const key of Object.keys(env).filter(k => k !== 'FRAMEBRIAR_SCREENSHOTS')) {
    const missing = { ...env }; delete missing[key]
    assert.equal(publicConfig(missing).enabled, false, key)
  }
  for (const origin of ['https://localhost', 'https://127.0.0.1', 'https://*.example.test', 'http://example.test', 'https://example.test/', 'https://user@example.test', 'https://example.test?q=x']) {
    assert.equal(publicConfig({ ...env, FRAMEBRIAR_ALLOWED_ORIGIN: origin }).enabled, false, origin)
  }
  assert.equal(publicConfig({ ...env, FRAMEBRIAR_BUILD_SHA: 'dev' }).enabled, false)
  assert.equal(publicConfig({ ...env, FRAMEBRIAR_ENABLED: '0', FRAMEBRIAR_SCREENSHOTS: '1' }).screenshots, false)
})
