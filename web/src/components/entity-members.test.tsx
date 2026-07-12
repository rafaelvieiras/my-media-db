import { describe, expect, it, vi } from 'vitest'
import type { ReactNode } from 'react'
import { render, screen } from '@testing-library/react'
import { CardNumerado, CardTemporada } from '@/components/entity-members'
import { entityCardBase as card } from '@/test/detail-harness'
import type { EntityCard } from '@/lib/api'

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

// episodio monta um card de episodio com o numbering real dado (0039).
function episodio(numbering: EntityCard['numbering']) {
  return card({ id: 'ep1', title: 'Piloto', type: 'episode', numbering })
}

describe('CardNumerado — numbering real do episódio (0039)', () => {
  it('exibe o número REAL (E05), não a posição na lista', () => {
    render(<CardNumerado card={episodio({ season: 1, number: 5 })} />)
    expect(screen.getByText('E05')).toBeTruthy()
  })

  it('zero-padding a 2 dígitos (E01)', () => {
    render(<CardNumerado card={episodio({ season: 1, number: 1 })} />)
    expect(screen.getByText('E01')).toBeTruthy()
  })

  it('anexa o absoluto quando presente e distinto do número da ordem', () => {
    render(
      <CardNumerado
        card={episodio({ season: 1, number: 5, absolute_number: 128 })}
      />,
    )
    expect(screen.getByText('E05 · #128')).toBeTruthy()
  })

  it('NÃO anexa o absoluto quando é igual ao número da ordem', () => {
    render(
      <CardNumerado
        card={episodio({ season: 1, number: 5, absolute_number: 5 })}
      />,
    )
    expect(screen.getByText('E05')).toBeTruthy()
    expect(screen.queryByText(/#/)).toBeNull()
  })

  it('episódio draft (sem numbering) → rótulo neutro, sem número inventado', () => {
    // O PosterCard tambem rotula o tipo ("Episódio"), entao mira a label do numbering:
    // o primeiro span do wrapper. Sem numbering ela e o rotulo neutro, sem "E\d".
    const { container } = render(<CardNumerado card={episodio(null)} />)
    const label = container.querySelector('.flex.flex-col.gap-1 > span')
    expect(label?.textContent).toBe('Episódio')
    expect(screen.queryByText(/^E\d/)).toBeNull()
  })
})

// episodioComStill monta um card de episodio com still_url cru do provider (0042).
function episodioComStill(stillUrl: string | null) {
  return card({
    id: 'ep1',
    title: 'Piloto',
    type: 'episode',
    numbering: { season: 1, number: 5 },
    still_url: stillUrl,
  })
}

describe('CardNumerado — still 16:9 do episódio (0042)', () => {
  const still = 'https://artworks.thetvdb.com/banners/episodes/72514/5.jpg'

  it('com still: thumbnail 16:9 servido pelo image proxy', () => {
    const { container } = render(
      <CardNumerado card={episodioComStill(still)} />,
    )
    const img = screen.getByAltText('Still de Piloto') as HTMLImageElement
    // Roteia pela rota do proxy (GET /images?src=) com a URL crua encodada — nao a URL
    // direta do CDN da fonte.
    expect(img.getAttribute('src')).toBe(
      `/api/v1/images?src=${encodeURIComponent(still)}`,
    )
    // Enquadramento proprio do episodio (16:9), nao o frame de poster 2:3.
    expect(container.querySelector('.aspect-video')).toBeTruthy()
    expect(container.querySelector('.aspect-\\[2\\/3\\]')).toBeNull()
    // O numero E05 segue intacto (0039).
    expect(screen.getByText('E05')).toBeTruthy()
  })

  it('sem still: placeholder "Sem arte" no frame 16:9 (sem imagem)', () => {
    const { container } = render(<CardNumerado card={episodioComStill(null)} />)
    expect(screen.getByTestId('poster-placeholder')).toBeTruthy()
    expect(screen.queryByAltText('Still de Piloto')).toBeNull()
    expect(container.querySelector('.aspect-video')).toBeTruthy()
    expect(screen.getByText('E05')).toBeTruthy()
  })
})
