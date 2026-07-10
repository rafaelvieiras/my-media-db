// global-setup (Camada 2, briefing 0028): prepara o alvo das jornadas.
//
// Reusa um servidor externo se E2E_BASE_URL estiver setado (iteracao rapida);
// senao builda o front, builda o binario com -cover, e o sobe com o provider fake
// (MYMEDIADB_FAKE_PROVIDER=1), banco/assets temporarios e GOCOVERDIR, esperando o
// /health responder. O pid vai para o state.json, que o teardown le para parar o
// servidor e extrair a cobertura do binario. E2E_SKIP_BUILD=1 pula os builds.
import { execSync, spawn } from 'node:child_process'
import { existsSync, mkdirSync, openSync, rmSync, writeFileSync } from 'node:fs'
import {
  ARTIFACTS,
  ASSETS_DIR,
  BASE_URL,
  BIN,
  DB_PATH,
  EstadoE2E,
  GOCOVERDIR,
  PORT,
  ROOT,
  STATE_FILE,
  WEB,
} from './lib/paths'

async function esperarSaude(url: string, tentativas = 60): Promise<void> {
  for (let i = 0; i < tentativas; i++) {
    try {
      const r = await fetch(`${url}/api/v1/health`)
      if (r.ok) return
    } catch {
      /* servidor ainda subindo */
    }
    await new Promise((r) => setTimeout(r, 500))
  }
  throw new Error(`servidor nao respondeu /health em ${url} (timeout)`)
}

function escrever(estado: EstadoE2E): void {
  writeFileSync(STATE_FILE, JSON.stringify(estado, null, 2))
}

export default async function globalSetup(): Promise<void> {
  mkdirSync(ARTIFACTS, { recursive: true })

  // Servidor externo ja no ar: so registra e sai (sem builds, sem cobertura).
  if (process.env.E2E_BASE_URL) {
    console.log(`[e2e] reusando servidor externo em ${BASE_URL}`)
    await esperarSaude(BASE_URL)
    escrever({ baseURL: BASE_URL, gerenciado: false })
    return
  }

  // Estado limpo por execucao: banco novo, assets e GOCOVERDIR vazios.
  for (const dir of [DB_PATH, `${DB_PATH}-wal`, `${DB_PATH}-shm`]) {
    if (existsSync(dir)) rmSync(dir)
  }
  rmSync(ASSETS_DIR, { recursive: true, force: true })
  rmSync(GOCOVERDIR, { recursive: true, force: true })
  mkdirSync(ASSETS_DIR, { recursive: true })
  mkdirSync(GOCOVERDIR, { recursive: true })

  const coletarFront = process.env.E2E_COVERAGE === '1'
  if (coletarFront) {
    // Limpa o cache do monocart e propaga o flag ao build do vite (sourcemap inline).
    const { CoverageReport } = await import('monocart-coverage-reports')
    const { mcrOptions } = await import('./lib/coverage')
    await new CoverageReport(mcrOptions()).cleanCache()
  }

  if (process.env.E2E_SKIP_BUILD !== '1') {
    console.log(`[e2e] buildando o front (npm run build${coletarFront ? ', com sourcemap' : ''})...`)
    execSync('npm run build', {
      cwd: WEB,
      stdio: 'inherit',
      env: { ...process.env, E2E_COVERAGE: coletarFront ? '1' : '' },
    })
    console.log('[e2e] buildando o binario com -cover...')
    execSync(`go build -cover -o "${BIN}" ./cmd/mymediadb`, { cwd: ROOT, stdio: 'inherit' })
  }

  console.log(`[e2e] subindo o binario (fake provider) em ${BASE_URL}...`)
  const log = openSync(`${ARTIFACTS}/server.log`, 'w')
  const child = spawn(BIN, [], {
    cwd: ROOT,
    env: {
      ...process.env,
      GOCOVERDIR,
      MYMEDIADB_FAKE_PROVIDER: '1',
      PORT,
      MYMEDIADB_DB_PATH: DB_PATH,
      MYMEDIADB_ASSETS_DIR: ASSETS_DIR,
    },
    stdio: ['ignore', log, log],
  })
  child.unref()

  await esperarSaude(BASE_URL)
  console.log(`[e2e] servidor pronto (pid=${child.pid})`)
  escrever({ serverPid: child.pid, baseURL: BASE_URL, gerenciado: true })
}
