import { describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import { RichDetailView } from '@/components/rich-detail-view'
import type { RichDetail } from '@/lib/rich-detail'
import { richDetailBase } from '@/test/detail-harness'
import { renderWithClient } from '@/test/render'

// The link interpolates the params into the href, to assert the destination of the collection band.
vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock,
  useNavigate: () => vi.fn(),
}))

// detalheComColecao builds a minimal RichDetail with a collection (the rest empty: the
// sections without data do not render). It reuses the shared factory (jscpd).
function detalheComColecao(): RichDetail {
  return richDetailBase({
    collection: { source_id: '726871', name: 'Coleção Duna', poster_url: null },
  })
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
    // internal_id is an ADDITIVE field: the cast avoids depending on the regenerated type.
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
