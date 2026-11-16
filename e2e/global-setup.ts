// global-setup (Layer 2): prepares the target of the journeys.
//
// It reuses an external server if E2E_BASE_URL is set (fast iteration); otherwise
// it builds the front end, builds the binary with -cover, and starts it with the
// fake provider (MYMEDIADB_FAKE_PROVIDER=1), temporary database/assets and
// GOCOVERDIR, waiting for /health to answer. The pid goes into state.json, which
// the teardown reads to stop the server and extract the binary coverage.
// E2E_SKIP_BUILD=1 skips the builds.
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
      /* server still starting up */
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

  // External server already up: just record it and leave (no builds, no coverage).
  if (process.env.E2E_BASE_URL) {
    console.log(`[e2e] reusando servidor externo em ${BASE_URL}`)
    await esperarSaude(BASE_URL)
    escrever({ baseURL: BASE_URL, gerenciado: false })
    return
  }

  // Clean state per run: fresh database, empty assets and GOCOVERDIR.
  for (const dir of [DB_PATH, `${DB_PATH}-wal`, `${DB_PATH}-shm`]) {
    if (existsSync(dir)) rmSync(dir)
  }
  rmSync(ASSETS_DIR, { recursive: true, force: true })
  rmSync(GOCOVERDIR, { recursive: true, force: true })
  mkdirSync(ASSETS_DIR, { recursive: true })
  mkdirSync(GOCOVERDIR, { recursive: true })

  const coletarFront = process.env.E2E_COVERAGE === '1'
  if (coletarFront) {
    // Clears the monocart cache and propagates the flag to the vite build
    // (inline sourcemap).
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
