import { describe, expect, it, vi } from 'vitest'
import type { ReactNode } from 'react'
import { render, screen } from '@testing-library/react'
import { CardNumerado, CardTemporada } from '@/components/entity-members'
import { entityCardBase as card } from '@/test/detail-harness'
import type { EntityCard } from '@/lib/api'

// Tests the SEASON card: the completeness badge (complete / partial)
// comes from `ownership` and goes in the compact top-right corner; the empty season stays without
// a badge and with the normal cover (the opacity indent was removed). CardTemporada
// renders the PosterCard (which uses Link), so it mocks only the router Link as an <a>.
vi.mock('@tanstack/react-router', () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
}))

// season builds a season card with the given aggregated ownership.
function season(owned: number, total: number) {
  return card({
    id: 'se1',
    title: 'Temporada 1',
    type: 'season',
    ownership: { owned_count: owned, total_count: total },
  })
}

describe('CardTemporada — completeness badge (0036/0038)', () => {
  it('COMPLETE when it owns every cataloged episode', () => {
    render(<CardTemporada card={season(10, 10)} />)
    // The badge copy belongs to the card (`poster-card.tsx`, another domain): the
    // regex tolerates both labels so this test does not depend on that extraction.
    expect(screen.getByLabelText(/^(completa|complete)$/i)).toBeTruthy()
  })

  it('PARTIAL shows "X/Y" with the unit in the aria-label', () => {
    render(<CardTemporada card={season(3, 10)} />)
    const badge = screen.getByText('3/10')
    expect(badge.getAttribute('aria-label')).toMatch(
      /3\/10 (episódios|episodes)/,
    )
  })

  it('EMPTY stays without a badge and with the normal cover (0038)', () => {
    const { container } = render(<CardTemporada card={season(0, 10)} />)
    expect(screen.queryByText('0/10')).toBeNull()
    expect(screen.queryByLabelText(/episódios|episodes/)).toBeNull()
    // The opacity indent was removed: the cover is always normal.
    expect(container.querySelector('.opacity-60')).toBeNull()
  })

  it('NO badge when the total is 0 (nothing cataloged)', () => {
    render(<CardTemporada card={season(0, 0)} />)
    expect(screen.queryByLabelText(/episódios|episodes/)).toBeNull()
    expect(screen.queryByLabelText(/^(completa|complete)$/i)).toBeNull()
  })

  it('NO badge when the card has no ownership (no highlight)', () => {
    const { container } = render(
      <CardTemporada card={card({ id: 'se1', type: 'season' })} />,
    )
    expect(screen.queryByLabelText(/episódios/)).toBeNull()
    expect(container.querySelector('.opacity-60')).toBeNull()
  })
})

// episodio builds an episode card with the given real numbering.
function episodio(numbering: EntityCard['numbering']) {
  return card({ id: 'ep1', title: 'Piloto', type: 'episode', numbering })
}

describe('CardNumerado — real episode numbering (0039)', () => {
  it('shows the REAL number (E05), not the position in the list', () => {
    render(<CardNumerado card={episodio({ season: 1, number: 5 })} />)
    expect(screen.getByText('E05')).toBeTruthy()
  })

  it('zero-pads to 2 digits (E01)', () => {
    render(<CardNumerado card={episodio({ season: 1, number: 1 })} />)
    expect(screen.getByText('E01')).toBeTruthy()
  })

  it('appends the absolute when present and different from the order number', () => {
    render(
      <CardNumerado
        card={episodio({ season: 1, number: 5, absolute_number: 128 })}
      />,
    )
    expect(screen.getByText('E05 · #128')).toBeTruthy()
  })

  it('does NOT append the absolute when it equals the order number', () => {
    render(
      <CardNumerado
        card={episodio({ season: 1, number: 5, absolute_number: 5 })}
      />,
    )
    expect(screen.getByText('E05')).toBeTruthy()
    expect(screen.queryByText(/#/)).toBeNull()
  })

  it('draft episode (no numbering) → neutral label, no invented number', () => {
    // The PosterCard also labels the type ("Episode"), so it targets the numbering label:
    // the first span of the wrapper. Without numbering it is the neutral label, without "E\d".
    const { container } = render(<CardNumerado card={episodio(null)} />)
    const label = container.querySelector('.flex.flex-col.gap-1 > span')
    expect(label?.textContent).toBe('Episode')
    expect(screen.queryByText(/^E\d/)).toBeNull()
  })
})

// episodioComStill builds an episode card with the raw still_url of the provider.
function episodioComStill(stillUrl: string | null) {
  return card({
    id: 'ep1',
    title: 'Piloto',
    type: 'episode',
    numbering: { season: 1, number: 5 },
    still_url: stillUrl,
  })
}

describe('CardNumerado — 16:9 still of the episode (0042)', () => {
  const still = 'https://artworks.thetvdb.com/banners/episodes/72514/5.jpg'

  it('with a still: 16:9 thumbnail served by the image proxy', () => {
    const { container } = render(
      <CardNumerado card={episodioComStill(still)} />,
    )
    const img = screen.getByAltText('Still of Piloto') as HTMLImageElement
    // It routes through the proxy route (GET /images?src=) with the encoded raw URL — not the
    // direct URL of the source CDN.
    expect(img.getAttribute('src')).toBe(
      `/api/v1/images?src=${encodeURIComponent(still)}`,
    )
    // Episode framing itself (16:9), not the 2:3 poster frame.
    expect(container.querySelector('.aspect-video')).toBeTruthy()
    expect(container.querySelector('.aspect-\\[2\\/3\\]')).toBeNull()
    // The number E05 stays intact.
    expect(screen.getByText('E05')).toBeTruthy()
  })

  it('without a still: "No artwork" placeholder in the 16:9 frame (no image)', () => {
    const { container } = render(<CardNumerado card={episodioComStill(null)} />)
    expect(screen.getByTestId('poster-placeholder')).toBeTruthy()
    expect(screen.queryByAltText('Still of Piloto')).toBeNull()
    expect(container.querySelector('.aspect-video')).toBeTruthy()
    expect(screen.getByText('E05')).toBeTruthy()
  })
})
