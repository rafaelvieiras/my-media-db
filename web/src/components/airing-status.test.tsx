import { describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import { RichDetailView } from '@/components/rich-detail-view'
import { richDetailBase } from '@/test/detail-harness'
import { renderWithClient } from '@/test/render'

// airing-status covers the SITUATION BADGE of the series: the RichDetailView renders
// a <Badge> on the hero row (next to the certification) with the translated label when
// the situation is present. It is series metadata; without the data (movie) the badge does not appear.
vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock,
  useNavigate: () => vi.fn(),
}))

describe('RichDetailView — situation badge (0045)', () => {
  it('series "Returning Series" → "Airing" badge', () => {
    renderWithClient(
      <RichDetailView
        detail={richDetailBase({
          type: 'tv',
          airingStatus: 'Returning Series',
        })}
        source="tmdb"
      />,
    )
    expect(screen.getByText('Airing')).toBeInTheDocument()
  })

  it('series "Ended" → "Ended" badge', () => {
    renderWithClient(
      <RichDetailView
        detail={richDetailBase({ type: 'tv', airingStatus: 'Ended' })}
        source="tmdb"
      />,
    )
    expect(screen.getByText('Ended')).toBeInTheDocument()
  })

  it('situation coming from TVDB ("Continuing") → "Airing" badge', () => {
    renderWithClient(
      <RichDetailView
        detail={richDetailBase({ type: 'tv', airingStatus: 'Continuing' })}
        source="tvdb"
      />,
    )
    expect(screen.getByText('Airing')).toBeInTheDocument()
  })

  it('no situation (movie) → no situation badge', () => {
    renderWithClient(
      <RichDetailView
        detail={richDetailBase({ type: 'movie', airingStatus: null })}
        source="tmdb"
      />,
    )
    expect(screen.queryByText('Airing')).toBeNull()
    expect(screen.queryByText('Ended')).toBeNull()
  })
})
