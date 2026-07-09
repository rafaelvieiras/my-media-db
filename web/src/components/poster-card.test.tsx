import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { PosterCard } from '@/components/poster-card'
import type { EntityCard } from '@/lib/api'

// O <Link> do TanStack Router exige contexto de router; para um teste de unidade
// do card, mockamos por um <a> simples (só o children/className importam aqui).
vi.mock('@tanstack/react-router', () => ({
  Link: ({
    children,
    className,
  }: {
    children: ReactNode
    className?: string
  }) => (
    <a href="#" className={className}>
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

  it('mostra badge de revisão em baixa confiança', () => {
    render(<PosterCard card={card({ match_confidence: 40 })} />)
    expect(screen.getByText('revisar')).toBeInTheDocument()
  })

  it('mostra badge de não identificado para draft', () => {
    render(<PosterCard card={card({ status: 'draft' })} />)
    expect(screen.getByText('não identificado')).toBeInTheDocument()
    expect(screen.queryByText('revisar')).not.toBeInTheDocument()
  })
})
