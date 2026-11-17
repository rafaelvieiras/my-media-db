import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { DatasourceList } from '@/components/datasource-list'
import type { ScanStatus, Source } from '@/lib/api'
import { renderWithClient } from '@/test/render'

// The creation and the editing/removal of a path migrated to dedicated
// routes (`/library/new`, `/library/$id/edit` — covered in
// `library-new.test.tsx`/`library-edit.test.tsx`). This list only covers what
// stays here: empty state, path+badge+progress, "Scan", the watch
// switch and the "Add datasource"/"Edit" links.
vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock,
}))

function okJson<T>(body: T, status = 200) {
  return { ok: true, status, json: async () => body } as Response
}

// Routes the fetch by URL/method to simulate the library+scan API.
function fetchRouter(sources: Source[], status: ScanStatus) {
  return vi.fn(async (url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'
    if (u === '/api/v1/library' && method === 'GET') {
      return okJson({ items: sources })
    }
    if (u.endsWith('/scan') && method === 'GET') {
      return okJson(status)
    }
    if (u.endsWith('/scan') && method === 'POST') {
      return okJson({ job_id: 'j', status: 'accepted' }, 202)
    }
    if (u.startsWith('/api/v1/library/') && method === 'PATCH') {
      const source = sources.find((s) => u === `/api/v1/library/${s.id}`)
      const body = JSON.parse(String(init?.body ?? '{}')) as {
        watch_enabled?: boolean
        path?: string
      }
      // It mutates in memory so that the refetch after invalidation (triggered by the
      // component) reflects the new state, as the real API would.
      Object.assign(source ?? {}, body)
      return okJson({ ...source, ...body })
    }
    throw new Error(`rota não mockada: ${method} ${u}`)
  })
}

const idle: ScanStatus = { running: false, files_seen: 0, files_total: 0 }

// Source fixture — most tests only vary id/path/type/watch_enabled.
function sourceFixture(over: Partial<Source> = {}): Source {
  return {
    id: '1',
    path: '/mnt/filmes',
    type: 'movie',
    content_type: 'movie',
    state: 'online',
    last_scan: null,
    watch_enabled: false,
    ...over,
  }
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('DatasourceList', () => {
  it('shows the empty state when there are no datasources', async () => {
    vi.stubGlobal('fetch', fetchRouter([], idle))
    renderWithClient(<DatasourceList />)
    await waitFor(() =>
      expect(screen.getByText('No datasource registered')).toBeInTheDocument(),
    )
  })

  it('shows the "Add datasource" link pointing to /dashboard/library/new', async () => {
    vi.stubGlobal('fetch', fetchRouter([], idle))
    renderWithClient(<DatasourceList />)

    const link = await screen.findByRole('link', {
      name: /Add datasource/,
    })
    expect(link).toHaveAttribute('href', '/dashboard/library/new')
  })

  it('lists the datasource with its state and the scan progress in flight', async () => {
    const source = sourceFixture()
    vi.stubGlobal(
      'fetch',
      fetchRouter([source], {
        running: true,
        files_seen: 5,
        files_total: 10,
      }),
    )
    renderWithClient(<DatasourceList />)

    await waitFor(() =>
      expect(screen.getByText('/mnt/filmes')).toBeInTheDocument(),
    )
    expect(screen.getByText('online')).toBeInTheDocument()
    await waitFor(() =>
      expect(
        screen.getByText('Scanning… 5 of 10 files seen'),
      ).toBeInTheDocument(),
    )
  })

  it('the "Edit" link of each row points to /dashboard/library/$id/edit', async () => {
    const source = sourceFixture({ id: '42' })
    vi.stubGlobal('fetch', fetchRouter([source], idle))
    renderWithClient(<DatasourceList />)

    const link = await screen.findByRole('link', { name: /Edit/ })
    expect(link).toHaveAttribute('href', '/dashboard/library/42/edit')
  })

  it('triggers the scan when Scan is clicked', async () => {
    const user = userEvent.setup()
    const source = sourceFixture({
      id: '7',
      path: '/tv',
      type: 'tv',
      content_type: 'series',
    })
    const fetchMock = fetchRouter([source], idle)
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<DatasourceList />)

    await waitFor(() => expect(screen.getByText('/tv')).toBeInTheDocument())
    await user.click(screen.getByRole('button', { name: /^Scan$/ }))

    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(
          ([u, init]) =>
            String(u) === '/api/v1/library/7/scan' &&
            (init as RequestInit | undefined)?.method === 'POST',
        ),
      ).toBe(true),
    )
  })

  it('turns the filesystem watch on via PATCH when the switch is toggled', async () => {
    const user = userEvent.setup()
    const source = sourceFixture({ id: '9' })
    const fetchMock = fetchRouter([source], idle)
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<DatasourceList />)

    await waitFor(() =>
      expect(screen.getByText('/mnt/filmes')).toBeInTheDocument(),
    )
    const toggle = screen.getByRole('switch', {
      name: 'Watch this folder for changes',
    })
    expect(toggle).not.toBeChecked()
    await user.click(toggle)

    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(([u, init]) => {
          const i = init as RequestInit | undefined
          return (
            String(u) === '/api/v1/library/9' &&
            i?.method === 'PATCH' &&
            JSON.parse(String(i.body)).watch_enabled === true
          )
        }),
      ).toBe(true),
    )
    await waitFor(() => expect(toggle).toBeChecked())
  })

  it('no longer has a fixed form at the top nor a removal button in the list', async () => {
    const source = sourceFixture()
    vi.stubGlobal('fetch', fetchRouter([source], idle))
    renderWithClient(<DatasourceList />)

    await waitFor(() =>
      expect(screen.getByText('/mnt/filmes')).toBeInTheDocument(),
    )
    expect(screen.queryByLabelText('Datasource path')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Remove datasource' }),
    ).not.toBeInTheDocument()
  })
})
