import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { IntegrationDetailScreen } from '@/routes/integration-detail'
import type { IntegrationDetail } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson } from '@/test/detail-harness'

// Rota `/dashboard/integrations/$slug` (briefing 0096): carrega o schema+
// valor efetivo e monta o `IntegrationForm`, que reseeda os PRÓPRIOS campos
// (sem desmontar) a partir do eco do PUT após uma gravação bem-sucedida —
// nunca do valor de um campo secreto, que o eco também nunca carrega.
const spies = vi.hoisted(() => ({
  navigate: vi.fn(),
  back: vi.fn(),
  params: { slug: 'tmdb' } as Record<string, string>,
}))

vi.mock('@tanstack/react-router', async () => {
  const { routerMock } = await import('@/test/detail-harness')
  return routerMock(spies)
})

function detalhe(over: Partial<IntegrationDetail> = {}): IntegrationDetail {
  return {
    slug: 'tmdb',
    nome: 'TMDB',
    descricao: 'The Movie Database.',
    campos: [
      {
        nome: 'token',
        label: 'Read Access Token (v4)',
        tipo: 'secret',
        obrigatorio: false,
        definido: false,
        origem: 'nenhum',
      },
      {
        nome: 'api_key',
        label: 'API Key (v3)',
        tipo: 'secret',
        obrigatorio: false,
        definido: false,
        origem: 'nenhum',
      },
    ],
    ...over,
  }
}

function fetchRouter(opts: {
  detalheInicial?: IntegrationDetail
  putResposta?: (body: unknown) => Response
  getErro?: { status: number; code: string; message: string }
}) {
  let atual = opts.detalheInicial ?? detalhe()
  return vi.fn(async (url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'
    if (u === '/api/v1/integrations/tmdb' && method === 'GET') {
      if (opts.getErro) {
        return erroJson(
          opts.getErro.status,
          opts.getErro.code,
          opts.getErro.message,
        )
      }
      return okJson(atual)
    }
    if (u === '/api/v1/integrations/tmdb' && method === 'PUT') {
      const body = JSON.parse(String(init?.body))
      if (opts.putResposta) {
        return opts.putResposta(body)
      }
      // Aplica o diff (mesma semântica do backend: null remove, presença seta)
      // pra simular o eco pós-gravação e o refetch subsequente.
      const campos = atual.campos.map((c) => {
        if (!(c.nome in body.valores)) return c
        const novoValor = body.valores[c.nome]
        if (novoValor === null) {
          return { ...c, definido: false, origem: 'nenhum' as const }
        }
        return { ...c, definido: true, origem: 'banco' as const }
      })
      atual = { ...atual, campos }
      return okJson(atual)
    }
    throw new Error(`rota nao mockada: ${method} ${u}`)
  })
}

afterEach(() => {
  vi.restoreAllMocks()
  spies.navigate.mockReset()
  spies.params = { slug: 'tmdb' }
})

describe('IntegrationDetailScreen', () => {
  it('mostra o heading/descrição e os 2 campos do TMDB', async () => {
    vi.stubGlobal('fetch', fetchRouter({}))
    renderWithClient(<IntegrationDetailScreen />)

    expect(
      await screen.findByRole('heading', { level: 1, name: 'TMDB' }),
    ).toBeInTheDocument()
    expect(screen.getByText('The Movie Database.')).toBeInTheDocument()
    expect(screen.getByLabelText('Read Access Token (v4)')).toBeInTheDocument()
    expect(screen.getByLabelText('API Key (v3)')).toBeInTheDocument()
  })

  it('estado de carregando antes da resposta', () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise(() => {})),
    )
    renderWithClient(<IntegrationDetailScreen />)
    expect(screen.getByText('Carregando integração…')).toBeInTheDocument()
  })

  it('slug desconhecido (404) mostra o ErrorState com a mensagem pt-BR', async () => {
    vi.stubGlobal(
      'fetch',
      fetchRouter({
        getErro: {
          status: 404,
          code: 'not_found',
          message: 'integração desconhecida',
        },
      }),
    )
    renderWithClient(<IntegrationDetailScreen />)

    expect(
      await screen.findByText('Não foi possível carregar a integração'),
    ).toBeInTheDocument()
    expect(screen.getByText('integração desconhecida')).toBeInTheDocument()
  })

  it('salvar um campo: o refetch reflete origem "banco" e o campo secreto continua vazio', async () => {
    const user = userEvent.setup()
    vi.stubGlobal('fetch', fetchRouter({}))
    renderWithClient(<IntegrationDetailScreen />)

    const campoToken = await screen.findByLabelText('Read Access Token (v4)')
    await user.type(campoToken, 'novo-token')
    await user.click(screen.getByRole('button', { name: 'Salvar' }))

    await waitFor(() =>
      expect(screen.getByText('Definido no painel.')).toBeInTheDocument(),
    )
    // Critério de aceite: NUNCA reidrata o campo secreto com o valor digitado.
    expect(screen.getByLabelText('Read Access Token (v4)')).toHaveValue('')
  })
})
