import { defineConfig, devices } from '@playwright/test'
export default defineConfig({
  testDir: './e2e',
  testMatch: '*.spec.ts',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: 'list',
  use: { baseURL: 'http://localhost:4181', trace: 'retain-on-failure' },
  projects: [
    { name: 'desktop-chromium', use: { ...devices['Desktop Chrome'] } },
    { name: 'phone-chromium', use: { ...devices['Pixel 7'] } },
    { name: 'phone-webkit', use: { ...devices['iPhone 13'] } },
  ],
  webServer: [{
    command: 'bash e2e/start-backend.sh',
    url: 'http://127.0.0.1:4182/__fixture/health',
    reuseExistingServer: !process.env.CI,
    timeout: 120000,
    gracefulShutdown: { signal: 'SIGTERM', timeout: 15000 },
  }, {
    command: 'npm run dev -- --config e2e/vite.config.ts --host localhost --port 4181 --strictPort',
    url: 'http://localhost:4181/e2e/feedback.html',
    reuseExistingServer: false,
    env: {
      FRAMEBRIAR_ENABLED: '1', FRAMEBRIAR_ENVIRONMENT: 'preview',
      FRAMEBRIAR_APP_ID: 'meal-planner-preview',
      FRAMEBRIAR_ALLOWED_ORIGIN: 'http://localhost:4181',
      FRAMEBRIAR_BUILD_SHA: '1234567890123456789012345678901234567890',
      FRAMEBRIAR_SCREENSHOTS: '1',
    },
  }],
})
