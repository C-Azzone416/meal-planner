import { test } from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
const read = name => fs.readFileSync(`.github/workflows/${name}.yml`, 'utf8')
test('ordinary CI and deployment workflows receive no private package reader', () => {
  for (const name of ['ci', 'preview', 'deploy-production', 'vercel-bootstrap']) {
    assert.doesNotMatch(read(name), /secrets\.FRAMEBRIAR_PACKAGE_READ_TOKEN/)
  }
  const ci = read('ci')
  assert.match(ci, /TRUSTED_WORKFLOW_SHA: \$\{\{ vars.FRAMEBRIAR_TRUSTED_WORKFLOW_SHA \}\}/)
  assert.match(ci, /node .trusted-evidence\/tools\/framebriar-ci\/bridge.mjs/)
  assert.match(ci, /needs.affected.outputs.hosted_go == 'true'/)
})
test('protected installation confines package reader to inert manifest install', () => {
  const workflow = read('framebriar-reviewed-ci')
  const blocks = workflow.split(/(?=^      - )/m)
  const readers = blocks.filter(block => block.includes('secrets.FRAMEBRIAR_PACKAGE_READ_TOKEN'))
  assert.equal(readers.length, 1)
  assert.match(readers[0], /npm ci --ignore-scripts/)
  assert.match(readers[0], /env -i/)
  assert.doesNotMatch(readers[0], /npm (?:run|rebuild)|upload-artifact|actions\/cache|vercel@/)
  assert.match(workflow, /environment: framebriar-ci/)
  assert.match(workflow, /test:browser/)
  assert.match(workflow, /check-targets.sh/)
})
