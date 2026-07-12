import { describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import { RichDetailView } from '@/components/rich-detail-view'
import { richDetailBase } from '@/test/detail-harness'
import { renderWithClient } from '@/test/render'

// still-detail cobre o STILL grande 16:9 no topo do detalhe do EPISODIO (0042): a
// RichDetailView troca o backdrop/poster pelo still quando o tipo e `episode`. Com still
// mostra a imagem (ja servida pelo proxy); sem still cai no placeholder. Os demais tipos
// nao ganham o frame de still.
vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock,
  useNavigate: () => vi.fn(),
}))

describe('RichDetailView — still do episódio (0042)', () => {
  // A URL de still ja chega PRONTA (o adapter deResolvedEntity aplicou imagemProxySrc).
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
    // O episodio nao usa o frame de poster 2:3.
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
    // Filme não ganha o frame de still; o hero segue no frame de pôster 2:3.
    expect(screen.queryByTestId('still-placeholder')).toBeNull()
    expect(container.querySelector('.aspect-video')).toBeNull()
    expect(container.querySelector('.aspect-\\[2\\/3\\]')).toBeTruthy()
  })
})
