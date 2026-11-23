import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { IntegrationDetailScreen } from '@/routes/integration-detail'
import type { IntegrationDetail } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson } from '@/test/detail-harness'

// The "leave without saving" guard is the router blocker: outside a
// RouterProvider it cannot run, so it is mocked here (the hook has its own test and
// each screen asserts the argument it passes).
vi.mock('@/lib/use-unsaved-guard', () => ({ useUnsavedGuard: vi.fn() }))

// Route `/dashboard/integrations/$slug`: it loads the schema+
// effective value and mounts the `IntegrationForm`, which reseeds ITS OWN fields
// (without unmounting) from the PUT echo after a successful write —
// never from the value of a secret field, which the echo never carries either.
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
      // Applies the diff (same semantics as the backend: null removes, presence sets)
      // to simulate the post-write echo and the subsequent refetch.
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

// `FormSaveFooter` is a shared component outside this domain (its own extraction
// lands in parallel), so the submit label is accepted in either language.
const BOTAO_SALVAR = /^(Save|Salvar)$/

afterEach(() => {
  vi.restoreAllMocks()
  spies.navigate.mockReset()
  spies.params = { slug: 'tmdb' }
})

describe('IntegrationDetailScreen', () => {
  it('shows the heading/description and the 2 TMDB fields', async () => {
    vi.stubGlobal('fetch', fetchRouter({}))
    renderWithClient(<IntegrationDetailScreen />)

    expect(
      await screen.findByRole('heading', { level: 1, name: 'TMDB' }),
    ).toBeInTheDocument()
    expect(screen.getByText('The Movie Database.')).toBeInTheDocument()
    expect(screen.getByLabelText('Read Access Token (v4)')).toBeInTheDocument()
    expect(screen.getByLabelText('API Key (v3)')).toBeInTheDocument()
  })

  it('loading state before the response', () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise(() => {})),
    )
    renderWithClient(<IntegrationDetailScreen />)
    expect(screen.getByText('Loading integration…')).toBeInTheDocument()
  })

  it('unknown slug (404) shows the ErrorState with the backend message', async () => {
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
      await screen.findByText('Could not load the integration'),
    ).toBeInTheDocument()
    expect(screen.getByText('integração desconhecida')).toBeInTheDocument()
  })

  it('saving a field: the refetch reflects origem "banco" and the secret field stays empty', async () => {
    const user = userEvent.setup()
    vi.stubGlobal('fetch', fetchRouter({}))
    renderWithClient(<IntegrationDetailScreen />)

    const campoToken = await screen.findByLabelText('Read Access Token (v4)')
    await user.type(campoToken, 'novo-token')
    await user.click(screen.getByRole('button', { name: BOTAO_SALVAR }))

    await waitFor(() =>
      expect(screen.getByText('Set in the panel.')).toBeInTheDocument(),
    )
    // Acceptance criterion: it NEVER rehydrates the secret field with the typed value.
    expect(screen.getByLabelText('Read Access Token (v4)')).toHaveValue('')
  })
})
