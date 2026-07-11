import { describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import { RichDetailView } from '@/components/rich-detail-view'
import type { RichDetail } from '@/lib/rich-detail'
import { renderWithClient } from '@/test/render'

// Link interpola os params no href, para afirmar o destino da faixa de coleção.
vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock,
  useNavigate: () => vi.fn(),
}))

// detalheComColecao monta um RichDetail mínimo com uma coleção (o resto vazio: as
// seções sem dado não renderizam).
function detalheComColecao(): RichDetail {
  return {
    type: 'movie',
    title: 'Duna',
    originalTitle: null,
    description: null,
    tagline: null,
    year: null,
    runtime: null,
    certification: null,
    voteAverage: null,
    localRating: null,
    originalLanguage: null,
    posterUrl: null,
    backdropUrl: null,
    genres: [],
    keywords: [],
    videos: [],
    collection: { source_id: '726871', name: 'Coleção Duna', poster_url: null },
    similar: [],
    externalLinks: {},
  }
}

describe('ColecaoBand (na RichDetailView)', () => {
  it('linka para a página própria da coleção', () => {
    renderWithClient(
      <RichDetailView detail={detalheComColecao()} source="tmdb" />,
    )
    const link = screen.getByRole('link', { name: /Coleção Duna/ })
    expect(link.getAttribute('href')).toBe('/collection/tmdb/726871')
  })

  it('linka para a entity INTERNA quando a coleção já foi importada (0026)', () => {
    const detalhe = detalheComColecao()
    // internal_id é campo aditivo (0026): o cast evita depender do tipo regenerado.
    detalhe.collection = {
      source_id: '726871',
      name: 'Coleção Duna',
      poster_url: null,
      internal_id: 'ent-col',
    } as RichDetail['collection']
    renderWithClient(<RichDetailView detail={detalhe} source="tmdb" />)

    const link = screen.getByRole('link', { name: /Coleção Duna/ })
    expect(link.getAttribute('href')).toBe('/entities/ent-col')
  })
})
