import { test } from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
for (const name of ['ci', 'preview', 'deploy-production', 'vercel-bootstrap']) {
  test(`${name}: package reader is install-only, uncached, with lifecycle disabled`, () => {
    const workflow = fs.readFileSync(`.github/workflows/${name}.yml`, 'utf8')
    const blocks = workflow.split(/(?=^      - )/m)
    const reader = blocks.filter(block => block.includes('secrets.FRAMEBRIAR_PACKAGE_READ_TOKEN'))
    assert.ok(reader.length > 0)
    for (const block of reader) {
      assert.match(block, /npm ci --ignore-scripts --no-audit --no-fund --registry=https:\/\/registry.npmjs.org/)
      assert.match(block, /trap 'rm -rf "\$install_root"' EXIT/)
      assert.match(block, /env -i PATH=/)
      assert.match(block, /cp web\/package.json web\/package-lock.json/)
      assert.match(block, /--userconfig=.*--globalconfig=/)
      assert.doesNotMatch(block, /npm (?:run|rebuild)|vercel@|upload-artifact|actions\/cache/)
    }
    assert.doesNotMatch(workflow, /cache: npm/)
    assert.match(workflow, /run: bash tools\/feedback\/postinstall.sh/)
    if (name === 'ci') assert.match(reader[0], /github.event_name == 'push'/)
    else {
      assert.match(workflow, /FRAMEBRIAR_BUILD_SHA: \$\{\{ inputs.sha \}\}/)
      assert.match(workflow, /run: node tools\/feedback\/prepare-build.mjs/)
      assert.match(workflow, /run: node tools\/feedback\/check-server-artifact.mjs/)
      for (const block of blocks.filter(b => /name: Vercel build/.test(b))) {
        assert.match(block, /FRAMEBRIAR_ENABLED: "0"/)
        assert.match(block, /FRAMEBRIAR_SCREENSHOTS: "0"/)
        assert.doesNotMatch(block, /secrets\./)
      }
    }
  })
}

test('browser CI is credential-free, full-stack and gated for backend-only changes', () => {
  const workflow = fs.readFileSync('.github/workflows/ci.yml', 'utf8')
  const web = workflow.split('\n  web:\n')[1].split('\n  hosted-go:\n')[0]
  assert.match(web, /if: needs.affected.outputs.web == 'true' \|\| needs.affected.outputs.hosted_go == 'true'/)
  assert.match(workflow, /SEL_WEB: \$\{\{ needs.affected.outputs.web == 'true' \|\| needs.affected.outputs.hosted_go == 'true' \}\}/)
  assert.match(web, /actions\/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e/)
  assert.match(web, /pg_config --bindir/)
  assert.match(web, /for tool in initdb pg_ctl postgres/)
  assert.match(web, /npx --no-install playwright install --with-deps chromium webkit/)
  assert.match(web, /npm run --prefix web test:browser/)
  const browserSteps = web.slice(web.indexOf('      - uses: actions/setup-go@'))
  assert.doesNotMatch(browserSteps, /secrets\.|NODE_AUTH_TOKEN|FRAMEBRIAR_PACKAGE_READ_TOKEN|VERCEL_TOKEN/)
  assert.ok(web.indexOf('run: bash tools/feedback/postinstall.sh') < web.indexOf('npx --no-install playwright'))
})
