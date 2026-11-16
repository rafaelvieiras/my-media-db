import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { LibraryEditScreen } from '@/routes/library-edit'
import type { Source } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { okJson } from '@/test/entity-fixtures'
import { erroJson } from '@/test/detail-harness'

// Route `/library/$id/edit`: it preloads the current path (via
// `libraryQueryOptions`), saves via PATCH (the path re-basing) and concentrates the
// removal in a "danger zone" with a 2-step confirmation (`alertdialog`,
// same pattern as `/orphans`).
const spies = vi.hoisted(() => ({
  navigate: vi.fn(),
  back: vi.fn(),
  params: { id: 's1' } as Record<string, string>,
}))

vi.mock('@tanstack/react-router', async () => {
  const { routerMock } = await import('@/test/detail-harness')
  return routerMock(spies)
})

function source(over: Partial<Source> = {}): Source {
  return {
    id: 's1',
    path: '/mnt/atual',
    type: 'movie',
    content_type: 'movie',
    state: 'online',
    last_scan: null,
    watch_enabled: false,
    ...over,
  }
}

function fetchRouter(opts: {
  fonte?: Source
  patchResp?: () => Response
  deleteResp?: () => Response
  retryDaysGlobal?: number
}) {
  const items = opts.fonte ? [opts.fonte] : []
  return vi.fn(async (url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'
    if (u === '/api/v1/library' && method === 'GET') {
      return okJson({ items })
    }
    if (u === '/api/v1/settings/enrichment-retroativo' && method === 'GET') {
      return okJson({ retry_days: opts.retryDaysGlobal ?? 15 })
    }
    if (u === '/api/v1/library/s1' && method === 'PATCH') {
      return opts.patchResp ? opts.patchResp() : okJson({ ...items[0] })
    }
    if (u === '/api/v1/library/s1' && method === 'DELETE') {
      return opts.deleteResp
        ? opts.deleteResp()
        : ({ ok: true, status: 204 } as Response)
    }
    throw new Error(`rota não mockada: ${method} ${u}`)
  })
}

afterEach(() => {
  vi.restoreAllMocks()
  spies.navigate.mockReset()
})

// chamouDelete checks whether the fetch mock has already received a DELETE on /library/s1.
function chamouDelete(fetchMock: ReturnType<typeof fetchRouter>): boolean {
  return fetchMock.mock.calls.some(
    ([u, init]) =>
      String(u) === '/api/v1/library/s1' &&
      (init as RequestInit | undefined)?.method === 'DELETE',
  )
}

// Clicks "Salvar", waits for the success navigation and returns the body (already
// parsed) of PATCH /library/s1 — the submit tests only vary in what they
// filled in before and in what they check after this common point.
async function clicarSalvarEPegarCorpo(
  user: ReturnType<typeof userEvent.setup>,
  fetchMock: ReturnType<typeof fetchRouter>,
) {
  await user.click(screen.getByRole('button', { name: 'Salvar' }))

  await waitFor(() =>
    expect(spies.navigate).toHaveBeenCalledWith({ to: '/dashboard/library' }),
  )
  const chamada = fetchMock.mock.calls.find(
    ([u, init]) =>
      String(u) === '/api/v1/library/s1' &&
      (init as RequestInit | undefined)?.method === 'PATCH',
  )
  return JSON.parse(String(chamada?.[1]?.body))
}

// abrirZonaDePerigo mounts the screen, clicks "Remover datasource" and returns
// `user`/`fetchMock` for the test to continue (confirm or cancel).
async function abrirZonaDePerigo() {
  const user = userEvent.setup()
  const fetchMock = fetchRouter({ fonte: source() })
  vi.stubGlobal('fetch', fetchMock)
  renderWithClient(<LibraryEditScreen />)

  await screen.findByLabelText('Caminho do datasource')
  await user.click(screen.getByRole('button', { name: 'Remover datasource' }))
  return { user, fetchMock }
}

describe('LibraryEditScreen', () => {
  it('pré-carrega o caminho atual do datasource', async () => {
    vi.stubGlobal('fetch', fetchRouter({ fonte: source() }))
    renderWithClient(<LibraryEditScreen />)

    await waitFor(() =>
      expect(screen.getByLabelText('Caminho do datasource')).toHaveValue(
        '/mnt/atual',
      ),
    )
  })

  it('pré-carrega o override de reentativa de enriquecimento quando a fonte já tem um', async () => {
    vi.stubGlobal(
      'fetch',
      fetchRouter({ fonte: source({ retroactive_enrichment_days: 45 }) }),
    )
    renderWithClient(<LibraryEditScreen />)

    await waitFor(() =>
      expect(
        screen.getByLabelText(
          'Reentativa de enriquecimento (dias) do datasource',
        ),
      ).toHaveValue(45),
    )
  })

  it('limpa o override de reentativa (campo vazio) e envia null no PATCH', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({
      fonte: source({ retroactive_enrichment_days: 45 }),
    })
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<LibraryEditScreen />)

    const input = await screen.findByLabelText(
      'Reentativa de enriquecimento (dias) do datasource',
    )
    await waitFor(() => expect(input).toHaveValue(45))
    await user.clear(input)
    const corpo = await clicarSalvarEPegarCorpo(user, fetchMock)
    expect(corpo).toEqual({
      path: '/mnt/atual',
      retroactive_enrichment_days: null,
    })
  })

  it('mostra "Datasource não encontrado" quando o id não existe na lista', async () => {
    vi.stubGlobal('fetch', fetchRouter({}))
    renderWithClient(<LibraryEditScreen />)

    expect(
      await screen.findByText('Datasource não encontrado'),
    ).toBeInTheDocument()
  })

  it('salva o novo caminho via PATCH e navega para /dashboard/library', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({ fonte: source() })
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<LibraryEditScreen />)

    const input = await screen.findByLabelText('Caminho do datasource')
    await user.clear(input)
    await user.type(input, '/mnt/novo')
    const corpo = await clicarSalvarEPegarCorpo(user, fetchMock)
    expect(corpo).toEqual({
      path: '/mnt/novo',
    })
  })

  it('mostra o erro 422 inline ao salvar um caminho inválido e não navega', async () => {
    const user = userEvent.setup()
    vi.stubGlobal(
      'fetch',
      fetchRouter({
        fonte: source(),
        patchResp: () =>
          erroJson(422, 'invalid_path', 'o caminho informado não existe'),
      }),
    )
    renderWithClient(<LibraryEditScreen />)

    const input = await screen.findByLabelText('Caminho do datasource')
    await user.clear(input)
    await user.type(input, '/nao/existe')
    await user.click(screen.getByRole('button', { name: 'Salvar' }))

    expect(
      await screen.findByText('o caminho informado não existe'),
    ).toBeInTheDocument()
    expect(spies.navigate).not.toHaveBeenCalled()
  })

  it('remover exige confirmação em 2 passos antes de chamar a API', async () => {
    const { user, fetchMock } = await abrirZonaDePerigo()

    expect(screen.getByRole('alertdialog')).toBeInTheDocument()
    // It did not call DELETE just by opening the confirmation.
    expect(chamouDelete(fetchMock)).toBe(false)

    await user.click(screen.getByRole('button', { name: 'Confirmar remoção' }))

    await waitFor(() => expect(chamouDelete(fetchMock)).toBe(true))
    await waitFor(() =>
      expect(spies.navigate).toHaveBeenCalledWith({ to: '/dashboard/library' }),
    )
  })

  it('cancelar a confirmação de remoção não chama a API', async () => {
    const { user, fetchMock } = await abrirZonaDePerigo()
    await user.click(screen.getByRole('button', { name: 'Cancelar' }))

    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    expect(chamouDelete(fetchMock)).toBe(false)
  })
})
