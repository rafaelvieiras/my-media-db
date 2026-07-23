import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { LibraryNewScreen } from '@/routes/library-new'
import { renderWithClient } from '@/test/render'
import { okJson } from '@/test/entity-fixtures'
import { erroJson } from '@/test/detail-harness'

// Rota `/library/new` (briefing 0071): campo de caminho + PathBrowser + Select
// de tipo + submit (`createLibrary`). Sucesso navega para `/library`; erro
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

describe('LibraryNewScreen', () => {
  it('cria o datasource (caminho + tipo) e navega para /library', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({})
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<LibraryNewScreen />)

    await user.type(
      screen.getByLabelText('Caminho do datasource'),
      '/mnt/series',
    )
    await user.selectOptions(screen.getByLabelText('Tipo do datasource'), 'tv')
    await user.click(
      screen.getByRole('button', { name: /Adicionar datasource/ }),
    )

    await waitFor(() =>
      expect(spies.navigate).toHaveBeenCalledWith({ to: '/library' }),
    )
    const chamada = fetchMock.mock.calls.find(
      ([u, init]) =>
        String(u) === '/api/v1/library' &&
        (init as RequestInit | undefined)?.method === 'POST',
    )
    expect(chamada).toBeDefined()
    const corpo = JSON.parse(String(chamada?.[1]?.body))
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
