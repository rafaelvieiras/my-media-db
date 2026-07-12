import { describe, expect, it, vi } from 'vitest'
import type { ReactNode } from 'react'
import { render, screen } from '@testing-library/react'
import { CardTemporada } from '@/components/entity-members'
import { entityCardBase as card } from '@/test/detail-harness'

// Testa o card de TEMPORADA (0036): o selo de completude (completa / parcial /
// vazia) sai de `ownership`, e a temporada vazia recua (opacity). CardTemporada
// renderiza o PosterCard (usa Link), então mocka só o Link do router por um <a>.
vi.mock('@tanstack/react-router', () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
}))

// season monta um card de temporada com a posse agregada dada.
function season(owned: number, total: number) {
  return card({
    id: 'se1',
    title: 'Temporada 1',
    type: 'season',
    ownership: { owned_count: owned, total_count: total },
  })
}

describe('CardTemporada — selo de completude (0036)', () => {
  it('COMPLETA quando possui todos os episódios catalogados', () => {
    render(<CardTemporada card={season(10, 10)} />)
    expect(screen.getByText('completa')).toBeTruthy()
  })

  it('PARCIAL mostra "possui X/Y episódios"', () => {
    render(<CardTemporada card={season(3, 10)} />)
    expect(screen.getByText('possui 3/10 episódios')).toBeTruthy()
  })

  it('VAZIA mostra "0/Y episódios" e recua (opacity)', () => {
    const { container } = render(<CardTemporada card={season(0, 10)} />)
    expect(screen.getByText('0/10 episódios')).toBeTruthy()
    // A temporada vazia fica discreta: o wrapper ganha opacity reduzida.
    expect(container.querySelector('.opacity-60')).toBeTruthy()
  })

  it('SEM selo quando total é 0 (nada catalogado)', () => {
    render(<CardTemporada card={season(0, 0)} />)
    expect(screen.queryByText(/episódios/)).toBeNull()
    expect(screen.queryByText('completa')).toBeNull()
  })

  it('SEM selo quando o card não traz ownership (fica sem realce)', () => {
    const { container } = render(
      <CardTemporada card={card({ id: 'se1', type: 'season' })} />,
    )
    expect(screen.queryByText(/episódios/)).toBeNull()
    expect(container.querySelector('.opacity-60')).toBeNull()
  })
})
