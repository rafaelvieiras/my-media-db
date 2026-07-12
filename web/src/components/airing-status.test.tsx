import { describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import { RichDetailView } from '@/components/rich-detail-view'
import { richDetailBase } from '@/test/detail-harness'
import { renderWithClient } from '@/test/render'

// airing-status cobre o SELO DE SITUAÇÃO da série (0045): a RichDetailView renderiza
// um <Badge> na linha do herói (junto da classificação) com o rótulo pt-BR quando a
// situação está presente. É metadado de série; sem o dado (filme) o selo não aparece.
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
