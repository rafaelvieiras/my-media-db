// Fixture de teste que coleta a COBERTURA V8 do front por jornada (Camada 2,
// briefing 0028). Só age quando E2E_COVERAGE=1 (o run normal segue leve, sem
// sourcemap nem coleta) e no Chromium (page.coverage é CDP-only). Cada teste
// adiciona seus dados ao cache do monocart; o global-teardown gera o relatorio.
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
      // Cada add() persiste no cache em disco (compartilhado entre processos).
      await new CoverageReport(mcrOptions()).add(cov)
    }
  },
})

export { expect }
