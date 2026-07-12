import { describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import { RichDetailView } from '@/components/rich-detail-view'
import { richDetailBase } from '@/test/detail-harness'
import { renderWithClient } from '@/test/render'

// Link/useNavigate mockados (a view usa ambos nas seções de coleção/similares).
vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock,
  useNavigate: () => vi.fn(),
}))

describe('RichDetailView — selos de disponibilidade de artes (0049)', () => {
  it('mostra um selo por kind disponível, com rótulo + contagem, na ordem canônica', () => {
    renderWithClient(
      <RichDetailView
        detail={richDetailBase()}
        source="tmdb"
        availableArtwork={{ fanart: 40, poster: 23, logo: 17 }}
      />,
    )
    expect(screen.getByText('Pôster 23')).toBeInTheDocument()
    expect(screen.getByText('Fanart 40')).toBeInTheDocument()
    expect(screen.getByText('Logo 17')).toBeInTheDocument()
  })

  it('sem availableArtwork a faixa não aparece', () => {
    renderWithClient(<RichDetailView detail={richDetailBase()} source="tmdb" />)
    expect(screen.queryByText(/Pôster \d/)).toBeNull()
  })
})
