import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { PosterGrid } from '@/components/poster-grid'
import type { EntityCard } from '@/lib/api'
import { renderWithClient } from '@/test/render'

vi.mock('@tanstack/react-router', () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
}))

function page(items: EntityCard[], total = items.length) {
  return {
    ok: true,
    status: 200,
    json: async () => ({ items, total, limit: 24, offset: 0 }),
  } as Response
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('PosterGrid', () => {
  it('mostra o estado de carregamento', () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise<Response>(() => {})),
    )
    renderWithClient(<PosterGrid />)
    expect(screen.getByText('Carregando catálogo…')).toBeInTheDocument()
  })

  it('mostra o estado de erro', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        json: async () => ({ error: { code: 'x', message: 'falhou' } }),
      } as Response),
    )
    renderWithClient(<PosterGrid />)
    await waitFor(() =>
      expect(
        screen.getByText('Não foi possível carregar o catálogo'),
      ).toBeInTheDocument(),
    )
  })

  it('mostra o estado vazio que orienta o usuário', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(page([], 0)))
    renderWithClient(<PosterGrid />)
    await waitFor(() =>
      expect(screen.getByText('Nenhum item no catálogo')).toBeInTheDocument(),
    )
    expect(screen.getByText(/adicione um datasource/i)).toBeInTheDocument()
  })

  it('renderiza os cards e a contagem da paginação', async () => {
    const items: EntityCard[] = [
      {
        id: 'a',
        type: 'movie',
        title: 'Alfa',
        status: 'cataloged',
        owned: true,
        file_count: 1,
        match_confidence: 100,
        poster_url: null,
        catalog_completeness: null,
      },
    ]
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(page(items, 1)))
    renderWithClient(<PosterGrid />)
    await waitFor(() => expect(screen.getByText('Alfa')).toBeInTheDocument())
    expect(screen.getByText('1–1 de 1')).toBeInTheDocument()
  })
})
