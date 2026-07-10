// Caminhos e constantes compartilhados entre a config, o global-setup e o
// global-teardown (briefing 0028, Camada 2). Tudo derivado da raiz do repo.
import { join } from 'node:path'

// RAIZ do repositorio (o e2e/ mora um nivel abaixo).
export const ROOT = join(__dirname, '..', '..')
export const WEB = join(ROOT, 'web')

// Diretorio de artefatos efemeros da orquestracao (gitignored).
export const ARTIFACTS = join(ROOT, 'e2e', '.artifacts')
export const BIN = join(ARTIFACTS, 'mymediadb')
export const DB_PATH = join(ARTIFACTS, 'e2e.sqlite')
export const ASSETS_DIR = join(ARTIFACTS, 'assets')
// GOCOVERDIR: onde o binario -cover grava os contadores; go tool covdata le daqui.
export const GOCOVERDIR = join(ARTIFACTS, 'covdata')
// Estado passado do setup para o teardown (pid do servidor etc.).
export const STATE_FILE = join(ARTIFACTS, 'state.json')

// Porta fixa (ou E2E_PORT) — a baseURL da config precisa dela ANTES do setup rodar.
export const PORT = process.env.E2E_PORT || '8799'
// baseURL: um servidor externo ja no ar (E2E_BASE_URL) tem precedencia; senao o
// setup sobe o binario nesta porta local.
export const BASE_URL = process.env.E2E_BASE_URL || `http://127.0.0.1:${PORT}`

// Estado serializado entre setup e teardown.
export interface EstadoE2E {
  // pid do binario que o setup subiu; ausente quando reusamos servidor externo.
  serverPid?: number
  baseURL: string
  // true quando o setup subiu o binario -cover (o teardown extrai a cobertura).
  gerenciado: boolean
}
