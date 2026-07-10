import { defineConfig, devices } from '@playwright/test'
import { BASE_URL } from './lib/paths'

// Config da Camada 2 (briefing 0028): Playwright dirige um Chromium real contra o
// BINARIO da app (SPA + API no mesmo origin) com o provider fake. O global-setup
// builda+sobe o binario -cover; o global-teardown para e extrai a cobertura do
// binario (GOCOVERDIR). workers=1: as jornadas compartilham UM banco (estado
// acumula de proposito — narrativa E2E), entao rodam em serie.
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
