import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import { fireEvent } from '@testing-library/react'
import type { ReactNode } from 'react'
import { EntityDetailScreen } from '@/routes/entity-detail'
import { LanguageProvider } from '@/lib/language-context'
import { renderWithClient } from '@/test/render'
import type { EntityCard, EpisodeGroups, ResolvedEntity } from '@/lib/api'
import {
  entityCardBase,
  episodioCard,
  grupoEpisodios,
} from '@/test/detail-harness'
import { baseEntity, okJson, respostaComum } from '@/test/entity-fixtures'

// Tests the SERIES SECTION via EntityDetailScreen: the selector only appears with more
// than one order; switching navigates the search param (going back to the default removes the param); an
// alternative order renders the grouped view. The router is mocked with a mutable
// search (`state.search`) and a navigate spy.
const state = vi.hoisted(() => ({
  search: { ordering: undefined as string | undefined },
  navigate: vi.fn(),
}))

vi.mock('@tanstack/react-router', () => ({
  useParams: () => ({ id: 'e1' }),
  useSearch: () => state.search,
  useNavigate: () => state.navigate,
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
}))

// serieTV builds the resolved entity of a series with the given orders.
function serieTV(available: string[], deft = 'official'): ResolvedEntity {
  return baseEntity({
    type: 'tv',
    title: 'Série X',
    orderings: available.length ? { default: deft, available } : undefined,
  })
}

function fetchRouter(
  entity: ResolvedEntity,
  membros: EntityCard[],
  groups: EpisodeGroups,
) {
  return vi.fn(async (url: string) => {
    const u = String(url)
    const comum = respostaComum(u)
    if (comum) {
      return comum
    }
    if (u.includes('/episodes')) {
      return okJson(groups)
    }
    if (u.includes('/members')) {
      return okJson(membros)
    }
    if (u.startsWith('/api/v1/entities/e1')) {
      return okJson(entity)
    }
    throw new Error(`rota nao mockada: ${u}`)
  })
}

function renderSerie(
  entity: ResolvedEntity,
  membros: EntityCard[] = [],
  groups: EpisodeGroups = { ordering: 'dvd', groups: [] },
) {
  vi.stubGlobal('fetch', fetchRouter(entity, membros, groups))
  return renderWithClient(
    <LanguageProvider>
      <EntityDetailScreen />
    </LanguageProvider>,
  )
}

beforeEach(() => {
  localStorage.clear()
  state.search = { ordering: undefined }
  state.navigate = vi.fn()
})

afterEach(() => {
  vi.restoreAllMocks()
})

const temporada = entityCardBase({
  id: 'se1',
  type: 'season',
  title: 'Temporada 1',
})

describe('SecaoSerie — seletor de ordenação (0040)', () => {
  it('esconde o seletor quando há só uma ordem', async () => {
    renderSerie(serieTV(['official']), [temporada])
    // The default view (Temporadas) appears; the selector does not.
    expect(await screen.findByText('Temporadas')).toBeTruthy()
    expect(
      screen.queryByLabelText('Ordem de exibição dos episódios'),
    ).toBeNull()
  })

  it('mostra o seletor quando há mais de uma ordem, na view default', async () => {
    renderSerie(serieTV(['official', 'dvd', 'absolute']), [temporada])
    expect(await screen.findByText('Temporadas')).toBeTruthy()
    expect(
      screen.getByLabelText('Ordem de exibição dos episódios'),
    ).toBeTruthy()
  })

  it('trocar para uma ordem alternativa navega com o search param', async () => {
    renderSerie(serieTV(['official', 'dvd', 'absolute']), [temporada])
    const select = await screen.findByLabelText(
      'Ordem de exibição dos episódios',
    )
    fireEvent.change(select, { target: { value: 'dvd' } })
    expect(state.navigate).toHaveBeenCalledWith(
      expect.objectContaining({ search: { ordering: 'dvd' } }),
    )
  })

  it('voltar ao default REMOVE o param (search vazio)', async () => {
    // It starts in an alternative order (dvd): the grouped view is rendered.
    state.search = { ordering: 'dvd' }
    const groups: EpisodeGroups = {
      ordering: 'dvd',
      groups: [grupoEpisodios(1, 'Temporada 1', [episodioCard('ep1', 1, 1)])],
    }
    renderSerie(serieTV(['official', 'dvd', 'absolute']), [temporada], groups)
    // The grouped view shows the group and the numbered episode.
    expect(await screen.findByText('E01')).toBeTruthy()
    const select = screen.getByLabelText('Ordem de exibição dos episódios')
    fireEvent.change(select, { target: { value: 'official' } })
    expect(state.navigate).toHaveBeenCalledWith(
      expect.objectContaining({ search: {} }),
    )
  })

  it('ordem alternativa renderiza a view agrupada (busca /episodes)', async () => {
    state.search = { ordering: 'dvd' }
    const groups: EpisodeGroups = {
      ordering: 'dvd',
      groups: [grupoEpisodios(2, 'Temporada 2', [episodioCard('ep9', 2, 9)])],
    }
    renderSerie(serieTV(['official', 'dvd', 'absolute']), [temporada], groups)
    // Non-navigable group header + card numbered in the dvd order.
    expect(await screen.findByText('Temporada 2')).toBeTruthy()
    expect(screen.getByText('E09')).toBeTruthy()
    // The default view (season cards) is NOT the source here.
    await waitFor(() =>
      expect(screen.queryByText('Carregando temporadas…')).toBeNull(),
    )
  })
})
