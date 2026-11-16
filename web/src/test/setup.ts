// Global setup of vitest (jsdom). It imports the jest-dom matchers
// (`toBeInTheDocument`, `toHaveTextContent`, …) for all the tests.
import '@testing-library/jest-dom/vitest'
import { afterAll, afterEach, beforeAll } from 'vitest'
import { server } from './msw/server'

// jsdom does not implement ResizeObserver (embla-carousel, used by the Carousel of
// shadcn, calls `new ResizeObserver(...)` on mount) —
// a global no-op stub so that any test rendering a <Carousel /> does not
// blow up with `ReferenceError: ResizeObserver is not defined`.
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
if (typeof globalThis.ResizeObserver === 'undefined') {
  globalThis.ResizeObserver =
    ResizeObserverStub as unknown as typeof ResizeObserver
}

// Same for IntersectionObserver (embla uses it for `slidesInView`).
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

// jsdom also does not implement `window.matchMedia` (embla-carousel queries the
// media queries of the `opts` on mount, even without any slide) — a stub that
// returns "does not match" for any query, without a real listener (no
// test depends on a media query change at runtime).
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

// MSW (Phase 3): it turns the server on for the WHOLE suite. An unmocked route
// throws an error (`onUnhandledRequest: 'error'`) — the same "fail loud"
// philosophy of the current `stub-fetch.ts`, it does not stay silent. It coexists with the
// per-test fetch mocks (`criarFetch`/`stubRota`): those do not use MSW, so
// they are not affected by this listener.
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => server.resetHandlers())
afterAll(() => server.close())
