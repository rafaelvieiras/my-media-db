// Config compartilhada da cobertura do FRONT (monocart V8) — usada em 3 processos:
// o global-setup limpa o cache, cada worker chama add() com a cobertura V8 do
// Chromium, e o global-teardown chama generate(). As opcoes precisam ser IDENTICAS
// nos tres (o cache em disco é derivado delas). Ver briefing 0028 (Camada 2).
import { join } from 'node:path'
import { ARTIFACTS } from './paths'

export const COVERAGE_DIR = join(ARTIFACTS, 'web-coverage')

// mcrOptions devolve as opcoes do MonocartCoverageReports. sourceFilter mantem só o
// nosso codigo-fonte (via sourcemap inline), fora node_modules/vendor/gerados.
export function mcrOptions() {
  return {
    name: 'MyMediaDB E2E — cobertura do front',
    outputDir: COVERAGE_DIR,
    // Relatorio v8 (HTML navegavel) + resumo no console.
    reports: [['v8'], ['console-summary']] as unknown as string[],
    // Só arquivos do nosso src/; descarta deps e artefatos.
    sourceFilter: (sourcePath: string) =>
      sourcePath.includes('/src/') &&
      !sourcePath.includes('node_modules') &&
      !sourcePath.includes('/src/lib/api-types') &&
      !/\.(css|d\.ts)$/.test(sourcePath),
    // Junta os pedacos de multiplos add() antes de gerar (multiprocess).
    logging: 'error',
  }
}
