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

describe('RichDetailView — still do episódio (0042)', () => {
  // The still URL already arrives READY (the deResolvedEntity adapter applied imagemProxySrc).
  const stillProxied = '/api/v1/images?src=https%3A%2F%2Fx%2Fs.jpg'

  it('episódio com still: mostra o still grande 16:9, sem o pôster 2:3', () => {
    const { container } = renderWithClient(
      <RichDetailView
        detail={richDetailBase({ type: 'episode', stillUrl: stillProxied })}
        source="tmdb"
      />,
    )
    const img = screen.getByAltText('Still de Duna') as HTMLImageElement
    expect(img.getAttribute('src')).toBe(stillProxied)
    expect(container.querySelector('.aspect-video')).toBeTruthy()
    // The episode does not use the 2:3 poster frame.
    expect(screen.queryByAltText('Pôster de Duna')).toBeNull()
  })

  it('episódio sem still: placeholder no frame 16:9', () => {
    const { container } = renderWithClient(
      <RichDetailView
        detail={richDetailBase({ type: 'episode', stillUrl: null })}
        source="tmdb"
      />,
    )
    expect(screen.getByTestId('still-placeholder')).toBeTruthy()
    expect(screen.queryByAltText('Still de Duna')).toBeNull()
    expect(container.querySelector('.aspect-video')).toBeTruthy()
  })

  it('não-episódio: sem frame de still (mantém o frame de pôster 2:3)', () => {
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
