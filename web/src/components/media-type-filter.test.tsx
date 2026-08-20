import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MediaTypeFilter } from '@/components/media-type-filter'
import type { EntityType } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { erroJson } from '@/test/detail-harness'

// Testa o comportamento novo do briefing 0113/ADR-0030: as opções do Select
// deixam de ser um enum fixo `'all' | 'movie' | 'tv'` e passam a vir de
// `GET /entities/types` (mesmo hook que a navbar/home já consultam).

function okJson<T>(body: T): Response {
  return { ok: true, status: 200, json: async () => body } as Response
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('MediaTypeFilter', () => {
  it('lista "Todos" + os tipos de topo reais devolvidos por GET /entities/types', async () => {
    const tipos: EntityType[] = [
      { type: 'movie', count: 10 },
      { type: 'tv', count: 5 },
      { type: 'artista', count: 3 },
    ]
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(okJson(tipos)))
    renderWithClient(<MediaTypeFilter value="all" onChange={() => {}} />)

    await waitFor(() =>
      expect(
        screen.getByRole('option', { name: 'Artistas' }),
      ).toBeInTheDocument(),
    )
    expect(screen.getByRole('option', { name: 'Todos' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'Filmes' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'Séries' })).toBeInTheDocument()
    // Nada hardcoded: só as 4 opções vindas de "Todos" + dado real.
    expect(screen.getAllByRole('option')).toHaveLength(4)
  })

  it('degrada mostrando só "Todos" quando GET /entities/types falha', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(erroJson(500, 'x', 'falhou')),
    )
    renderWithClient(<MediaTypeFilter value="all" onChange={() => {}} />)

    await waitFor(() =>
      expect(screen.getByRole('combobox')).toBeInTheDocument(),
    )
    expect(screen.getAllByRole('option')).toHaveLength(1)
    expect(screen.getByRole('option', { name: 'Todos' })).toBeInTheDocument()
  })

  it('propaga a troca de tipo pelo onChange com o "type" real (não mais preso a movie/tv)', async () => {
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
        screen.getByRole('option', { name: 'Artistas' }),
      ).toBeInTheDocument(),
    )
    await user.selectOptions(
      screen.getByLabelText('Filtrar por tipo'),
      'artista',
    )
    expect(onChange).toHaveBeenCalledWith('artista')
  })
})
