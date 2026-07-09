import { render } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'

// Helper de teste compartilhado: renderiza a UI dentro de um QueryClient próprio
// com retries desligados (o estado de erro aparece imediatamente, sem backoff).
// Centralizado em src/test para os testes não duplicarem o wrapper (jscpd).
export function renderWithClient(ui: ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>,
  )
}
