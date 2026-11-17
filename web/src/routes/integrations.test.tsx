import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import { IntegrationsListScreen } from '@/routes/integrations'
import type { IntegrationSummary } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson } from '@/test/detail-harness'

// Route `/dashboard/integrations`: lists the cards with the
// `configured` badge reflecting EXACTLY what the API returned (no logic
// of its own in the front, acceptance criterion §5).
vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock,
}))

function integracao(
  over: Partial<IntegrationSummary> = {},
): IntegrationSummary {
  return {
    slug: 'tmdb',
    nome: 'TMDB',
    descricao: 'The Movie Database.',
    configurada: false,
    ...over,
  }
}

function fetchRouter(resp: () => Response) {
  return vi.fn(async (url: string) => {
    if (String(url) === '/api/v1/integrations') {
      return resp()
    }
    throw new Error(`rota nao mockada: ${String(url)}`)
  })
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('IntegrationsListScreen', () => {
  it('shows the heading and the TMDB card with the "Not configured" badge', async () => {
    vi.stubGlobal(
      'fetch',
      fetchRouter(() => okJson({ integrations: [integracao()] })),
    )
    renderWithClient(<IntegrationsListScreen />)

    expect(
      screen.getByRole('heading', { level: 1, name: 'Integrations' }),
    ).toBeInTheDocument()
    expect(await screen.findByText('TMDB')).toBeInTheDocument()
    expect(screen.getByText('The Movie Database.')).toBeInTheDocument()
    expect(screen.getByText('Not configured')).toBeInTheDocument()

    const link = screen.getByRole('link', { name: /TMDB/ })
    expect(link).toHaveAttribute('href', '/dashboard/integrations/tmdb')
  })

  it('shows the "Configured" badge when the API returns configurada:true', async () => {
    vi.stubGlobal(
      'fetch',
      fetchRouter(() =>
        okJson({ integrations: [integracao({ configurada: true })] }),
      ),
    )
    renderWithClient(<IntegrationsListScreen />)

    expect(await screen.findByText('Configured')).toBeInTheDocument()
    expect(screen.queryByText('Not configured')).not.toBeInTheDocument()
  })

  it('empty state: "No integration registered" when the list comes back empty', async () => {
    vi.stubGlobal(
      'fetch',
      fetchRouter(() => okJson({ integrations: [] })),
    )
    renderWithClient(<IntegrationsListScreen />)

    expect(
      await screen.findByText('No integration registered'),
    ).toBeInTheDocument()
  })

  it('error state shows the message of the envelope', async () => {
    vi.stubGlobal(
      'fetch',
      fetchRouter(() => erroJson(500, 'erro_interno', 'falhou de verdade')),
    )
    renderWithClient(<IntegrationsListScreen />)

    expect(
      await screen.findByText('Could not load the integrations'),
    ).toBeInTheDocument()
    expect(screen.getByText('falhou de verdade')).toBeInTheDocument()
  })

  it('loading state shows the LoadingState before the response', () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise(() => {})),
    )
    renderWithClient(<IntegrationsListScreen />)

    expect(screen.getByText('Loading integrations…')).toBeInTheDocument()
  })
})
