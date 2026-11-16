// Paths and constants shared between the config, the global-setup and the
// global-teardown (Layer 2). Everything is derived from the repo root.
import { join } from 'node:path'

// Repository ROOT (e2e/ lives one level below).
export const ROOT = join(__dirname, '..', '..')
export const WEB = join(ROOT, 'web')

// Directory of ephemeral orchestration artifacts (gitignored).
export const ARTIFACTS = join(ROOT, 'e2e', '.artifacts')
export const BIN = join(ARTIFACTS, 'mymediadb')
export const DB_PATH = join(ARTIFACTS, 'e2e.sqlite')
export const ASSETS_DIR = join(ARTIFACTS, 'assets')
// GOCOVERDIR: where the -cover binary writes the counters; go tool covdata reads
// from here.
export const GOCOVERDIR = join(ARTIFACTS, 'covdata')
// State passed from the setup to the teardown (server pid etc.).
export const STATE_FILE = join(ARTIFACTS, 'state.json')

// Fixed port (or E2E_PORT) — the config's baseURL needs it BEFORE the setup runs.
export const PORT = process.env.E2E_PORT || '8799'
// baseURL: an external server already up (E2E_BASE_URL) takes precedence; otherwise
// the setup starts the binary on this local port.
export const BASE_URL = process.env.E2E_BASE_URL || `http://127.0.0.1:${PORT}`

// State serialized between setup and teardown.
export interface EstadoE2E {
  // pid of the binary the setup started; absent when we reuse an external server.
  serverPid?: number
  baseURL: string
  // true when the setup started the -cover binary (the teardown extracts the
  // coverage).
  gerenciado: boolean
}
