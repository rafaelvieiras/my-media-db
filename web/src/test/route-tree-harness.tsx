import { render } from '@testing-library/react'
import { createMemoryHistory, RouterProvider } from '@tanstack/react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { router } from '@/router'
import { LanguageProvider } from '@/lib/language-context'

// Harness da Fase 3 (MSW + jsdom, briefing 0068): monta a ÁRVORE DE ROTA REAL do
// app (o `router` de src/router.tsx) num `memoryHistory` apontando para um path
// inicial, dentro de um QueryClient PRÓPRIO do teste — mesmo espírito de
// `renderComEspioes` (harness.tsx), mas para a árvore inteira em vez de um
// componente isolado. A rede é mockada pelo MSW (src/test/msw); nenhuma rota bate
// na rede de verdade — rota não mockada estoura erro (setup.ts,
// `onUnhandledRequest: 'error'`).
//
// O `router` é um SINGLETON (module de src/router.tsx); em vez de reconstruir a
// árvore, cada chamada faz `router.update(...)` para trocar o histórico (fresco,
// isolado por teste) e o contexto/Wrap (QueryClient fresco do teste, com retries
// desligados — o erro aparece na hora). O `Wrap` original (definido em
// router.tsx) fecha sobre o QueryClient SINGLETON do app; aqui substituímos por
// um Wrap equivalente fechando sobre o QueryClient do teste, para o cache lido
// pelos loaders/hooks (via `context.queryClient`) ser o MESMO do
// `QueryClientProvider` que envolve a árvore renderizada.
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
        <LanguageProvider>{children}</LanguageProvider>
      </QueryClientProvider>
    ),
  })

  // Resolve a navegação/os matches ANTES de montar — evita flakiness do
  // primeiro render pego num estado de rota ainda pendente.
  await router.load()

  render(<RouterProvider router={router} />)

  return { router, queryClient }
}
