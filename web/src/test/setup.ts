// Setup global do vitest (jsdom). Importa os matchers do jest-dom
// (`toBeInTheDocument`, `toHaveTextContent`, …) para todos os testes.
import '@testing-library/jest-dom/vitest'
import { afterAll, afterEach, beforeAll } from 'vitest'
import { server } from './msw/server'

// jsdom não implementa ResizeObserver (embla-carousel, usado pelo Carousel do
// shadcn desde o briefing 0109, chama `new ResizeObserver(...)` ao montar) —
// stub no-op global para qualquer teste que renderize um <Carousel /> não
// estourar `ReferenceError: ResizeObserver is not defined`.
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
if (typeof globalThis.ResizeObserver === 'undefined') {
  globalThis.ResizeObserver =
    ResizeObserverStub as unknown as typeof ResizeObserver
}

// Idem para IntersectionObserver (embla usa para o `slidesInView`).
class IntersectionObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords(): IntersectionObserverEntry[] {
    return []
  }
}
if (typeof globalThis.IntersectionObserver === 'undefined') {
  globalThis.IntersectionObserver =
    IntersectionObserverStub as unknown as typeof IntersectionObserver
}

// jsdom também não implementa `window.matchMedia` (embla-carousel consulta as
// media queries das `opts` ao montar, mesmo sem nenhuma passada) — stub que
// devolve "não corresponde" para qualquer query, sem listener real (nenhum
// teste depende de mudança de media query em runtime).
if (typeof window !== 'undefined' && !window.matchMedia) {
  window.matchMedia = (query: string) =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    }) as MediaQueryList
}

// MSW (Fase 3, briefing 0068): liga o servidor para TODA a suíte. Rota não
// mockada estoura erro (`onUnhandledRequest: 'error'`) — mesma filosofia de
// "falha alto" do `stub-fetch.ts` atual, não fica silenciosa. Coexiste com os
// mocks de fetch por-teste (`criarFetch`/`stubRota`): estes não usam MSW, então
// não são afetados por este listener.
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => server.resetHandlers())
afterAll(() => server.close())
