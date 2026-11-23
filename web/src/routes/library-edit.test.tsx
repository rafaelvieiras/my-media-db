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
//
// The interface language falls back to English in these tests (no stored
// preference), so the assertions use the English copy of the `library` catalog.
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
    throw new Error(`unmocked route: ${method} ${u}`)
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

// chamouPatch is the same check for the PATCH of the path (a submit that must not
// reach the API).
function chamouPatch(fetchMock: ReturnType<typeof fetchRouter>): boolean {
  return fetchMock.mock.calls.some(
    ([u, init]) =>
      String(u) === '/api/v1/library/s1' &&
      (init as RequestInit | undefined)?.method === 'PATCH',
  )
}

// Clicks "Save", waits for the success navigation and returns the body (already
// parsed) of PATCH /library/s1 — the submit tests only vary in what they
// filled in before and in what they check after this common point.
async function clicarSalvarEPegarCorpo(
  user: ReturnType<typeof userEvent.setup>,
  fetchMock: ReturnType<typeof fetchRouter>,
) {
  await user.click(screen.getByRole('button', { name: 'Save' }))

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

// abrirZonaDePerigo mounts the screen, clicks "Remove datasource" and returns
// `user`/`fetchMock`/`gatilho` for the test to continue (confirm, cancel or Esc). The
// trigger is captured BEFORE the click: while the dialog is open the rest of the page
// is `aria-hidden` (focus trap of the shared AlertDialog).
async function abrirZonaDePerigo() {
  const user = userEvent.setup()
  const fetchMock = fetchRouter({ fonte: source() })
  vi.stubGlobal('fetch', fetchMock)
  renderWithClient(<LibraryEditScreen />)

  await screen.findByLabelText('Datasource path')
  const gatilho = screen.getByRole('button', { name: 'Remove datasource' })
  await user.click(gatilho)
  return { user, fetchMock, gatilho }
}

describe('LibraryEditScreen', () => {
  it('preloads the current path of the datasource', async () => {
    vi.stubGlobal('fetch', fetchRouter({ fonte: source() }))
    renderWithClient(<LibraryEditScreen />)

    await waitFor(() =>
      expect(screen.getByLabelText('Datasource path')).toHaveValue(
        '/mnt/atual',
      ),
    )
  })

  it('preloads the enrichment retry override when the source already has one', async () => {
    vi.stubGlobal(
      'fetch',
      fetchRouter({ fonte: source({ retroactive_enrichment_days: 45 }) }),
    )
    renderWithClient(<LibraryEditScreen />)

    await waitFor(() =>
      expect(
        screen.getByLabelText('Datasource enrichment retry (days)'),
      ).toHaveValue(45),
    )
  })

  it('clears the retry override (empty field) and sends null in the PATCH', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({
      fonte: source({ retroactive_enrichment_days: 45 }),
    })
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<LibraryEditScreen />)

    const input = await screen.findByLabelText(
      'Datasource enrichment retry (days)',
    )
    await waitFor(() => expect(input).toHaveValue(45))
    await user.clear(input)
    const corpo = await clicarSalvarEPegarCorpo(user, fetchMock)
    expect(corpo).toEqual({
      path: '/mnt/atual',
      retroactive_enrichment_days: null,
    })
  })

  it('shows "Datasource not found" when the id is not in the list', async () => {
    vi.stubGlobal('fetch', fetchRouter({}))
    renderWithClient(<LibraryEditScreen />)

    expect(await screen.findByText('Datasource not found')).toBeInTheDocument()
  })

  it('saves the new path via PATCH and navigates to /dashboard/library', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({ fonte: source() })
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<LibraryEditScreen />)

    const input = await screen.findByLabelText('Datasource path')
    await user.clear(input)
    await user.type(input, '/mnt/novo')
    const corpo = await clicarSalvarEPegarCorpo(user, fetchMock)
    expect(corpo).toEqual({
      path: '/mnt/novo',
    })
  })

  it('shows the 422 error inline when saving an invalid path and does not navigate', async () => {
    const user = userEvent.setup()
    vi.stubGlobal(
      'fetch',
      fetchRouter({
        fonte: source(),
        patchResp: () =>
          erroJson(422, 'invalid_path', 'the informed path does not exist'),
      }),
    )
    renderWithClient(<LibraryEditScreen />)

    const input = await screen.findByLabelText('Datasource path')
    await user.clear(input)
    await user.type(input, '/nao/existe')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    expect(
      await screen.findByText('the informed path does not exist'),
    ).toBeInTheDocument()
    expect(spies.navigate).not.toHaveBeenCalled()
  })

  it('removal requires a 2-step confirmation before calling the API', async () => {
    const { user, fetchMock } = await abrirZonaDePerigo()

    expect(screen.getByRole('alertdialog')).toBeInTheDocument()
    // It did not call DELETE just by opening the confirmation.
    expect(chamouDelete(fetchMock)).toBe(false)

    await user.click(screen.getByRole('button', { name: 'Confirm removal' }))

    await waitFor(() => expect(chamouDelete(fetchMock)).toBe(true))
    await waitFor(() =>
      expect(spies.navigate).toHaveBeenCalledWith({ to: '/dashboard/library' }),
    )
  })

  it('cancelling the removal confirmation does not call the API', async () => {
    const { user, fetchMock } = await abrirZonaDePerigo()
    await user.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    expect(chamouDelete(fetchMock)).toBe(false)
  })

  it('Esc closes the removal dialog and gives the focus back to the button', async () => {
    const { user, fetchMock, gatilho } = await abrirZonaDePerigo()

    await user.keyboard('{Escape}')

    await waitFor(() =>
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument(),
    )
    expect(gatilho).toHaveFocus()
    expect(chamouDelete(fetchMock)).toBe(false)
  })

  it('keeps the removal dialog open and shows the error inside it', async () => {
    const user = userEvent.setup()
    vi.stubGlobal(
      'fetch',
      fetchRouter({
        fonte: source(),
        deleteResp: () => erroJson(500, 'internal', 'database is locked'),
      }),
    )
    renderWithClient(<LibraryEditScreen />)

    await screen.findByLabelText('Datasource path')
    await user.click(screen.getByRole('button', { name: 'Remove datasource' }))
    await user.click(
      await screen.findByRole('button', { name: 'Confirm removal' }),
    )

    expect(
      (await screen.findAllByText('database is locked')).length,
    ).toBeGreaterThan(0)
    // The message appears inside the dialog too: the section that holds the other
    // copy is `aria-hidden` while the dialog is open.
    expect(screen.getByRole('alertdialog')).toBeInTheDocument()
    expect(spies.navigate).not.toHaveBeenCalled()
  })

  it('an empty submit says what is missing and focuses the path field', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({ fonte: source() })
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<LibraryEditScreen />)

    const campo = await screen.findByLabelText('Datasource path')
    await user.clear(campo)
    await user.click(screen.getByRole('button', { name: 'Save' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Enter the folder path.',
    )
    expect(campo).toHaveAttribute('aria-invalid', 'true')
    expect(campo).toHaveFocus()
    expect(chamouPatch(fetchMock)).toBe(false)
  })

  it('asks for confirmation before leaving with an unsaved change', async () => {
    const user = userEvent.setup()
    vi.stubGlobal('fetch', fetchRouter({ fonte: source() }))
    renderWithClient(<LibraryEditScreen />)

    const campo = await screen.findByLabelText('Datasource path')
    await user.clear(campo)
    await user.type(campo, '/mnt/novo')
    await user.click(screen.getByRole('link', { name: /Back to libraries/ }))

    expect(
      await screen.findByRole('alertdialog', { name: 'Discard changes?' }),
    ).toBeInTheDocument()
    expect(spies.navigate).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: 'Discard and leave' }))
    expect(spies.navigate).toHaveBeenCalledWith({
      to: '/dashboard/library',
    })
  })

  it('offers a retry when the datasource list fails to load', async () => {
    const user = userEvent.setup()
    let tentativas = 0
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: RequestInit) => {
        const u = String(url)
        if (u === '/api/v1/library' && (init?.method ?? 'GET') === 'GET') {
          tentativas += 1
          return tentativas === 1
            ? erroJson(500, 'internal', 'database is locked')
            : okJson({ items: [source()] })
        }
        if (u === '/api/v1/settings/enrichment-retroativo') {
          return okJson({ retry_days: 15 })
        }
        throw new Error(`unmocked route: ${u}`)
      }),
    )
    renderWithClient(<LibraryEditScreen />)

    expect(await screen.findByText('database is locked')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Try again' }))

    await waitFor(() =>
      expect(screen.getByLabelText('Datasource path')).toHaveValue(
        '/mnt/atual',
      ),
    )
  })
})
