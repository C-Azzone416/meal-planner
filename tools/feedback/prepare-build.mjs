import fs from 'node:fs'
import assert from 'node:assert/strict'
import { publicConfig } from './config.mjs'
const config = publicConfig(process.env)
assert.match(config.buildSha, /^[0-9a-f]{40}$/, 'Explicit build SHA required')
assert.ok(config.environment, 'Explicit build target required')
assert.equal(process.env.FRAMEBRIAR_APP_ID, config.appId, 'App must match target')
fs.writeFileSync('hosted/feedback_build.go', `package hosted\n\n// Generated public provenance. No credentials.\nconst feedbackBuildSHA = ${JSON.stringify(config.buildSha)}\nconst feedbackBuildEnvironment = ${JSON.stringify(config.environment)}\nconst feedbackBuildApp = ${JSON.stringify(config.appId)}\n`)
