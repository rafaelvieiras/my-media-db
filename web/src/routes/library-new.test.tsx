import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { LibraryNewScreen } from '@/routes/library-new'
import { renderWithClient } from '@/test/render'
import { okJson } from '@/test/entity-fixtures'
import { erroJson } from '@/test/detail-harness'

// Route `/dashboard/library/new`: path field + PathBrowser + type
// Select + submit (`createLibrary`). Success navigates to `/dashboard/library`; an error
// (`ApiError`, e.g. 422 invalid/duplicate path) appears inline.
const spies = vi.hoisted(() => ({
  navigate: vi.fn(),
  back: vi.fn(),
  params: {},
}))

vi.mock('@tanstack/react-router', async () => {
  const { routerMock } = await import('@/test/detail-harness')
  return routerMock(spies)
})

// The folder browser has its own screen and copy: here it is replaced by a single
// button, so this test covers the WIRING (the folder it picks lands in the path
// field) without depending on the labels of another domain's component.
vi.mock('@/components/path-browser', () => ({
  PathBrowser: ({ onChange }: { onChange: (value: string) => void }) => (
    <button type="button" onClick={() => onChange('/')}>
      Pick a folder
    </button>
  ),
}))

// fetchRouter routes the creation (POST /library) and the global retry setting —
// the only routes this screen calls (the folder listing belongs to the mocked
// PathBrowser).
function fetchRouter(opts: {
  createResp?: () => Response
  retryDaysGlobal?: number
}) {
  return vi.fn(async (url: string, init?: RequestInit) => {
    const u = new URL(String(url), 'http://localhost')
    const method = init?.method ?? 'GET'
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
    throw new Error(`unmocked route: ${method} ${u.pathname}`)
  })
}

afterEach(() => {
  vi.restoreAllMocks()
  spies.navigate.mockReset()
})

// Clicks "Add datasource", waits for the success navigation and returns the
// body (already parsed) of POST /library — the submit tests only vary in what they
// filled in before and in what they check after this common point.
async function clicarAdicionarEPegarCorpo(
  user: ReturnType<typeof userEvent.setup>,
  fetchMock: ReturnType<typeof fetchRouter>,
) {
  await user.click(screen.getByRole('button', { name: /Add datasource/ }))

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
  it('creates the datasource (path + type) and navigates to /dashboard/library', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({})
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<LibraryNewScreen />)

    await user.type(screen.getByLabelText('Datasource path'), '/mnt/series')
    await user.selectOptions(screen.getByLabelText('Datasource type'), 'tv')
    const corpo = await clicarAdicionarEPegarCorpo(user, fetchMock)
    expect(corpo).toEqual({
      path: '/mnt/series',
      type: 'tv',
      content_type: 'series',
    })
  })

  it('does not submit with an empty path', async () => {
    const user = userEvent.setup()
    vi.stubGlobal('fetch', fetchRouter({}))
    renderWithClient(<LibraryNewScreen />)

    await user.click(screen.getByRole('button', { name: /Add datasource/ }))

    expect(spies.navigate).not.toHaveBeenCalled()
  })

  it('shows the 422 error inline and does not navigate', async () => {
    const user = userEvent.setup()
    vi.stubGlobal(
      'fetch',
      fetchRouter({
        createResp: () =>
          erroJson(422, 'invalid_path', 'the informed path does not exist'),
      }),
    )
    renderWithClient(<LibraryNewScreen />)

    await user.type(screen.getByLabelText('Datasource path'), '/nao/ha')
    await user.click(screen.getByRole('button', { name: /Add datasource/ }))

    expect(
      await screen.findByText('the informed path does not exist'),
    ).toBeInTheDocument()
    expect(spies.navigate).not.toHaveBeenCalled()
  })

  it('shows the placeholder with the global default and creates with a retry override', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({ retryDaysGlobal: 20 })
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<LibraryNewScreen />)

    const input = await screen.findByLabelText(
      'Datasource enrichment retry (days)',
    )
    await waitFor(() =>
      expect(input).toHaveAttribute(
        'placeholder',
        'Use the global default (20 days)',
      ),
    )

    await user.type(screen.getByLabelText('Datasource path'), '/mnt/series')
    await user.type(input, '7')
    const corpo = await clicarAdicionarEPegarCorpo(user, fetchMock)
    expect(corpo.retroactive_enrichment_days).toBe(7)
  })

  it('uses the folder picked in the folder browser as the path', async () => {
    const user = userEvent.setup()
    vi.stubGlobal('fetch', fetchRouter({}))
    renderWithClient(<LibraryNewScreen />)

    await user.click(screen.getByRole('button', { name: 'Pick a folder' }))

    expect(screen.getByLabelText('Datasource path')).toHaveValue('/')
  })
})
