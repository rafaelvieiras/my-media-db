import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { PosterCard } from '@/components/poster-card'
import type { EntityCard } from '@/lib/api'
import { I18nProvider } from '@/lib/i18n'
import { entityCardBase } from '@/test/detail-harness'

// card: same defaults as entityCardBase, it only swaps id/title to the values that
// this file already used (id 'x', title 'Matrix') — avoids duplicating the whole
// factory (jscpd) just because of those two fields.
function card(over: Partial<EntityCard> = {}): EntityCard {
  return entityCardBase({ id: 'x', title: 'Matrix', ...over })
}

// The <Link> of TanStack Router requires router context; for a unit test
// of the card, we mock it with a plain <a>. The `to` becomes `href` to assert the destination
// of the review badge (link to `/review`).
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

afterEach(() => {
  localStorage.clear()
})

describe('PosterCard', () => {
  it('shows the title, the type and the ownership badge', () => {
    render(<PosterCard card={card({ owned: true })} />)
    expect(screen.getByText('Matrix')).toBeInTheDocument()
    expect(screen.getByText('Movie')).toBeInTheDocument()
    expect(screen.getByText('in library')).toBeInTheDocument()
  })

  it('an owned movie shows "in library" in the top-right corner (0038)', () => {
    const { container } = render(<PosterCard card={card({ owned: true })} />)
    const overlayDireito = container.querySelector('.right-1\\.5')
    expect(overlayDireito).toBeTruthy()
    expect(overlayDireito?.textContent).toContain('in library')
  })

  it('without ownership it does not show the library badge', () => {
    render(<PosterCard card={card({ owned: false })} />)
    expect(screen.queryByText('in library')).not.toBeInTheDocument()
  })

  it('no card is dimmed (no opacity-60, 0038)', () => {
    const { container } = render(<PosterCard card={card({ owned: false })} />)
    expect(container.querySelector('.opacity-60')).toBeNull()
  })

  it('falls back to the placeholder when there is no artwork', () => {
    render(<PosterCard card={card({ poster_url: null })} />)
    expect(screen.getByTestId('poster-placeholder')).toBeInTheDocument()
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
  })

  it('renders the image when there is a poster', () => {
    render(
      <PosterCard
        card={card({ poster_url: '/api/v1/entities/x/assets/poster' })}
      />,
    )
    const img = screen.getByRole('img')
    expect(img).toHaveAttribute('src', '/api/v1/entities/x/assets/poster')
    expect(img).toHaveAttribute('alt', 'Poster for Matrix')
  })

  it('falls back to the placeholder when the image fails to load', () => {
    render(
      <PosterCard
        card={card({ poster_url: '/api/v1/entities/x/assets/poster' })}
      />,
    )
    fireEvent.error(screen.getByRole('img'))
    expect(screen.getByTestId('poster-placeholder')).toBeInTheDocument()
  })

  it('shows the review badge on low confidence and it links to /dashboard/review', () => {
    const { container } = render(
      <PosterCard card={card({ match_confidence: 40 })} />,
    )
    const badge = screen.getByText('review')
    expect(badge).toBeInTheDocument()
    expect(badge.closest('a')).toHaveAttribute('href', '/dashboard/review')
    // The review (attention) one stays in the top-LEFT, not in the ownership one.
    const overlayEsquerdo = container.querySelector('.left-1\\.5')
    expect(overlayEsquerdo?.textContent).toContain('review')
  })

  it('shows the not-identified badge for a draft and it links to /dashboard/review', () => {
    render(<PosterCard card={card({ status: 'draft' })} />)
    const badge = screen.getByText('not identified')
    expect(badge).toBeInTheDocument()
    expect(badge.closest('a')).toHaveAttribute('href', '/dashboard/review')
    expect(screen.queryByText('review')).not.toBeInTheDocument()
  })

  // One rendering test in a non-default locale: the default is English (ADR-0032), and
  // the pt-BR catalog is the one the provider picks up from localStorage.
  it('renders the pt-BR copy when the interface language is pt-BR', () => {
    localStorage.setItem('mmdb.ui-lang', 'pt-BR')
    render(
      <I18nProvider>
        <PosterCard
          card={card({
            owned: true,
            poster_url: '/api/v1/entities/x/assets/poster',
          })}
        />
      </I18nProvider>,
    )
    expect(screen.getByText('Filme')).toBeInTheDocument()
    expect(screen.getByText('na biblioteca')).toBeInTheDocument()
    expect(screen.getByRole('img')).toHaveAttribute('alt', 'Pôster de Matrix')
  })
})

// Completeness badge per type: the SERIES card counts SEASONS, the
// SEASON one counts EPISODES. The same state logic (completa/parcial/vazia) of the
// ownership badge, only with the unit swapped by the type. Movie/collection never have a badge.
describe('PosterCard — completeness badge per type (0037/0038)', () => {
  it('a PARTIAL series shows the compact "X/Y" in the top-right, unit in the aria-label', () => {
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
    // The full unit stays accessible in the aria-label/title, not in the visible text.
    expect(badge).toHaveAttribute('aria-label', 'has 3/38 seasons')
    expect(screen.queryByText('has 3/38 seasons')).not.toBeInTheDocument()
    // The ownership badge goes in the top-RIGHT overlay.
    const overlayDireito = container.querySelector('.right-1\\.5')
    expect(overlayDireito?.textContent).toContain('3/38')
    // A series with content does not show "in library" (no redundancy).
    expect(screen.queryByText('in library')).not.toBeInTheDocument()
  })

  it('a COMPLETE series shows the "complete" badge (✓ with aria-label) in the top-right', () => {
    const { container } = render(
      <PosterCard
        card={card({
          type: 'tv',
          ownership: { owned_count: 5, total_count: 5 },
        })}
      />,
    )
    const badge = screen.getByLabelText('complete')
    expect(badge).toBeInTheDocument()
    const overlayDireito = container.querySelector('.right-1\\.5')
    expect(overlayDireito?.contains(badge)).toBe(true)
    expect(screen.queryByText('in library')).not.toBeInTheDocument()
  })

  it('an EMPTY series shows no badge and does not dim the cover (0038)', () => {
    const { container } = render(
      <PosterCard
        card={card({
          type: 'tv',
          ownership: { owned_count: 0, total_count: 38 },
        })}
      />,
    )
    expect(screen.queryByText('0/38')).not.toBeInTheDocument()
    expect(screen.queryByLabelText(/seasons/)).not.toBeInTheDocument()
    expect(container.querySelector('.opacity-60')).toBeNull()
    // No ownership badge (not even "in library").
    expect(screen.queryByText('in library')).not.toBeInTheDocument()
  })

  it('a PARTIAL season shows "X/Y" with the unit "episodes" in the aria-label (0036 intact)', () => {
    render(
      <PosterCard
        card={card({
          type: 'season',
          ownership: { owned_count: 3, total_count: 10 },
        })}
      />,
    )
    const badge = screen.getByText('3/10')
    expect(badge).toHaveAttribute('aria-label', 'has 3/10 episodes')
  })

  it('a MOVIE has no badge even with ownership present', () => {
    render(
      <PosterCard
        card={card({
          type: 'movie',
          ownership: { owned_count: 1, total_count: 2 },
        })}
      />,
    )
    // movie does not count a unit: no "seasons"/"episodes" and no "complete".
    expect(screen.queryByText(/seasons|episodes/)).not.toBeInTheDocument()
    expect(screen.queryByText('complete')).not.toBeInTheDocument()
  })

  it('without ownership it shows no badge (movie/episode/collection)', () => {
    render(<PosterCard card={card({ type: 'tv' })} />)
    expect(screen.queryByText(/seasons/)).not.toBeInTheDocument()
  })
})

// CATALOGING completeness indicator — DIFFERENT from the ownership badge
// (covered above). Icon-only in the top-LEFT, next to the review
// badge. It only appears when `catalog_completeness.complete === false`.
describe('PosterCard — cataloging completeness indicator (0058)', () => {
  it('shows the alert icon when incomplete, with title/aria-label of the list', () => {
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
    const rotulo = 'Incomplete cataloging: cast, fanart artwork'
    const badge = screen.getByLabelText(rotulo)
    expect(badge).toBeInTheDocument()
    expect(badge).toHaveAttribute('title', rotulo)
    // It stays in the top-LEFT (next to the review badge), not in the ownership one.
    const overlayEsquerdo = container.querySelector('.left-1\\.5')
    expect(overlayEsquerdo?.contains(badge)).toBe(true)
  })

  it('does not show the icon when complete', () => {
    render(
      <PosterCard
        card={card({
          catalog_completeness: { complete: true, missing: [] },
        })}
      />,
    )
    expect(
      screen.queryByLabelText(/Incomplete cataloging/),
    ).not.toBeInTheDocument()
  })

  it('does not show the icon when null (draft/not evaluated by the job yet)', () => {
    render(<PosterCard card={card({ catalog_completeness: null })} />)
    expect(
      screen.queryByLabelText(/Incomplete cataloging/),
    ).not.toBeInTheDocument()
  })
})
