import fs from 'node:fs'
import path from 'node:path'
import assert from 'node:assert/strict'
import { publicConfig } from './config.mjs'
const root = process.argv[2] ?? 'web/dist'
const expected = publicConfig(process.env)
assert.match(expected.buildSha, /^[0-9a-f]{40}$/, 'Explicit expected deployment SHA is required')
assert.ok(expected.environment, 'Explicit expected target is required')
const actual = JSON.parse(fs.readFileSync(path.join(root, 'framebriar-build.json'), 'utf8'))
assert.deepEqual(actual, expected, 'Packaged public configuration differs from requested target')
let scripts = ''
function walk(dir) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const file = path.join(dir, entry.name)
    assert.ok(!entry.isSymbolicLink(), 'Artifact must not contain symlinks')
    if (entry.isDirectory()) walk(file)
    else {
      const text = fs.readFileSync(file).toString('utf8')
      // Known synthetic markers and credential formats; names are not secret values.
      assert.ok(!/FRAMEBRIAR_FAKE_SERVICE|FRAMEBRIAR_TEST_SECRET|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|-----BEGIN (?:RSA |EC )?PRIVATE KEY-----/.test(text), 'Forbidden credential or fixture marker in artifact')
      if (/\.m?js$/.test(file)) scripts += text
    }
  }
}
walk(root)
assert.ok(scripts.includes(expected.buildSha), 'Actual JavaScript lacks the exact deployed SHA')
assert.ok(scripts.includes(expected.appId), 'Actual JavaScript lacks the environment app identity')
if (!expected.screenshots) assert.ok(!/FRAMEBRIAR_CAPTURE_ACTIVE|html2canvas|html-to-image|toPng\(/.test(scripts), 'Screenshot code present with capture disabled')
if (expected.screenshots) assert.ok(scripts.includes('FRAMEBRIAR_CAPTURE_ACTIVE'), 'Enabled capture marker absent from actual JavaScript')
console.log('Framebriar target artifact verified:', expected.environment, expected.enabled ? 'enabled' : 'disabled', expected.screenshots ? 'capture enabled' : 'capture absent')
