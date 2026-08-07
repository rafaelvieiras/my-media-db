import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { LibraryNewScreen } from '@/routes/library-new'
import { renderWithClient } from '@/test/render'
import { okJson } from '@/test/entity-fixtures'
import { erroJson } from '@/test/detail-harness'

// Rota `/dashboard/library/new` (briefing 0071): campo de caminho + PathBrowser + Select
// de tipo + submit (`createLibrary`). Sucesso navega para `/dashboard/library`; erro
// (`ApiError`, ex. 422 path inválido/duplicado) aparece inline.
const spies = vi.hoisted(() => ({
  navigate: vi.fn(),
  back: vi.fn(),
  params: {},
}))

vi.mock('@tanstack/react-router', async () => {
  const { routerMock } = await import('@/test/detail-harness')
  return routerMock(spies)
})

// fetchRouter roteia a listagem de pastas (GET /library/browse) e a criação
// (POST /library) — a única rota deste teste que precisa de ambas ao mesmo
// tempo (as demais telas do projeto só criam um recurso simples).
function fetchRouter(opts: {
  createResp?: () => Response
  browse?: Record<string, unknown>
  retryDaysGlobal?: number
}) {
  return vi.fn(async (url: string, init?: RequestInit) => {
    const u = new URL(String(url), 'http://localhost')
    const method = init?.method ?? 'GET'
    if (u.pathname === '/api/v1/library/browse') {
      const path = u.searchParams.get('path') ?? '/'
      const resultado = opts.browse?.[path]
      if (!resultado) {
        throw new Error(`path de browse não mockado no teste: ${path}`)
      }
      return okJson(resultado)
    }
    if (
      u.pathname === '/api/v1/settings/enrichment-retroativo' &&
      method === 'GET'
    ) {
      return okJson({ retry_days: opts.retryDaysGlobal ?? 15 })
    }
    if (u.pathname === '/api/v1/library' && method === 'POST') {
      return opts.createResp
        ? opts.createResp()
        : ({
            ok: true,
            status: 201,
            json: async () => ({ id: 's1', path: '/mnt/filmes' }),
          } as Response)
    }
    throw new Error(`rota não mockada: ${method} ${u.pathname}`)
  })
}

afterEach(() => {
  vi.restoreAllMocks()
  spies.navigate.mockReset()
})

// Clica em "Adicionar datasource", espera a navegação de sucesso e devolve o
// corpo (já parseado) do POST /library — os testes de submit só variam no que
// preencheram antes e no que checam depois deste ponto em comum.
async function clicarAdicionarEPegarCorpo(
  user: ReturnType<typeof userEvent.setup>,
  fetchMock: ReturnType<typeof fetchRouter>,
) {
  await user.click(screen.getByRole('button', { name: /Adicionar datasource/ }))

  await waitFor(() =>
    expect(spies.navigate).toHaveBeenCalledWith({ to: '/dashboard/library' }),
  )
  const chamada = fetchMock.mock.calls.find(
    ([u, init]) =>
      String(u) === '/api/v1/library' &&
      (init as RequestInit | undefined)?.method === 'POST',
  )
  expect(chamada).toBeDefined()
  return JSON.parse(String(chamada?.[1]?.body))
}

describe('LibraryNewScreen', () => {
  it('cria o datasource (caminho + tipo) e navega para /dashboard/library', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({})
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<LibraryNewScreen />)

    await user.type(
      screen.getByLabelText('Caminho do datasource'),
      '/mnt/series',
    )
    await user.selectOptions(screen.getByLabelText('Tipo do datasource'), 'tv')
    const corpo = await clicarAdicionarEPegarCorpo(user, fetchMock)
    expect(corpo).toEqual({
      path: '/mnt/series',
      type: 'tv',
      content_type: 'series',
    })
  })

  it('não submete com caminho vazio', async () => {
    const user = userEvent.setup()
    vi.stubGlobal('fetch', fetchRouter({}))
    renderWithClient(<LibraryNewScreen />)

    await user.click(
      screen.getByRole('button', { name: /Adicionar datasource/ }),
    )

    expect(spies.navigate).not.toHaveBeenCalled()
  })

  it('mostra o erro 422 inline e não navega', async () => {
    const user = userEvent.setup()
    vi.stubGlobal(
      'fetch',
      fetchRouter({
        createResp: () =>
          erroJson(422, 'invalid_path', 'o caminho informado não existe'),
      }),
    )
    renderWithClient(<LibraryNewScreen />)

    await user.type(screen.getByLabelText('Caminho do datasource'), '/nao/ha')
    await user.click(
      screen.getByRole('button', { name: /Adicionar datasource/ }),
    )

    expect(
      await screen.findByText('o caminho informado não existe'),
    ).toBeInTheDocument()
    expect(spies.navigate).not.toHaveBeenCalled()
  })

  it('mostra o placeholder com o padrão global e cria com override de reentativa', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({ retryDaysGlobal: 20 })
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<LibraryNewScreen />)

    const input = await screen.findByLabelText(
      'Reentativa de enriquecimento (dias) do datasource',
    )
    await waitFor(() =>
      expect(input).toHaveAttribute(
        'placeholder',
        'Usar padrão global (20 dias)',
      ),
    )

    await user.type(
      screen.getByLabelText('Caminho do datasource'),
      '/mnt/series',
    )
    await user.type(input, '7')
    const corpo = await clicarAdicionarEPegarCorpo(user, fetchMock)
    expect(corpo.retroactive_enrichment_days).toBe(7)
  })

  it('abre o PathBrowser atrás de "Procurar pasta…" e usa a pasta escolhida', async () => {
    const user = userEvent.setup()
    vi.stubGlobal(
      'fetch',
      fetchRouter({
        browse: {
          '/': {
            path: '/',
            parent: null,
            entries: [{ name: 'mnt', path: '/mnt', readable: true }],
            truncated: false,
          },
        },
      }),
    )
    renderWithClient(<LibraryNewScreen />)

    await user.click(screen.getByRole('button', { name: 'Procurar pasta…' }))
    await screen.findByText('mnt')
    await user.click(screen.getByRole('button', { name: 'Usar esta pasta' }))

    expect(screen.getByLabelText('Caminho do datasource')).toHaveValue('/')
  })
})
