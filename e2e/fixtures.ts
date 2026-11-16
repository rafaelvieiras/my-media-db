// Test fixture that collects the FRONT-END V8 COVERAGE per journey (Layer 2). It
// only acts when E2E_COVERAGE=1 (the normal run stays lightweight, with neither
// sourcemap nor collection) and on Chromium (page.coverage is CDP-only). Each test
// adds its data to the monocart cache; the global-teardown generates the report.
import { test as base, expect } from '@playwright/test'
import { CoverageReport } from 'monocart-coverage-reports'
import { mcrOptions } from './lib/coverage'

const coletar = process.env.E2E_COVERAGE === '1'

export const test = base.extend({
  page: async ({ page, browserName }, use) => {
    const cobrindo = coletar && browserName === 'chromium'
    if (cobrindo) {
      await page.coverage.startJSCoverage({ resetOnNavigation: false })
    }

    await use(page)

    if (cobrindo) {
      const cov = await page.coverage.stopJSCoverage()
      // Each add() persists to the on-disk cache (shared across processes).
      await new CoverageReport(mcrOptions()).add(cov)
    }
  },
})

export { expect }
