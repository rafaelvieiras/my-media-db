import { describe, expect, it, vi } from 'vitest'
import type { ReactNode } from 'react'
import { render, screen } from '@testing-library/react'
import { CardTemporada } from '@/components/entity-members'
import { entityCardBase as card } from '@/test/detail-harness'

// Testa o card de TEMPORADA (0036/0038): o selo de completude (completa / parcial)
// sai de `ownership` e vai no topo-direito compacto; a temporada vazia fica sem
// badge e com a capa normal (0038 removeu o recuo por opacity). CardTemporada
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

describe('CardTemporada — selo de completude (0036/0038)', () => {
  it('COMPLETA quando possui todos os episódios catalogados', () => {
    render(<CardTemporada card={season(10, 10)} />)
    expect(screen.getByLabelText('completa')).toBeTruthy()
  })

  it('PARCIAL mostra "X/Y" com a unidade no aria-label', () => {
    render(<CardTemporada card={season(3, 10)} />)
    const badge = screen.getByText('3/10')
    expect(badge.getAttribute('aria-label')).toBe('possui 3/10 episódios')
  })

  it('VAZIA fica sem badge e com a capa normal (0038)', () => {
    const { container } = render(<CardTemporada card={season(0, 10)} />)
    expect(screen.queryByText('0/10')).toBeNull()
    expect(screen.queryByLabelText(/episódios/)).toBeNull()
    // 0038 removeu o recuo por opacity: capa sempre normal.
    expect(container.querySelector('.opacity-60')).toBeNull()
  })

  it('SEM selo quando total é 0 (nada catalogado)', () => {
    render(<CardTemporada card={season(0, 0)} />)
    expect(screen.queryByLabelText(/episódios/)).toBeNull()
    expect(screen.queryByLabelText('completa')).toBeNull()
  })

  it('SEM selo quando o card não traz ownership (fica sem realce)', () => {
    const { container } = render(
      <CardTemporada card={card({ id: 'se1', type: 'season' })} />,
    )
    expect(screen.queryByLabelText(/episódios/)).toBeNull()
    expect(container.querySelector('.opacity-60')).toBeNull()
  })
})
