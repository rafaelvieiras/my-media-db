import { describe, expect, it, vi } from 'vitest'
import type { ReactNode } from 'react'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import {
  EpisodiosAgrupados,
  SeletorOrdenacao,
} from '@/components/episode-ordering'
import { renderWithClient } from '@/test/render'
import { episodioCard, grupoEpisodios, okJson } from '@/test/detail-harness'
import type { EpisodeGroups } from '@/lib/api'

// Testa o SELETOR de ordenação (0040) e a view AGRUPADA. CardNumerado (dentro dos
// grupos) usa PosterCard→Link, então mocka o Link; useLanguage vem do módulo real
// mockado por um idioma fixo (sem fetch de settings).
vi.mock('@tanstack/react-router', () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
}))
vi.mock('@/lib/language-context', () => ({
  useLanguage: () => ({
    lang: 'pt-BR',
    allowed: ['pt-BR', 'en'],
    primary: 'pt-BR',
    setLang: () => {},
  }),
}))

describe('SeletorOrdenacao', () => {
  it('lista as opções com rótulos amigáveis e emite a ordem CRUA ao trocar', () => {
    const onChange = vi.fn()
    renderWithClient(
      <SeletorOrdenacao
        value="official"
        options={['official', 'dvd', 'absolute', 'regional']}
        onChange={onChange}
      />,
    )
    const select = screen.getByLabelText(
      'Ordem de exibição dos episódios',
    ) as HTMLSelectElement
    // Rótulos amigáveis: as ordens conhecidas mapeadas e as demais capitalizadas.
    expect(screen.getByRole('option', { name: 'Oficial' })).toBeTruthy()
    expect(screen.getByRole('option', { name: 'DVD' })).toBeTruthy()
    expect(screen.getByRole('option', { name: 'Absoluto' })).toBeTruthy()
    expect(screen.getByRole('option', { name: 'Regional' })).toBeTruthy()
    // Trocar emite o valor CRU (dvd), não o rótulo.
    fireEvent.change(select, { target: { value: 'dvd' } })
    expect(onChange).toHaveBeenCalledWith('dvd')
  })
})

const groupsDvd: EpisodeGroups = {
  ordering: 'dvd',
  groups: [
    grupoEpisodios(1, 'Temporada 1', [episodioCard('ep1', 1, 1, 'Piloto')]),
    grupoEpisodios(2, 'Temporada 2', [episodioCard('ep2', 2, 3, 'Segundo')]),
  ],
}

describe('EpisodiosAgrupados — view agrupada', () => {
  it('renderiza as seções (cabeçalhos não-navegáveis) e os cards numerados', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        if (String(url).includes('/episodes?ordering=dvd')) {
          return okJson(groupsDvd)
        }
        throw new Error(`rota nao mockada: ${url}`)
      }),
    )
    renderWithClient(
      <EpisodiosAgrupados id="s1" ordering="dvd" seletor={null} />,
    )
    // Cabeçalhos de grupo (rótulos, não links).
    expect(await screen.findByText('Temporada 1')).toBeTruthy()
    expect(screen.getByText('Temporada 2')).toBeTruthy()
    // Números REAIS projetados na ordem dvd (E01 na T1, E03 na T2).
    expect(screen.getByText('E01')).toBeTruthy()
    expect(screen.getByText('E03')).toBeTruthy()
    vi.restoreAllMocks()
  })

  it('mostra o vazio quando a série não tem episódios na ordem', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => okJson({ ordering: 'dvd', groups: [] })),
    )
    renderWithClient(
      <EpisodiosAgrupados id="s1" ordering="dvd" seletor={null} />,
    )
    await waitFor(() =>
      expect(
        screen.getByText('Esta série ainda não tem episódios nesta ordem.'),
      ).toBeTruthy(),
    )
    vi.restoreAllMocks()
  })
})
