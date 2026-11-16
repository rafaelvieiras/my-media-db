import { setupServer } from 'msw/node'
import { handlers } from './handlers'

// MSW server (Node, Phase 3): it intercepts the network in jsdom with the
// real contract. `server.use(...)` in the tests overrides/extends the handlers
// POINTWISE (e.g. a 502 in an error journey) — `afterEach` (setup.ts)
// restores the base handlers between tests.
export const server = setupServer(...handlers)
