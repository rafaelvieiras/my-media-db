import { defineConfig, devices } from '@playwright/test'
import { BASE_URL } from './lib/paths'

// Layer 2 config: Playwright drives a real Chromium against the app BINARY
// (SPA + API on the same origin) with the fake provider. The global-setup
// builds+starts the -cover binary; the global-teardown stops it and extracts the
// binary coverage (GOCOVERDIR). workers=1: the journeys share ONE database (state
// accumulates on purpose — E2E narrative), so they run serially.
export default defineConfig({
  testDir: './tests',
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: [['list']],
  globalSetup: require.resolve('./global-setup'),
  globalTeardown: require.resolve('./global-teardown'),
  timeout: 30_000,
  expect: { timeout: 10_000 },
  use: {
    baseURL: BASE_URL,
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
})
