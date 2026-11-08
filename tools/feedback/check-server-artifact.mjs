import fs from 'node:fs'
import path from 'node:path'
import assert from 'node:assert/strict'
const [root, sha, environment] = process.argv.slice(2)
assert.match(sha ?? '', /^[0-9a-f]{40}$/)
assert.ok(['preview', 'production'].includes(environment))
const app = environment === 'preview' ? 'meal-planner-preview' : 'meal-planner'
let checked = 0
function walk(dir) {
  for (const item of fs.readdirSync(dir, { withFileTypes: true })) {
    const file = path.join(dir, item.name)
    assert.ok(!item.isSymbolicLink(), 'Packaged server output must be self-contained')
    if (item.isDirectory()) walk(file)
    else {
      const bytes = fs.readFileSync(file)
      assert.ok(!bytes.includes(Buffer.from('FRAMEBRIAR_FAKE_SERVICE')) && !bytes.includes(Buffer.from('FRAMEBRIAR_TEST_SECRET')), 'Test seam leaked into packaged server')
      if (file.includes('feedback') && item.name === 'bootstrap') {
        for (const value of [sha, environment, app]) assert.ok(bytes.includes(Buffer.from(value)), 'Feedback server lacks exact compiled provenance')
        checked++
      }
    }
  }
}
walk(root)
assert.ok(checked > 0, 'No materialized feedback server binary found')
console.log('Verified feedback server provenance in', checked, 'function(s)')
