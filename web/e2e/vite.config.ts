// Test-only dev-server proxy. Not referenced by the production build.
import { defineConfig, mergeConfig } from 'vite'
import base from '../vite.config.ts'
export default defineConfig(context => mergeConfig(base(context), {
  server: { proxy: {
    '/api': 'http://127.0.0.1:4182',
    '/__fixture': 'http://127.0.0.1:4182',
  } },
}))
