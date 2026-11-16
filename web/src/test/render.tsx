import { render } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'

// Shared test helper: renders the UI inside its own QueryClient
// with retries turned off (the error state appears immediately, without backoff).
// Centralized in src/test so the tests do not duplicate the wrapper (jscpd).
export function renderWithClient(ui: ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>,
  )
}
