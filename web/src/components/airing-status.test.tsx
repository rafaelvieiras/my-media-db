import { describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import { RichDetailView } from '@/components/rich-detail-view'
import { richDetailBase } from '@/test/detail-harness'
import { renderWithClient } from '@/test/render'

// airing-status covers the SITUATION BADGE of the series: the RichDetailView renders
// a <Badge> on the hero row (next to the certification) with the pt-BR label when the
// situation is present. It is series metadata; without the data (movie) the badge does not appear.
vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock,
  useNavigate: () => vi.fn(),
}))

describe('RichDetailView — selo de situação (0045)', () => {
  it('série "Returning Series" → selo "Em exibição"', () => {
    renderWithClient(
      <RichDetailView
        detail={richDetailBase({
          type: 'tv',
          airingStatus: 'Returning Series',
        })}
        source="tmdb"
      />,
    )
    expect(screen.getByText('Em exibição')).toBeInTheDocument()
  })

  it('série "Ended" → selo "Encerrada"', () => {
    renderWithClient(
      <RichDetailView
        detail={richDetailBase({ type: 'tv', airingStatus: 'Ended' })}
        source="tmdb"
      />,
    )
    expect(screen.getByText('Encerrada')).toBeInTheDocument()
  })

  it('situação vinda do TVDB ("Continuing") → selo "Em exibição"', () => {
    renderWithClient(
      <RichDetailView
        detail={richDetailBase({ type: 'tv', airingStatus: 'Continuing' })}
        source="tvdb"
      />,
    )
    expect(screen.getByText('Em exibição')).toBeInTheDocument()
  })

  it('sem situação (filme) → nenhum selo de situação', () => {
    renderWithClient(
      <RichDetailView
        detail={richDetailBase({ type: 'movie', airingStatus: null })}
        source="tmdb"
      />,
    )
    expect(screen.queryByText('Em exibição')).toBeNull()
    expect(screen.queryByText('Encerrada')).toBeNull()
  })
})
