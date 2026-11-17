import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MediaTypeFilter } from '@/components/media-type-filter'
import type { EntityType } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { erroJson } from '@/test/detail-harness'

// Tests the new behavior of ADR-0030: the options of the Select
// are no longer a fixed enum `'all' | 'movie' | 'tv'` and now come from
// `GET /entities/types` (the same hook that the navbar/home already query).

function okJson<T>(body: T): Response {
  return { ok: true, status: 200, json: async () => body } as Response
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('MediaTypeFilter', () => {
  it('lists "All" plus the real top-level types returned by GET /entities/types', async () => {
    const tipos: EntityType[] = [
      { type: 'movie', count: 10 },
      { type: 'tv', count: 5 },
      { type: 'artista', count: 3 },
    ]
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(okJson(tipos)))
    renderWithClient(<MediaTypeFilter value="all" onChange={() => {}} />)

    await waitFor(() =>
      expect(
        screen.getByRole('option', { name: 'Artists' }),
      ).toBeInTheDocument(),
    )
    expect(screen.getByRole('option', { name: 'All' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'Movies' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'Series' })).toBeInTheDocument()
    // Nothing hardcoded: only the 4 options coming from "All" + real data.
    expect(screen.getAllByRole('option')).toHaveLength(4)
  })

  it('degrades to only "All" when GET /entities/types fails', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(erroJson(500, 'x', 'falhou')),
    )
    renderWithClient(<MediaTypeFilter value="all" onChange={() => {}} />)

    await waitFor(() =>
      expect(screen.getByRole('combobox')).toBeInTheDocument(),
    )
    expect(screen.getAllByRole('option')).toHaveLength(1)
    expect(screen.getByRole('option', { name: 'All' })).toBeInTheDocument()
  })

  it('propagates the type change through onChange with the real "type" (no longer pinned to movie/tv)', async () => {
    const tipos: EntityType[] = [
      { type: 'movie', count: 10 },
      { type: 'artista', count: 3 },
    ]
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(okJson(tipos)))
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderWithClient(<MediaTypeFilter value="all" onChange={onChange} />)

    await waitFor(() =>
      expect(
        screen.getByRole('option', { name: 'Artists' }),
      ).toBeInTheDocument(),
    )
    await user.selectOptions(screen.getByLabelText('Filter by type'), 'artista')
    expect(onChange).toHaveBeenCalledWith('artista')
  })
})
