import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import { fireEvent } from '@testing-library/react'
import type { ReactNode } from 'react'
import { EntityDetailScreen } from '@/routes/entity-detail'
import { LanguageProvider } from '@/lib/language-context'
import { I18nProvider } from '@/lib/i18n'
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
    <I18nProvider>
      <LanguageProvider>
        <EntityDetailScreen />
      </LanguageProvider>
    </I18nProvider>,
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

describe('SecaoSerie — ordering selector (0040)', () => {
  it('hides the selector when there is only one order', async () => {
    renderSerie(serieTV(['official']), [temporada])
    // The default view (Seasons) appears; the selector does not.
    expect(await screen.findByText('Seasons')).toBeTruthy()
    expect(screen.queryByLabelText('Episode display order')).toBeNull()
  })

  it('shows the selector when there is more than one order, in the default view', async () => {
    renderSerie(serieTV(['official', 'dvd', 'absolute']), [temporada])
    expect(await screen.findByText('Seasons')).toBeTruthy()
    expect(screen.getByLabelText('Episode display order')).toBeTruthy()
  })

  it('the ordering row wraps inside the section header (it used to push the page 51 px to the side at 390 px)', async () => {
    renderSerie(serieTV(['official', 'dvd', 'absolute']), [temporada])
    // The row that holds the selector + "Episode order" is the slot of the section header:
    // `min-w-0` lets it shrink and `flex-wrap` drops the button to a second line instead of
    // overflowing the viewport.
    const botao = await screen.findByRole('button', { name: 'Episode order' })
    const linha = botao.parentElement as HTMLElement
    expect(linha).toHaveClass('flex-wrap')
    expect(linha).toHaveClass('min-w-0')
  })

  it('switching to an alternative order navigates with the search param', async () => {
    renderSerie(serieTV(['official', 'dvd', 'absolute']), [temporada])
    const select = await screen.findByLabelText('Episode display order')
    fireEvent.change(select, { target: { value: 'dvd' } })
    expect(state.navigate).toHaveBeenCalledWith(
      expect.objectContaining({ search: { ordering: 'dvd' } }),
    )
  })

  it('going back to the default REMOVES the param (empty search)', async () => {
    // It starts in an alternative order (dvd): the grouped view is rendered.
    state.search = { ordering: 'dvd' }
    const groups: EpisodeGroups = {
      ordering: 'dvd',
      groups: [grupoEpisodios(1, 'Temporada 1', [episodioCard('ep1', 1, 1)])],
    }
    renderSerie(serieTV(['official', 'dvd', 'absolute']), [temporada], groups)
    // The grouped view shows the group and the numbered episode.
    expect(await screen.findByText('E01')).toBeTruthy()
    const select = screen.getByLabelText('Episode display order')
    fireEvent.change(select, { target: { value: 'official' } })
    expect(state.navigate).toHaveBeenCalledWith(
      expect.objectContaining({ search: {} }),
    )
  })

  it('an alternative order renders the grouped view (fetches /episodes)', async () => {
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
      expect(screen.queryByText('Loading seasons…')).toBeNull(),
    )
  })
})
