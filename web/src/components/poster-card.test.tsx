import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { PosterCard } from '@/components/poster-card'
import type { EntityCard } from '@/lib/api'

// O <Link> do TanStack Router exige contexto de router; para um teste de unidade
// do card, mockamos por um <a> simples. O `to` vira `href` para afirmar o destino
// do badge de revisão (link para `/review`).
vi.mock('@tanstack/react-router', () => ({
  Link: ({
    children,
    className,
    to,
  }: {
    children: ReactNode
    className?: string
    to?: string
  }) => (
    <a href={to ?? '#'} className={className}>
      {children}
    </a>
  ),
}))

function card(over: Partial<EntityCard> = {}): EntityCard {
  return {
    id: 'x',
    type: 'movie',
    title: 'Matrix',
    status: 'cataloged',
    owned: true,
    file_count: 1,
    match_confidence: 100,
    poster_url: null,
    catalog_completeness: null,
    ...over,
  }
}

describe('PosterCard', () => {
  it('mostra título, tipo e badge de posse', () => {
    render(<PosterCard card={card({ owned: true })} />)
    expect(screen.getByText('Matrix')).toBeInTheDocument()
    expect(screen.getByText('Filme')).toBeInTheDocument()
    expect(screen.getByText('na biblioteca')).toBeInTheDocument()
  })

  it('filme owned mostra "na biblioteca" no topo-direito (0038)', () => {
    const { container } = render(<PosterCard card={card({ owned: true })} />)
    const overlayDireito = container.querySelector('.right-1\\.5')
    expect(overlayDireito).toBeTruthy()
    expect(overlayDireito?.textContent).toContain('na biblioteca')
  })

  it('sem posse não mostra o badge de biblioteca', () => {
    render(<PosterCard card={card({ owned: false })} />)
    expect(screen.queryByText('na biblioteca')).not.toBeInTheDocument()
  })

  it('nenhum card fica apagado (sem opacity-60, 0038)', () => {
    const { container } = render(<PosterCard card={card({ owned: false })} />)
    expect(container.querySelector('.opacity-60')).toBeNull()
  })

  it('cai no placeholder quando não há arte', () => {
    render(<PosterCard card={card({ poster_url: null })} />)
    expect(screen.getByTestId('poster-placeholder')).toBeInTheDocument()
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
  })

  it('renderiza a imagem quando há pôster', () => {
    render(
      <PosterCard
        card={card({ poster_url: '/api/v1/entities/x/assets/poster' })}
      />,
    )
    const img = screen.getByRole('img')
    expect(img).toHaveAttribute('src', '/api/v1/entities/x/assets/poster')
    expect(img).toHaveAttribute('alt', 'Pôster de Matrix')
  })

  it('cai no placeholder se a imagem falhar ao carregar', () => {
    render(
      <PosterCard
        card={card({ poster_url: '/api/v1/entities/x/assets/poster' })}
      />,
    )
    fireEvent.error(screen.getByRole('img'))
    expect(screen.getByTestId('poster-placeholder')).toBeInTheDocument()
  })

  it('mostra badge de revisão em baixa confiança e ele linka para /review', () => {
    const { container } = render(
      <PosterCard card={card({ match_confidence: 40 })} />,
    )
    const badge = screen.getByText('revisar')
    expect(badge).toBeInTheDocument()
    expect(badge.closest('a')).toHaveAttribute('href', '/review')
    // Revisão (atenção) fica no topo-ESQUERDO, não no de posse (0038).
    const overlayEsquerdo = container.querySelector('.left-1\\.5')
    expect(overlayEsquerdo?.textContent).toContain('revisar')
  })

  it('mostra badge de não identificado para draft e ele linka para /review', () => {
    render(<PosterCard card={card({ status: 'draft' })} />)
    const badge = screen.getByText('não identificado')
    expect(badge).toBeInTheDocument()
    expect(badge.closest('a')).toHaveAttribute('href', '/review')
    expect(screen.queryByText('revisar')).not.toBeInTheDocument()
  })
})

// Selo de completude por tipo (0037): o card de SÉRIE conta TEMPORADAS, o de
// TEMPORADA conta EPISÓDIOS. A mesma lógica de estado (completa/parcial/vazia) do
// 0036, só com a unidade trocada pelo tipo. Movie/collection nunca têm selo.
describe('PosterCard — selo de completude por tipo (0037/0038)', () => {
  it('SÉRIE parcial mostra "X/Y" compacto no topo-direito, unidade no aria-label', () => {
    const { container } = render(
      <PosterCard
        card={card({
          type: 'tv',
          ownership: { owned_count: 3, total_count: 38 },
        })}
      />,
    )
    const badge = screen.getByText('3/38')
    expect(badge).toBeInTheDocument()
    // Unidade completa fica acessível no aria-label/title, não no texto visível.
    expect(badge).toHaveAttribute('aria-label', 'possui 3/38 temporadas')
    expect(screen.queryByText('possui 3/38 temporadas')).not.toBeInTheDocument()
    // Selo de posse vai no overlay do topo-DIREITO (0038).
    const overlayDireito = container.querySelector('.right-1\\.5')
    expect(overlayDireito?.textContent).toContain('3/38')
    // Série com conteúdo não mostra "na biblioteca" (sem redundância, 0038).
    expect(screen.queryByText('na biblioteca')).not.toBeInTheDocument()
  })

  it('SÉRIE completa mostra o selo "completa" (✓ com aria-label) no topo-direito', () => {
    const { container } = render(
      <PosterCard
        card={card({
          type: 'tv',
          ownership: { owned_count: 5, total_count: 5 },
        })}
      />,
    )
    const badge = screen.getByLabelText('completa')
    expect(badge).toBeInTheDocument()
    const overlayDireito = container.querySelector('.right-1\\.5')
    expect(overlayDireito?.contains(badge)).toBe(true)
    expect(screen.queryByText('na biblioteca')).not.toBeInTheDocument()
  })

  it('SÉRIE vazia não mostra badge nem apaga a capa (0038)', () => {
    const { container } = render(
      <PosterCard
        card={card({
          type: 'tv',
          ownership: { owned_count: 0, total_count: 38 },
        })}
      />,
    )
    expect(screen.queryByText('0/38')).not.toBeInTheDocument()
    expect(screen.queryByLabelText(/temporadas/)).not.toBeInTheDocument()
    expect(container.querySelector('.opacity-60')).toBeNull()
    // Nenhum badge de posse (nem "na biblioteca").
    expect(screen.queryByText('na biblioteca')).not.toBeInTheDocument()
  })

  it('TEMPORADA parcial mostra "X/Y" com unidade "episódios" no aria-label (0036 intacto)', () => {
    render(
      <PosterCard
        card={card({
          type: 'season',
          ownership: { owned_count: 3, total_count: 10 },
        })}
      />,
    )
    const badge = screen.getByText('3/10')
    expect(badge).toHaveAttribute('aria-label', 'possui 3/10 episódios')
  })

  it('FILME não tem selo mesmo com ownership presente', () => {
    render(
      <PosterCard
        card={card({
          type: 'movie',
          ownership: { owned_count: 1, total_count: 2 },
        })}
      />,
    )
    // movie não conta unidade: sem "temporadas"/"episódios" e sem "completa".
    expect(screen.queryByText(/temporadas|episódios/)).not.toBeInTheDocument()
    expect(screen.queryByText('completa')).not.toBeInTheDocument()
  })

  it('sem ownership não mostra selo (movie/episode/collection)', () => {
    render(<PosterCard card={card({ type: 'tv' })} />)
    expect(screen.queryByText(/temporadas/)).not.toBeInTheDocument()
  })
})

// Indicador de completude de CATALOGAÇÃO (0058) — DIFERENTE do selo de posse
// (0037/0038, coberto acima). Ícone-only no topo-ESQUERDO, ao lado do badge de
// revisão. Só aparece quando `catalog_completeness.complete === false`.
describe('PosterCard — indicador de completude de catalogação (0058)', () => {
  it('mostra o ícone de alerta quando incompleto, com title/aria-label da lista', () => {
    const { container } = render(
      <PosterCard
        card={card({
          catalog_completeness: {
            complete: false,
            missing: ['cast', 'artwork:fanart'],
          },
        })}
      />,
    )
    const rotulo = 'Catalogação incompleta: elenco, arte fanart'
    const badge = screen.getByLabelText(rotulo)
    expect(badge).toBeInTheDocument()
    expect(badge).toHaveAttribute('title', rotulo)
    // Fica no topo-ESQUERDO (junto do badge de revisão), não no de posse.
    const overlayEsquerdo = container.querySelector('.left-1\\.5')
    expect(overlayEsquerdo?.contains(badge)).toBe(true)
  })

  it('não mostra o ícone quando completo', () => {
    render(
      <PosterCard
        card={card({
          catalog_completeness: { complete: true, missing: [] },
        })}
      />,
    )
    expect(
      screen.queryByLabelText(/Catalogação incompleta/),
    ).not.toBeInTheDocument()
  })

  it('não mostra o ícone quando null (draft/ainda não avaliado pelo job)', () => {
    render(<PosterCard card={card({ catalog_completeness: null })} />)
    expect(
      screen.queryByLabelText(/Catalogação incompleta/),
    ).not.toBeInTheDocument()
  })
})
