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

  it('sem posse não mostra o badge de biblioteca', () => {
    render(<PosterCard card={card({ owned: false })} />)
    expect(screen.queryByText('na biblioteca')).not.toBeInTheDocument()
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
    render(<PosterCard card={card({ match_confidence: 40 })} />)
    const badge = screen.getByText('revisar')
    expect(badge).toBeInTheDocument()
    expect(badge.closest('a')).toHaveAttribute('href', '/review')
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
describe('PosterCard — selo de completude por tipo (0037)', () => {
  it('SÉRIE parcial rotula "temporadas"', () => {
    render(
      <PosterCard
        card={card({
          type: 'tv',
          ownership: { owned_count: 3, total_count: 38 },
        })}
      />,
    )
    expect(screen.getByText('possui 3/38 temporadas')).toBeInTheDocument()
  })

  it('SÉRIE completa mostra o selo "completa"', () => {
    render(
      <PosterCard
        card={card({
          type: 'tv',
          ownership: { owned_count: 5, total_count: 5 },
        })}
      />,
    )
    expect(screen.getByText('completa')).toBeInTheDocument()
  })

  it('SÉRIE vazia recua (opacity) e rotula "0/Y temporadas"', () => {
    const { container } = render(
      <PosterCard
        card={card({
          type: 'tv',
          ownership: { owned_count: 0, total_count: 38 },
        })}
      />,
    )
    expect(screen.getByText('0/38 temporadas')).toBeInTheDocument()
    expect(container.querySelector('.opacity-60')).toBeTruthy()
  })

  it('TEMPORADA parcial rotula "episódios" (0036 intacto)', () => {
    render(
      <PosterCard
        card={card({
          type: 'season',
          ownership: { owned_count: 3, total_count: 10 },
        })}
      />,
    )
    expect(screen.getByText('possui 3/10 episódios')).toBeInTheDocument()
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
