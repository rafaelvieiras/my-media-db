// Shared FRONT-END coverage config (monocart V8) — used in 3 processes: the
// global-setup cleans the cache, each worker calls add() with the Chromium V8
// coverage, and the global-teardown calls generate(). The options must be IDENTICAL
// in all three (the on-disk cache is derived from them). See global-setup.ts (Layer 2).
import { join } from 'node:path'
import { ARTIFACTS } from './paths'

export const COVERAGE_DIR = join(ARTIFACTS, 'web-coverage')

// mcrOptions returns the MonocartCoverageReports options. sourceFilter keeps only
// our source code (through the inline sourcemap), excluding
// node_modules/vendor/generated.
export function mcrOptions() {
  return {
    name: 'MyMediaDB E2E — cobertura do front',
    outputDir: COVERAGE_DIR,
    // v8 report (browsable HTML) + console summary.
    reports: [['v8'], ['console-summary']] as unknown as string[],
    // Only files from our src/; discards deps and artifacts.
    sourceFilter: (sourcePath: string) =>
      sourcePath.includes('/src/') &&
      !sourcePath.includes('node_modules') &&
      !sourcePath.includes('/src/lib/api-types') &&
      !/\.(css|d\.ts)$/.test(sourcePath),
    // Merges the chunks from multiple add() calls before generating (multiprocess).
    logging: 'error',
  }
}
