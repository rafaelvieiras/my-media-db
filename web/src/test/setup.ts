// Setup global do vitest (jsdom). Importa os matchers do jest-dom
// (`toBeInTheDocument`, `toHaveTextContent`, …) para todos os testes.
import '@testing-library/jest-dom/vitest'
import { afterAll, afterEach, beforeAll } from 'vitest'
import { server } from './msw/server'

// MSW (Fase 3, briefing 0068): liga o servidor para TODA a suíte. Rota não
// mockada estoura erro (`onUnhandledRequest: 'error'`) — mesma filosofia de
// "falha alto" do `stub-fetch.ts` atual, não fica silenciosa. Coexiste com os
// mocks de fetch por-teste (`criarFetch`/`stubRota`): estes não usam MSW, então
// não são afetados por este listener.
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => server.resetHandlers())
afterAll(() => server.close())
