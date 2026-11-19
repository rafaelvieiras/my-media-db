import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { EntityDetailScreen } from '@/routes/entity-detail'
import { LanguageProvider } from '@/lib/language-context'
import { I18nProvider } from '@/lib/i18n'
import type { EntityCard, OverrideBlob, ResolvedEntity } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { entityCardBase as card } from '@/test/detail-harness'
import {
  baseEntity,
  okFields,
  okJson,
  respostaHistoricoVazia,
  settingsPadrao,
} from '@/test/entity-fixtures'

// Mocks the router: `useParams` fixes the id of the route; `useSearch` returns without an order
// (default view); `useNavigate` is a no-op; `Link` uses the shared mock
// (jscpd — `{...rest}` forwards `role`/other props that the Radix
// `asChild` of the DropdownMenuItem of "Manage" clones onto the child — without that the
// item would lose `role="menuitem"`).
vi.mock('@tanstack/react-router', async () => ({
  useParams: () => ({ id: 'e1' }),
  useSearch: () => ({ ordering: undefined }),
  useNavigate: () => vi.fn(),
  Link: (await import('@/test/link-mock')).LinkMock,
}))

// Routes the fetch: settings, descriptors (so the "edited" badge knows what is
// translatable), the resolved entity (`?lang=`), the files and the override
// blob (empty by default; some tests pass a blob for the "edited" badge).
function fetchRouter(
  entity: ResolvedEntity,
  blob: OverrideBlob = {},
  membros: EntityCard[] = [],
) {
  return vi.fn(async (url: string) => {
    const u = String(url)
    if (u === '/api/v1/settings/languages') {
      return okJson(settingsPadrao)
    }
    if (u === '/api/v1/schema/entity-fields') {
      return okFields()
    }
    if (u.startsWith('/api/v1/entities/e1/override')) {
      return okJson(blob)
    }
    if (u.startsWith('/api/v1/entities/e1/files')) {
      return okJson({ items: [] })
    }
    if (u.startsWith('/api/v1/entities/e1/assets')) {
      return okJson([])
    }
    if (u.startsWith('/api/v1/entities/e1/members')) {
      return okJson(membros)
    }
    // Consumption history: empty state (without the "Assistido" badge) and
    // without events (without the "Histórico" section) by default — the tests of this file
    // do not exercise the history itself (see historico-section.test.tsx).
    const historico = respostaHistoricoVazia(u)
    if (historico) {
      return historico
    }
    if (u.startsWith('/api/v1/entities/e1')) {
      return okJson(entity)
    }
    throw new Error(`rota nao mockada: ${u}`)
  })
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  vi.restoreAllMocks()
})

function renderDetail(
  entity: ResolvedEntity,
  blob?: OverrideBlob,
  membros?: EntityCard[],
) {
  vi.stubGlobal('fetch', fetchRouter(entity, blob, membros))
  return renderWithClient(
    <I18nProvider>
      <LanguageProvider>
        <EntityDetailScreen />
      </LanguageProvider>
    </I18nProvider>,
  )
}

describe('EntityDetailScreen — fallback badge', () => {
  it('renders the tags as badges', async () => {
    renderDetail(
      baseEntity({
        tags: ['Ficção científica', 'Aventura'],
        resolved_languages: { description: 'pt-BR', tags: 'pt-BR' },
      }),
    )

    await waitFor(() => expect(screen.getByText('Dune')).toBeInTheDocument())
    expect(screen.getByText('Ficção científica')).toBeInTheDocument()
    expect(screen.getByText('Aventura')).toBeInTheDocument()
    // Effective language == the one read (pt-BR): no badge.
    expect(screen.queryByText(/^in /)).not.toBeInTheDocument()
  })

  it('shows the badge when the description falls back (language != the one read)', async () => {
    renderDetail(
      baseEntity({
        description: 'Paul Atreides arrives on Arrakis.',
        resolved_languages: { description: 'en' },
      }),
    )

    await waitFor(() => expect(screen.getByText('Dune')).toBeInTheDocument())
    // Reading in pt-BR, but the description came in en → discreet badge.
    expect(screen.getByText('in English')).toBeInTheDocument()
  })

  it('shows the badge next to the tags when they fall back', async () => {
    renderDetail(
      baseEntity({
        tags: ['Science Fiction'],
        resolved_languages: { tags: 'en' },
      }),
    )

    await waitFor(() => expect(screen.getByText('Dune')).toBeInTheDocument())
    expect(screen.getByText('Science Fiction')).toBeInTheDocument()
    expect(screen.getByText('in English')).toBeInTheDocument()
  })
})

describe('EntityDetailScreen — status badge (0078)', () => {
  it('translates the cataloged status in the pt-BR interface', async () => {
    // The interface language is a browser preference (ADR-0032): pin it to pt-BR
    // before rendering so the translated badge is the one under test.
    localStorage.setItem('mmdb.ui-lang', 'pt-BR')
    renderDetail(baseEntity({ status: 'cataloged' }))

    await waitFor(() => expect(screen.getByText('Dune')).toBeInTheDocument())
    expect(screen.getByText('catalogado')).toBeInTheDocument()
    expect(screen.queryByText('cataloged')).not.toBeInTheDocument()
  })

  it('draft shows "unidentified", not the cataloged label', async () => {
    renderDetail(baseEntity({ status: 'draft' }))

    await waitFor(() => expect(screen.getByText('Dune')).toBeInTheDocument())
    expect(screen.getByText('unidentified')).toBeInTheDocument()
    expect(screen.queryByText('cataloged')).not.toBeInTheDocument()
  })
})

describe('EntityDetailScreen — "edited" badge and local rating', () => {
  it('shows the badge when the blob has an override in the current language', async () => {
    // title overridden in pt-BR (current language) → badge; en would not count.
    renderDetail(baseEntity(), { title: { 'pt-BR': 'Duna' } })

    await waitFor(() => expect(screen.getByText('Dune')).toBeInTheDocument())
    expect(screen.getByText('edited')).toBeInTheDocument()
  })

  it('without an override in the current language there is no "edited" badge', async () => {
    // Override only in en; reading in pt-BR → no badge.
    renderDetail(baseEntity(), { title: { en: 'Dune (EN)' } })

    await waitFor(() => expect(screen.getByText('Dune')).toBeInTheDocument())
    expect(screen.queryByText('edited')).not.toBeInTheDocument()
  })

  it('the resolved local rating appears in the hero (distinct from the source rating, 0086: row per source)', async () => {
    renderDetail(
      baseEntity({
        local_rating: 9.5,
        vote_average: 8.2,
        ratings: [{ source: 'tmdb', value: 8.2 }],
      }),
    )

    await waitFor(() => expect(screen.getByText('Dune')).toBeInTheDocument())
    expect(screen.getByLabelText('Your rating: 9.5')).toBeInTheDocument()
    expect(screen.getByText('8.2')).toBeInTheDocument()
  })

  it('"Edit" is an item of the "Manage" dropdown that links to the edit route', async () => {
    const user = userEvent.setup()
    renderDetail(baseEntity())

    await waitFor(() => expect(screen.getByText('Dune')).toBeInTheDocument())
    // The "Manage" dropdown only mounts its content in the DOM after it is opened
    // (Radix). In the Link mock, `to` becomes the href; the item uses asChild over the <Link>.
    await user.click(screen.getByRole('button', { name: /Manage/ }))
    const editar = screen.getByRole('menuitem', { name: /Edit/ })
    expect(editar).toBeInTheDocument()
  })
})

describe('EntityDetailScreen — series tree (0031)', () => {
  it('series (tv) lists the Seasons and shows no Files', async () => {
    renderDetail(baseEntity({ type: 'tv', title: 'Os Simpsons' }), {}, [
      card({ id: 's1', title: 'Temporada 1', type: 'season' }),
      card({ id: 's2', title: 'Temporada 2', type: 'season' }),
    ])

    await waitFor(() =>
      expect(screen.getByText('Os Simpsons')).toBeInTheDocument(),
    )
    expect(screen.getByRole('heading', { name: 'Seasons' })).toBeInTheDocument()
    expect(await screen.findByText('Temporada 1')).toBeInTheDocument()
    expect(screen.getByText('Temporada 2')).toBeInTheDocument()
    // Read-only (Phase 1): without the editable add/remove of the collection.
    expect(screen.queryByText('Add item')).toBeNull()
    // A series groups seasons — without a folder of its own.
    expect(screen.queryByRole('heading', { name: 'Files' })).toBeNull()
  })

  it('season lists the Episodes with the REAL number (0039), without Files', async () => {
    // Sparse library: only E01 and E05 (a hole in the middle). The card shows the REAL
    // number of the numbering (E01, E05), NOT the position in the list (which would be E01, E02).
    renderDetail(baseEntity({ type: 'season', title: 'Temporada 1' }), {}, [
      card({
        id: 'e1',
        title: 'Piloto',
        type: 'episode',
        numbering: { season: 1, number: 1 },
      }),
      card({
        id: 'e5',
        title: 'Quinto',
        type: 'episode',
        numbering: { season: 1, number: 5 },
      }),
    ])

    await waitFor(() =>
      expect(
        screen.getByRole('heading', { name: 'Episodes' }),
      ).toBeInTheDocument(),
    )
    expect(await screen.findByText('Piloto')).toBeInTheDocument()
    expect(screen.getByText('E01')).toBeInTheDocument()
    expect(screen.getByText('E05')).toBeInTheDocument()
    expect(screen.queryByText('E02')).toBeNull()
    expect(screen.queryByRole('heading', { name: 'Files' })).toBeNull()
  })

  it('episode shows Files and is not a parent node', async () => {
    renderDetail(baseEntity({ type: 'episode', title: 'Piloto' }))

    await waitFor(() => expect(screen.getByText('Piloto')).toBeInTheDocument())
    expect(screen.getByRole('heading', { name: 'Files' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Seasons' })).toBeNull()
    expect(screen.queryByRole('heading', { name: 'Episodes' })).toBeNull()
  })
})

// CATALOGING completeness section — DIFFERENT from ownership (0034/0036,
// covered above). It only appears when `catalog_completeness.complete === false`.
describe('EntityDetailScreen — cataloging completeness (0058)', () => {
  it('shows the "Incomplete cataloging" section with the list of what is missing', async () => {
    renderDetail(
      baseEntity({
        catalog_completeness: {
          complete: false,
          missing: ['cast', 'artwork:fanart'],
        },
      }),
    )

    await waitFor(() => expect(screen.getByText('Dune')).toBeInTheDocument())
    expect(screen.getByText('Incomplete cataloging')).toBeInTheDocument()
    expect(screen.getByText('cast')).toBeInTheDocument()
    expect(screen.getByText('fanart artwork')).toBeInTheDocument()
  })

  it('"media file" in the list comes with a link to Libraries (0077)', async () => {
    renderDetail(
      baseEntity({
        catalog_completeness: {
          complete: false,
          missing: ['cast', 'media_file'],
        },
      }),
    )

    await waitFor(() => expect(screen.getByText('Dune')).toBeInTheDocument())
    expect(screen.getByText('cast')).toBeInTheDocument()
    expect(screen.getByText('media file')).toBeInTheDocument()
    expect(
      screen.getByRole('link', { name: 'see Libraries' }),
    ).toBeInTheDocument()
  })

  it('does not show the section when complete', async () => {
    renderDetail(
      baseEntity({
        catalog_completeness: { complete: true, missing: [] },
      }),
    )

    await waitFor(() => expect(screen.getByText('Dune')).toBeInTheDocument())
    expect(screen.queryByText('Incomplete cataloging')).not.toBeInTheDocument()
  })

  it('does not show the section when null (draft/not evaluated yet)', async () => {
    renderDetail(baseEntity({ catalog_completeness: null }))

    await waitFor(() => expect(screen.getByText('Dune')).toBeInTheDocument())
    expect(screen.queryByText('Incomplete cataloging')).not.toBeInTheDocument()
  })
})

describe('EntityDetailScreen — suggested match warning', () => {
  it('shows "is this the right item?" in the hero when the backend kept a hint', async () => {
    renderDetail(
      baseEntity({
        type: 'tv',
        title: '2 Broken Girls',
        status: 'draft',
        match_suggestion: {
          source: 'tmdb',
          source_id: '39340',
          title: 'Duas Garotas em Apuros',
          year: '2011',
          poster_url: null,
          score: 95,
          query: '2 Brok',
          reason: 'name',
          can_apply: true,
          files_unmatched: 0,
          files_total: 0,
        },
      }),
    )

    const aviso = await screen.findByTestId('suggested-match-warning')
    expect(aviso).toBeInTheDocument()
    expect(screen.getByText('Duas Garotas em Apuros')).toBeInTheDocument()
  })

  it('shows no warning when the item has no hint', async () => {
    renderDetail(baseEntity({ type: 'tv', title: 'Serie Qualquer' }))

    await waitFor(() =>
      expect(screen.getByText('Serie Qualquer')).toBeInTheDocument(),
    )
    expect(
      screen.queryByTestId('suggested-match-warning'),
    ).not.toBeInTheDocument()
  })
})
