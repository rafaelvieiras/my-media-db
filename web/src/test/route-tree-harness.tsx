import { render } from '@testing-library/react'
import { createMemoryHistory, RouterProvider } from '@tanstack/react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { router } from '@/router'
import { I18nProvider } from '@/lib/i18n'
import { LanguageProvider } from '@/lib/language-context'

// Phase 3 harness (MSW + jsdom): it mounts the REAL ROUTE TREE of the
// app (the `router` from src/router.tsx) on a `memoryHistory` pointing at an
// initial path, inside a QueryClient of the TEST — same spirit as
// `renderComEspioes` (harness.tsx), but for the whole tree instead of an
// isolated component. The network is mocked by MSW (src/test/msw); no route hits
// the real network — an unmocked route throws an error (setup.ts,
// `onUnhandledRequest: 'error'`).
//
// The `router` is a SINGLETON (module of src/router.tsx); instead of rebuilding the
// tree, each call does `router.update(...)` to swap the history (fresh,
// isolated per test) and the context/Wrap (fresh QueryClient of the test, with retries
// turned off — the error appears right away). The original `Wrap` (defined in
// router.tsx) closes over the SINGLETON QueryClient of the app; here we replace it with
// an equivalent Wrap closing over the QueryClient of the test, so that the cache read
// by the loaders/hooks (via `context.queryClient`) is the SAME as the
// `QueryClientProvider` that wraps the rendered tree.
export async function renderRota(pathInicial: string) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const history = createMemoryHistory({ initialEntries: [pathInicial] })

  router.update({
    history,
    context: { queryClient },
    Wrap: ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={queryClient}>
        <I18nProvider>
          <LanguageProvider>{children}</LanguageProvider>
        </I18nProvider>
      </QueryClientProvider>
    ),
  })

  // It resolves the navigation/matches BEFORE mounting — avoids flakiness of the
  // first render caught in a still pending route state.
  await router.load()

  render(<RouterProvider router={router} />)

  return { router, queryClient }
}
