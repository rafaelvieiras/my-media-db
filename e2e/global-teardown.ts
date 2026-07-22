// global-teardown (Camada 2, briefing 0028): para o binario que o setup subiu e
// extrai a COBERTURA DO BINARIO exercida pelas jornadas de navegador.
//
// O binario foi buildado com -cover e roda com GOCOVERDIR: ele so grava os
// contadores ao SAIR. Por isso mandamos SIGTERM (o servidor faz graceful shutdown e
// retorna do main, gravando o perfil), esperamos o processo morrer de fato e so
// entao rodamos `go tool covdata` para o % e um perfil textfmt.
import { execSync } from 'node:child_process'
import { existsSync, readFileSync, writeFileSync } from 'node:fs'
import { ARTIFACTS, EstadoE2E, GOCOVERDIR, ROOT, STATE_FILE } from './lib/paths'

async function esperarMorte(pid: number, tentativas = 40): Promise<void> {
  for (let i = 0; i < tentativas; i++) {
    try {
      process.kill(pid, 0) // sonda: nao mata, so testa a existencia
    } catch {
      return // ESRCH: processo ja saiu
    }
    await new Promise((r) => setTimeout(r, 250))
  }
  console.warn(`[e2e] servidor pid=${pid} nao saiu a tempo; a cobertura pode ficar parcial`)
}

function coberturaBinario(): void {
  const perfil = `${ARTIFACTS}/e2e-binary-coverage.txt`
  try {
    execSync(`go tool covdata textfmt -i="${GOCOVERDIR}" -o="${perfil}"`, { cwd: ROOT })
    const porPacote = execSync(`go tool covdata percent -i="${GOCOVERDIR}"`, {
      cwd: ROOT,
      encoding: 'utf8',
    })
    // Total agregado (numero unico) a partir do perfil textfmt.
    const total = execSync(`go tool cover -func="${perfil}"`, { cwd: ROOT, encoding: 'utf8' })
      .trim()
      .split('\n')
      .filter((l) => l.startsWith('total:'))
      .pop()
    console.log('[e2e] cobertura do BINARIO exercida pelas jornadas de navegador:')
    console.log(porPacote.trim())
    if (total) console.log(`[e2e] ${total}`)
    console.log(`[e2e] perfil textfmt em ${perfil}`)
  } catch (e) {
    console.warn(`[e2e] nao foi possivel extrair a cobertura do binario: ${(e as Error).message}`)
  }
}

export default async function globalTeardown(): Promise<void> {
  if (!existsSync(STATE_FILE)) return
  const estado: EstadoE2E = JSON.parse(readFileSync(STATE_FILE, 'utf8'))
  if (!estado.gerenciado || !estado.serverPid) {
    console.log('[e2e] servidor externo — nada a parar nem cobertura a extrair')
    return
  }

  try {
    process.kill(estado.serverPid, 'SIGTERM')
  } catch {
    /* ja morto */
  }
  await esperarMorte(estado.serverPid)
  coberturaBinario()
  await coberturaFront()
}

// coberturaFront gera o relatorio do monocart a partir do cache que os workers
// alimentaram (só quando E2E_COVERAGE=1) e imprime o % de linhas do front.
async function coberturaFront(): Promise<void> {
  if (process.env.E2E_COVERAGE !== '1') return
  try {
    const { CoverageReport } = await import('monocart-coverage-reports')
    const { mcrOptions, COVERAGE_DIR } = await import('./lib/coverage')
    const resultado = await new CoverageReport(mcrOptions()).generate()
    const pct = resultado?.summary?.lines?.pct
    console.log('[e2e] cobertura do FRONT (jornadas de navegador):')
    console.log(`[e2e] linhas: ${typeof pct === 'number' ? pct + '%' : 'n/d'} — relatorio v8 em ${COVERAGE_DIR}`)
    // Persiste para a catraca (briefing 0068 - coletor collectE2EFrontCoverage
    // le este arquivo em vez de rodar o Playwright/monocart ele mesmo).
    if (typeof pct === 'number') {
      const destino = `${ARTIFACTS}/e2e-front-coverage.json`
      writeFileSync(destino, JSON.stringify({ linesPct: pct }), 'utf8')
      console.log(`[e2e] cobertura do front persistida em ${destino}`)
    }
  } catch (e) {
    console.warn(`[e2e] nao foi possivel gerar a cobertura do front: ${(e as Error).message}`)
  }
}
