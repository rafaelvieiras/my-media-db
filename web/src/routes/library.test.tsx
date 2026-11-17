import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import { LibraryScreen } from '@/routes/library'
import { renderWithClient } from '@/test/render'

// The `/dashboard/library` route is a header over `DatasourceList` — the list
// itself is covered in `components/datasource-list.test.tsx`; here the contract is
// the screen's own copy (title/subtitle), which now comes from the catalog.
vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock,
}))

function okJson<T>(body: T) {
  return { ok: true, status: 200, json: async () => body } as Response
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('LibraryScreen', () => {
  it('renders the screen title and subtitle above the datasource list', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => okJson({ items: [] })),
    )

    renderWithClient(<LibraryScreen />)

    expect(
      await screen.findByRole('heading', { name: 'Libraries' }),
    ).toBeInTheDocument()
    expect(
      screen.getByText(
        'Point at media folders, scan them, and follow the progress.',
      ),
    ).toBeInTheDocument()
  })
})
