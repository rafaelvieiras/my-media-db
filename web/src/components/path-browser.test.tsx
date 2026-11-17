import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { PathBrowser } from '@/components/path-browser'
import type { BrowseResult } from '@/lib/api'
import { renderWithClient } from '@/test/render'

// Folder browser: it covers navigation by click/breadcrumb,
// "Use this folder", an entry without read permission and an API error.

function okJson<T>(body: T): Response {
  return { ok: true, status: 200, json: async () => body } as Response
}

function erroJson(status: number, code: string, message: string): Response {
  return {
    ok: false,
    status,
    json: async () => ({ error: { code, message } }),
  } as Response
}

function browseResult(over: Partial<BrowseResult> = {}): BrowseResult {
  return {
    path: '/mnt',
    parent: '/',
    entries: [
      { name: 'filmes', path: '/mnt/filmes', readable: true },
      { name: 'privado', path: '/mnt/privado', readable: false },
    ],
    truncated: false,
    ...over,
  }
}

// Routes GET /api/v1/library/browse?path=... by `path` — each key of the map is
// the expected response for that path ('erro' returns 422 invalid_path).
function fetchRouter(byPath: Record<string, BrowseResult | 'erro'>) {
  return vi.fn(async (url: string) => {
    const u = new URL(String(url), 'http://localhost')
    if (u.pathname !== '/api/v1/library/browse') {
      throw new Error(`rota não mockada: ${u.pathname}`)
    }
    const path = u.searchParams.get('path') ?? '/'
    const resultado = byPath[path]
    if (resultado === 'erro') {
      return erroJson(422, 'invalid_path', 'o caminho informado não existe')
    }
    if (!resultado) {
      throw new Error(`path não mockado no teste: ${path}`)
    }
    return okJson(resultado)
  })
}

afterEach(() => {
  vi.restoreAllMocks()
})

// It opens the browser already with the routed fetch and the component mounted — most
// tests only differ in the route map/`value`/`onChange`, so the trio
// stubGlobal+render+click on "Browse folder…" is centralized here.
async function montarEAbrir(
  byPath: Record<string, BrowseResult | 'erro'>,
  props: Partial<{ value: string; onChange: (path: string) => void }> = {},
) {
  const user = userEvent.setup()
  vi.stubGlobal('fetch', fetchRouter(byPath))
  const onChange = props.onChange ?? vi.fn()
  renderWithClient(
    <PathBrowser value={props.value ?? ''} onChange={onChange} />,
  )
  await user.click(screen.getByRole('button', { name: 'Browse folder…' }))
  return { user, onChange }
}

describe('PathBrowser', () => {
  it('starts collapsed behind the "Browse folder…" button', () => {
    vi.stubGlobal('fetch', fetchRouter({}))
    renderWithClient(<PathBrowser value="" onChange={vi.fn()} />)

    expect(
      screen.getByRole('button', { name: 'Browse folder…' }),
    ).toBeInTheDocument()
    expect(screen.queryByText('filmes')).not.toBeInTheDocument()
  })

  it('lists the subfolders of the root when it opens and navigates on clicking a subfolder (breadcrumb follows)', async () => {
    const { user } = await montarEAbrir({
      '/': browseResult({ path: '/', parent: null }),
      '/mnt/filmes': browseResult({
        path: '/mnt/filmes',
        parent: '/mnt',
        entries: [{ name: 'acao', path: '/mnt/filmes/acao', readable: true }],
      }),
    })

    await waitFor(() => expect(screen.getByText('filmes')).toBeInTheDocument())
    await user.click(screen.getByText('filmes'))

    await waitFor(() => expect(screen.getByText('acao')).toBeInTheDocument())
    // The breadcrumb shows the "mnt" segment as a clickable button.
    expect(screen.getByRole('button', { name: 'mnt' })).toBeInTheDocument()
  })

  it('navigates up when clicking a segment of the breadcrumb', async () => {
    const { user } = await montarEAbrir(
      {
        '/': browseResult({ path: '/', parent: null }),
        '/mnt/filmes': browseResult({
          path: '/mnt/filmes',
          parent: '/mnt',
          entries: [{ name: 'acao', path: '/mnt/filmes/acao', readable: true }],
        }),
        '/mnt': browseResult({ path: '/mnt', parent: '/' }),
      },
      { value: '/mnt/filmes' },
    )

    await waitFor(() => expect(screen.getByText('acao')).toBeInTheDocument())
    await user.click(screen.getByRole('button', { name: 'mnt' }))

    await waitFor(() => expect(screen.getByText('filmes')).toBeInTheDocument())
  })

  it('an entry without read permission shows up disabled and does not navigate', async () => {
    const { user } = await montarEAbrir({
      '/': browseResult({ path: '/', parent: null }),
    })

    await waitFor(() => expect(screen.getByText('privado')).toBeInTheDocument())
    const botaoPrivado = screen.getByRole('button', { name: /privado/ })
    expect(botaoPrivado).toBeDisabled()

    await user.click(botaoPrivado)
    // It keeps showing the content of the root — it did not navigate to /mnt/privado.
    expect(screen.getByText('filmes')).toBeInTheDocument()
  })

  it('"Use this folder" calls onChange with the current path (normalized by the API)', async () => {
    const { user, onChange } = await montarEAbrir({
      '/': browseResult({ path: '/', parent: null }),
    })

    await waitFor(() => expect(screen.getByText('filmes')).toBeInTheDocument())
    await user.click(screen.getByRole('button', { name: 'Use this folder' }))

    expect(onChange).toHaveBeenCalledWith('/')
  })

  it('disables "Go up one level" when it is already at the root (parent null)', async () => {
    await montarEAbrir({ '/': browseResult({ path: '/', parent: null }) })

    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: /Go up one level/ }),
      ).toBeDisabled(),
    )
  })

  it('shows the API error sensibly when the path is not navigable', async () => {
    await montarEAbrir({ '/': 'erro' })

    expect(
      await screen.findByText('o caminho informado não existe'),
    ).toBeInTheDocument()
    expect(screen.getByRole('alert')).toBeInTheDocument()
  })

  it('shows the warning when the response comes truncated', async () => {
    await montarEAbrir({
      '/': browseResult({ path: '/', parent: null, truncated: true }),
    })

    expect(
      await screen.findByText('Showing the first 2000 folders.'),
    ).toBeInTheDocument()
  })

  it('a broken initial path (offline source) recovers to the root on its own instead of getting stuck', async () => {
    await montarEAbrir(
      {
        '/media/quebrado': 'erro',
        '/': browseResult({ path: '/', parent: null }),
      },
      { value: '/media/quebrado' },
    )

    // It errors when opening on the broken path, but recovers to the root on its own —
    // it does not get stuck on the error message waiting for the owner to click something.
    await waitFor(() => expect(screen.getByText('filmes')).toBeInTheDocument())
  })

  it('an offline path fires ONE call to browse, without the automatic retry of TanStack Query (0078)', async () => {
    // `renderWithClient` (the standard helper of the other tests) already turns off retry
    // in the WHOLE CLIENT, which would mask precisely the bug being fixed
    // (retry:false has to be in the queryOptions, not only in the test client).
    // Here we use a QueryClient with the REAL default of TanStack Query (retry:
    // 3) to prove that `libraryBrowseQueryOptions` turns retry off on its own.
    const fetchMock = vi.fn(async (url: string) => {
      const u = new URL(String(url), 'http://localhost')
      const path = u.searchParams.get('path') ?? '/'
      if (path === '/media/quebrado') {
        return {
          ok: false,
          status: 422,
          json: async () => ({
            error: {
              code: 'invalid_path',
              message: 'o caminho informado não existe',
            },
          }),
        } as Response
      }
      return okJson(browseResult({ path: '/', parent: null }))
    })
    vi.stubGlobal('fetch', fetchMock)
    const queryClient = new QueryClient()
    render(
      <QueryClientProvider client={queryClient}>
        <PathBrowser value="/media/quebrado" onChange={vi.fn()} />
      </QueryClientProvider>,
    )
    await userEvent
      .setup()
      .click(screen.getByRole('button', { name: 'Browse folder…' }))

    // It recovers to the root on its own (behavior already covered above); here what
    // matters is HOW MANY times the broken path was called.
    await waitFor(() => expect(screen.getByText('filmes')).toBeInTheDocument())

    const chamadasPathQuebrado = fetchMock.mock.calls.filter(([url]) => {
      const u = new URL(String(url), 'http://localhost')
      return u.searchParams.get('path') === '/media/quebrado'
    })
    expect(chamadasPathQuebrado).toHaveLength(1)
  })

  it('manual navigation is not undone by the debounce that follows the typed text (regression)', async () => {
    // The `value` of the caller stays still at '/mnt' (the owner did not edit the
    // text field) while the owner navigates manually through the browser —
    // before the fix, the synchronization debounce reacted to ANY
    // divergence between `pasta` and `value`, not only to a real edit of the
    // field, and reverted the navigation back to `value` ~400ms later.
    const { user } = await montarEAbrir(
      {
        '/mnt': browseResult({
          path: '/mnt',
          parent: '/',
          entries: [{ name: 'filmes', path: '/mnt/filmes', readable: true }],
        }),
        '/mnt/filmes': browseResult({
          path: '/mnt/filmes',
          parent: '/mnt',
          entries: [{ name: 'acao', path: '/mnt/filmes/acao', readable: true }],
        }),
      },
      { value: '/mnt' },
    )

    await waitFor(() => expect(screen.getByText('filmes')).toBeInTheDocument())
    await user.click(screen.getByText('filmes'))
    await waitFor(() => expect(screen.getByText('acao')).toBeInTheDocument())

    // It waits past the debounce interval (400ms) without editing `value`: the
    // navigation to /mnt/filmes must still stand.
    await new Promise((resolve) => setTimeout(resolve, 600))
    expect(screen.getByText('acao')).toBeInTheDocument()
  })
})
