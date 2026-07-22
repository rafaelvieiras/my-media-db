import { setupServer } from 'msw/node'
import { handlers } from './handlers'

// Servidor MSW (Node, Fase 3 — briefing 0068): intercepta a rede em jsdom com o
// contrato real. `server.use(...)` nos testes sobrepõe/estende os handlers
// PONTUALMENTE (ex.: um 502 numa jornada de erro) — `afterEach` (setup.ts)
// devolve aos handlers de base entre testes.
export const server = setupServer(...handlers)
