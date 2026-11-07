import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'
import { fileURLToPath } from 'node:url'
import { publicConfig } from '../tools/feedback/config.mjs'

export default defineConfig(({ mode }) => {
  const feedback = publicConfig(process.env, mode)
  return {
    // VITE_* is not an approved path to publish runtime or package credentials.
    envPrefix: [],
    define: { __FRAMEBRIAR_CONFIG__: JSON.stringify(feedback) },
    resolve: { alias: {
      'feedback-capture': fileURLToPath(new URL(`./src/feedback/capture${feedback.screenshots ? '' : '.disabled'}.ts`, import.meta.url)),
    } },
    plugins: [react(), {
      name: 'framebriar-public-provenance',
      generateBundle() {
        this.emitFile({ type: 'asset', fileName: 'framebriar-build.json', source: JSON.stringify(feedback) })
      },
    }],
    server: { proxy: { '/api': 'http://localhost:3000' } },
  }
})
