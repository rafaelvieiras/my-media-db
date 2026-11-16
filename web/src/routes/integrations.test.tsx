import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import { IntegrationsListScreen } from '@/routes/integrations'
import type { IntegrationSummary } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson } from '@/test/detail-harness'

// Route `/dashboard/integrations`: lists the cards with the
// `configurada` badge reflecting EXACTLY what the API returned (no logic
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
  it('mostra o heading e o card do TMDB com badge "Não configurada"', async () => {
    vi.stubGlobal(
      'fetch',
      fetchRouter(() => okJson({ integrations: [integracao()] })),
    )
    renderWithClient(<IntegrationsListScreen />)

    expect(
      screen.getByRole('heading', { level: 1, name: 'Integrações' }),
    ).toBeInTheDocument()
    expect(await screen.findByText('TMDB')).toBeInTheDocument()
    expect(screen.getByText('The Movie Database.')).toBeInTheDocument()
    expect(screen.getByText('Não configurada')).toBeInTheDocument()

    const link = screen.getByRole('link', { name: /TMDB/ })
    expect(link).toHaveAttribute('href', '/dashboard/integrations/tmdb')
  })

  it('badge "Configurada" quando a API devolve configurada:true', async () => {
    vi.stubGlobal(
      'fetch',
      fetchRouter(() =>
        okJson({ integrations: [integracao({ configurada: true })] }),
      ),
    )
    renderWithClient(<IntegrationsListScreen />)

    expect(await screen.findByText('Configurada')).toBeInTheDocument()
    expect(screen.queryByText('Não configurada')).not.toBeInTheDocument()
  })

  it('estado vazio: "Nenhuma integração registrada" quando a lista vem vazia', async () => {
    vi.stubGlobal(
      'fetch',
      fetchRouter(() => okJson({ integrations: [] })),
    )
    renderWithClient(<IntegrationsListScreen />)

    expect(
      await screen.findByText('Nenhuma integração registrada'),
    ).toBeInTheDocument()
  })

  it('estado de erro mostra a mensagem pt-BR do envelope', async () => {
    vi.stubGlobal(
      'fetch',
      fetchRouter(() => erroJson(500, 'erro_interno', 'falhou de verdade')),
    )
    renderWithClient(<IntegrationsListScreen />)

    expect(
      await screen.findByText('Não foi possível carregar as integrações'),
    ).toBeInTheDocument()
    expect(screen.getByText('falhou de verdade')).toBeInTheDocument()
  })

  it('estado de carregando mostra o LoadingState antes da resposta', () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise(() => {})),
    )
    renderWithClient(<IntegrationsListScreen />)

    expect(screen.getByText('Carregando integrações…')).toBeInTheDocument()
  })
})
