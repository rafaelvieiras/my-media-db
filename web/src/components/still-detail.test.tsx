import { describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import { RichDetailView } from '@/components/rich-detail-view'
import { richDetailBase } from '@/test/detail-harness'
import { renderWithClient } from '@/test/render'

// still-detail covers the large 16:9 STILL at the top of the EPISODE detail: the
// RichDetailView swaps the backdrop/poster for the still when the type is `episode`. With a still
// it shows the image (already served by the proxy); without a still it falls back to the placeholder. The other types
// do not get the still frame.
vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock,
  useNavigate: () => vi.fn(),
}))

describe('RichDetailView — episode still (0042)', () => {
  // The still URL already arrives READY (the deResolvedEntity adapter applied imagemProxySrc).
  const stillProxied = '/api/v1/images?src=https%3A%2F%2Fx%2Fs.jpg'

  it('episode with still: shows the large 16:9 still, not the 2:3 poster', () => {
    const { container } = renderWithClient(
      <RichDetailView
        detail={richDetailBase({ type: 'episode', stillUrl: stillProxied })}
        source="tmdb"
      />,
    )
    const img = screen.getByAltText('Still of Duna') as HTMLImageElement
    expect(img.getAttribute('src')).toBe(stillProxied)
    expect(container.querySelector('.aspect-video')).toBeTruthy()
    // The episode does not use the 2:3 poster frame.
    expect(screen.queryByAltText('Poster of Duna')).toBeNull()
  })

  it('episode without still: placeholder in the 16:9 frame', () => {
    const { container } = renderWithClient(
      <RichDetailView
        detail={richDetailBase({ type: 'episode', stillUrl: null })}
        source="tmdb"
      />,
    )
    expect(screen.getByTestId('still-placeholder')).toBeTruthy()
    expect(screen.queryByAltText('Still of Duna')).toBeNull()
    expect(container.querySelector('.aspect-video')).toBeTruthy()
  })

  it('non-episode: no still frame (keeps the 2:3 poster frame)', () => {
    const { container } = renderWithClient(
      <RichDetailView
        detail={richDetailBase({ type: 'movie', stillUrl: null })}
        source="tmdb"
      />,
    )
    // A movie does not get the still frame; the hero stays on the 2:3 poster frame.
    expect(screen.queryByTestId('still-placeholder')).toBeNull()
    expect(container.querySelector('.aspect-video')).toBeNull()
    expect(container.querySelector('.aspect-\\[2\\/3\\]')).toBeTruthy()
  })
})
