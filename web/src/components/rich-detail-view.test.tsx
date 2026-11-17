import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { RichDetailView } from '@/components/rich-detail-view'
import type { Asset, CreditCast, CreditCrew, ExternalVideo } from '@/lib/api'
import { richDetailBase } from '@/test/detail-harness'
import { renderWithClient } from '@/test/render'
import type { RichDetail } from '@/lib/rich-detail'

// Factories of mocked credits: the live backend does not return credits yet,
// so the sections are exercised with data built here.
function ator(i: number, over: Partial<CreditCast> = {}): CreditCast {
  return {
    source_id: String(i),
    name: `Ator ${i}`,
    character: `Personagem ${i}`,
    order: i,
    ...over,
  }
}

function membro(
  name: string,
  department: string,
  job: string,
  over: Partial<CreditCrew> = {},
): CreditCrew {
  return { source_id: name, name, department, job, ...over }
}

// Mocked Link/useNavigate (the view uses both in the collection/similars sections).
vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock,
  useNavigate: () => vi.fn(),
}))

// hero renders ONLY the HeroImersivo (via RichDetailView) for the cover/poster
// assertions below — the tests target the image block of the hero, not the
// whole page.
function hero(detail: RichDetail) {
  return renderWithClient(<RichDetailView detail={detail} source="tmdb" />)
}

describe('RichDetailView — album/artist hero (0111): 1:1 square cover', () => {
  it('album: 1:1 cover (rounded-xl), not the 2:3 poster', () => {
    const { container } = hero(
      richDetailBase({
        type: 'album',
        title: 'Rio',
        posterUrl: 'https://cdn.example/rio.jpg',
      }),
    )
    const img = screen.getByAltText('Cover of Rio') as HTMLImageElement
    expect(img.getAttribute('src')).toBe('https://cdn.example/rio.jpg')
    expect(container.querySelector('.aspect-square')).toBeTruthy()
    expect(container.querySelector('.aspect-\\[2\\/3\\]')).toBeNull()
    expect(container.querySelector('.rounded-full')).toBeNull()
  })

  it('artist: circular 1:1 photo (rounded-full)', () => {
    const { container } = hero(
      richDetailBase({
        type: 'artista',
        title: 'Duran Duran',
        posterUrl: 'https://cdn.example/dd.jpg',
      }),
    )
    const img = screen.getByAltText('Photo of Duran Duran') as HTMLImageElement
    expect(img.getAttribute('src')).toBe('https://cdn.example/dd.jpg')
    expect(container.querySelector('.aspect-square')).toBeTruthy()
    expect(container.querySelector('.rounded-full')).toBeTruthy()
  })

  it('no cover: placeholder in place of the image (without breaking)', () => {
    hero(richDetailBase({ type: 'album', title: 'Rio', posterUrl: null }))
    expect(screen.getByTestId('capa-placeholder')).toBeTruthy()
    expect(screen.queryByAltText('Cover of Rio')).toBeNull()
  })

  it('movie keeps the usual 2:3 poster (no regression)', () => {
    const { container } = hero(
      richDetailBase({
        type: 'movie',
        title: 'Duna',
        posterUrl: 'https://cdn.example/duna.jpg',
      }),
    )
    expect(screen.getByAltText('Poster of Duna')).toBeTruthy()
    expect(container.querySelector('.aspect-\\[2\\/3\\]')).toBeTruthy()
    expect(screen.queryByTestId('capa-placeholder')).toBeNull()
  })
})

describe('RichDetailView — artwork availability badges (0049)', () => {
  it('shows one badge per available kind, with label + count, in the canonical order', () => {
    renderWithClient(
      <RichDetailView
        detail={richDetailBase()}
        source="tmdb"
        availableArtwork={{ fanart: 40, poster: 23, logo: 17 }}
      />,
    )
    expect(screen.getByText('Poster 23')).toBeInTheDocument()
    expect(screen.getByText('Fanart 40')).toBeInTheDocument()
    expect(screen.getByText('Logo 17')).toBeInTheDocument()
  })

  it('without availableArtwork the row does not appear', () => {
    renderWithClient(<RichDetailView detail={richDetailBase()} source="tmdb" />)
    expect(screen.queryByText(/Poster \d/)).toBeNull()
  })
})

describe('RichDetailView — credits (cast/crew/companies, 0029)', () => {
  it('renders the three sections when not empty', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          cast: [ator(0)],
          crew: [membro('Denis Villeneuve', 'Direção', 'Diretor')],
          companies: [{ source_id: 'w', name: 'Warner Bros.' }],
        })}
      />,
    )
    expect(screen.getByText('Cast')).toBeInTheDocument()
    expect(screen.getByText('Crew')).toBeInTheDocument()
    expect(screen.getByText('Production companies')).toBeInTheDocument()
    expect(screen.getByText('Ator 0')).toBeInTheDocument()
    expect(screen.getByText('Denis Villeneuve')).toBeInTheDocument()
    expect(screen.getByText('Warner Bros.')).toBeInTheDocument()
  })

  it('sections disappear when the credits are empty', () => {
    renderWithClient(<RichDetailView detail={richDetailBase()} source="tmdb" />)
    expect(screen.queryByText('Cast')).toBeNull()
    expect(screen.queryByText('Crew')).toBeNull()
    expect(screen.queryByText('Production companies')).toBeNull()
  })

  it('cast shows the top 24 and "Show all (N)" expands the whole list', () => {
    const cast = Array.from({ length: 30 }, (_, i) => ator(i))
    renderWithClient(
      <RichDetailView source="tmdb" detail={richDetailBase({ cast })} />,
    )
    // The 25th (order 24) and the 26th stay hidden under the initial ceiling of 24.
    expect(screen.getByText('Ator 0')).toBeInTheDocument()
    expect(screen.getByText('Ator 23')).toBeInTheDocument()
    expect(screen.queryByText('Ator 24')).toBeNull()
    expect(screen.queryByText('Ator 25')).toBeNull()
    // It expands: it shows all of them and swaps the label to "Ver menos".
    fireEvent.click(screen.getByRole('button', { name: 'Show all (30)' }))
    expect(screen.getByText('Ator 24')).toBeInTheDocument()
    expect(screen.getByText('Ator 29')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Show less' }))
    expect(screen.queryByText('Ator 24')).toBeNull()
  })

  it('cast respects the billing order (order asc)', () => {
    // Out of order on input; only the one with the lowest `order` stays under the ceiling of 1... but here
    // we prove that the one with a high `order` disappears when there are more than the ceiling.
    const cast = Array.from({ length: 26 }, (_, i) => ator(25 - i)) // ordem invertida
    renderWithClient(
      <RichDetailView source="tmdb" detail={richDetailBase({ cast })} />,
    )
    // Sorted by `order`, Ator 0..23 appears; 24 and 25 fall outside the ceiling of 24.
    expect(screen.getByText('Ator 0')).toBeInTheDocument()
    expect(screen.queryByText('Ator 24')).toBeNull()
    expect(screen.queryByText('Ator 25')).toBeNull()
  })

  it('series: the cast card shows "· N eps"', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          type: 'series',
          cast: [ator(0, { character: 'Homer (voz)', episode_count: 12 })],
        })}
      />,
    )
    expect(screen.getByText(/Homer \(voz\) · 12 eps/)).toBeInTheDocument()
  })

  it('crew groups by department (Directing/Writing first, the rest alphabetical)', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          crew: [
            membro('Roger Deakins', 'Fotografia', 'Fotógrafo'),
            membro('Denis Villeneuve', 'Direção', 'Diretor'),
            membro('Jon Spaihts', 'Roteiro', 'Roteirista'),
            membro('Hans Zimmer', 'Som', 'Música'),
          ],
        })}
      />,
    )
    const grupos = screen
      .getAllByRole('heading', { level: 3 })
      .map((h) => h.textContent)
    expect(grupos).toEqual(['Direção', 'Roteiro', 'Fotografia', 'Som'])
    // Name + role of the member rendered together.
    expect(screen.getByText('Roger Deakins')).toBeInTheDocument()
    expect(screen.getByText(/— Fotógrafo/)).toBeInTheDocument()
  })

  it('cast: card with person_id becomes a link to /person/{id} and gains the "In the library" badge', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          cast: [ator(0, { name: 'Bryan Cranston', person_id: 'p1' })],
        })}
      />,
    )
    const link = screen.getByRole('link', {
      name: 'View Bryan Cranston in the library',
    })
    expect(link).toHaveAttribute('href', '/person/p1')
    expect(screen.getByText('In the library')).toBeInTheDocument()
  })

  it('cast: card without person_id is not an internal link and has no badge', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({ cast: [ator(0, { name: 'Ator Externo' })] })}
      />,
    )
    expect(
      screen.queryByRole('link', { name: /View Ator Externo in the library/ }),
    ).toBeNull()
    expect(screen.queryByText('In the library')).toBeNull()
  })

  it('crew: member with person_id becomes a link to /person/{id}; without person_id it keeps linking to TMDB', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          crew: [
            membro('Denis Villeneuve', 'Direção', 'Diretor', {
              person_id: 'p2',
            }),
            membro('Roger Deakins', 'Fotografia', 'Fotógrafo'),
          ],
        })}
      />,
    )
    const link = screen.getByRole('link', {
      name: 'View Denis Villeneuve in the library',
    })
    expect(link).toHaveAttribute('href', '/person/p2')
    // Without person_id, the name keeps linking to the source (TMDB) — unchanged behavior.
    const externo = screen.getByRole('link', { name: 'Roger Deakins' })
    expect(externo).toHaveAttribute(
      'href',
      'https://www.themoviedb.org/person/Roger Deakins',
    )
    expect(screen.getByText(/— Fotógrafo/)).toBeInTheDocument()
  })

  it('crew: Directing/Writing (prioritized) get a photo, like the cast PessoaCard', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          crew: [
            membro('Denis Villeneuve', 'Direção', 'Diretor', {
              profile_url: '/api/v1/images?src=denis',
            }),
            membro('Roger Deakins', 'Fotografia', 'Fotógrafo', {
              profile_url: '/api/v1/images?src=roger',
            }),
          ],
        })}
      />,
    )
    // Directing (prioritized): real photo via <img alt="Foto de X">, like the Cast.
    expect(
      screen.getByRole('img', { name: 'Photo of Denis Villeneuve' }),
    ).toHaveAttribute('src', '/api/v1/images?src=denis')
    // Photography (not prioritized): it stays in compact text, without a photo — even with
    // profile_url available.
    expect(
      screen.queryByRole('img', { name: 'Foto de Roger Deakins' }),
    ).toBeNull()
    expect(screen.getByText(/— Fotógrafo/)).toBeInTheDocument()
  })

  it('crew: prioritized department without profile_url degrades to the placeholder (like the cast)', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          crew: [membro('Denis Villeneuve', 'Direção', 'Diretor')],
        })}
      />,
    )
    expect(
      screen.queryByRole('img', { name: 'Photo of Denis Villeneuve' }),
    ).toBeNull()
    expect(screen.getAllByTestId('poster-placeholder').length).toBeGreaterThan(
      0,
    )
  })

  it('company without logo falls back to the name; with logo renders the already proxied image', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          companies: [
            { source_id: '1', name: 'Legendary' },
            {
              source_id: '2',
              name: 'Warner Bros.',
              logo_url: '/api/v1/images?src=logo',
            },
          ],
        })}
      />,
    )
    // Without a logo → name in text.
    expect(screen.getByText('Legendary')).toBeInTheDocument()
    // With a logo → <img> with the proxied URL directly (no re-proxying) and alt = name.
    const logo = screen.getByRole('img', { name: 'Warner Bros.' })
    expect(logo).toHaveAttribute('src', '/api/v1/images?src=logo')
  })
})

// ── Immersive hero: synopsis inside the hero ─────────────────────────────────

describe('RichDetailView — synopsis inside the hero (0082)', () => {
  it('the synopsis appears as a paragraph, without a separate "Synopsis" heading', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          description: 'Um hacker descobre a verdade.',
        })}
      />,
    )
    expect(
      screen.getByText('Um hacker descobre a verdade.'),
    ).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Sinopse' })).toBeNull()
  })

  it('without a synopsis, nothing appears in its place', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({ description: null })}
      />,
    )
    expect(screen.queryByRole('heading', { name: 'Sinopse' })).toBeNull()
  })
})

// ── Trailer ──────────────────────────────────────────────────────────────────

function videoFixture(over: Partial<ExternalVideo> = {}): ExternalVideo {
  return {
    name: 'Trailer Oficial',
    key: 'abc123',
    site: 'YouTube',
    type: 'Trailer',
    official: true,
    url: null,
    ...over,
  }
}

describe('RichDetailView — trailer button in the action bar (0082)', () => {
  it('with a YouTube trailer: the button appears and opens the SAME embedded player', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({ videos: [videoFixture()] })}
      />,
    )
    const botao = screen.getByRole('button', { name: 'Watch trailer' })
    fireEvent.click(botao)
    expect(screen.getByTitle('Trailer Oficial')).toHaveAttribute(
      'src',
      'https://www.youtube-nocookie.com/embed/abc123?autoplay=1',
    )
    fireEvent.click(screen.getByRole('button', { name: 'Close trailer' }))
    expect(screen.queryByTitle('Trailer Oficial')).toBeNull()
  })

  it('without a video of the Trailer type (only Teaser), the button does not appear', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          videos: [videoFixture({ type: 'Teaser' })],
        })}
      />,
    )
    expect(screen.queryByRole('button', { name: 'Watch trailer' })).toBeNull()
  })

  it('non-playable trailer (not YouTube), the button does not appear', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          videos: [videoFixture({ site: 'Vimeo', key: '' })],
        })}
      />,
    )
    expect(screen.queryByRole('button', { name: 'Watch trailer' })).toBeNull()
  })

  it('without any video, the button does not appear', () => {
    renderWithClient(<RichDetailView source="tmdb" detail={richDetailBase()} />)
    expect(screen.queryByRole('button', { name: 'Watch trailer' })).toBeNull()
  })
})

// ── Preferred logo, banner and disc + theme song (entity only) ───────────────

function assetFixture(over: Partial<Asset> = {}): Asset {
  return {
    id: 'a1',
    kind: 'logo',
    origin: 'user',
    preferred: true,
    is_principal: true,
    local_path: null,
    url: '/api/v1/entities/e1/assets/a1/file',
    ...over,
  }
}

// stubEntityFetch routes `/assets`, `/theme` (HEAD) and `/historico/estado` (the
// "Assistido" badge) to the given responses — the calls that the hero fires
// when `entityId` is present. `estado` defaults to empty (no badge).
function stubEntityFetch(
  assets: Asset[],
  temTema = false,
  estado: Record<string, unknown> = {},
) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      const u = String(url)
      if (u.includes('/theme')) {
        return temTema
          ? ({ ok: true, status: 200, json: async () => ({}) } as Response)
          : ({ ok: false, status: 404, json: async () => ({}) } as Response)
      }
      if (u.includes('/historico/estado')) {
        return {
          ok: true,
          status: 200,
          json: async () => ({ estado }),
        } as Response
      }
      if (u.includes('/assets')) {
        return { ok: true, status: 200, json: async () => assets } as Response
      }
      throw new Error(`rota nao mockada: ${u}`)
    }),
  )
}

// Shortcut common to the three descriptions below (logo/banner/disc, "Assistido" badge,
// theme song): they all render with the same entityId="e1"/source="tmdb" —
// only what `stubEntityFetch` returns changes.
function renderComEntity(detailOver: Partial<RichDetail> = {}) {
  return renderWithClient(
    <RichDetailView
      detail={richDetailBase(detailOver)}
      source="tmdb"
      entityId="e1"
    />,
  )
}

// Same for the "without entityId" case: none of the three sections calls `fetch`.
function stubFetchNaoDeveSerChamado() {
  const fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('RichDetailView — preferred logo/banner/disc (0082, entity)', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('with a preferred kind=logo asset, the logo replaces the text title', async () => {
    stubEntityFetch([assetFixture({ kind: 'logo' })])
    const { container } = renderComEntity()
    await waitFor(() =>
      expect(container.querySelector('img[alt="Duna"]')).toBeTruthy(),
    )
    expect(container.querySelector('img[alt="Duna"]')).toHaveAttribute(
      'src',
      '/api/v1/entities/e1/assets/a1/file',
    )
    expect(screen.queryByRole('heading', { level: 1, name: 'Duna' })).toBeNull()
  })

  it('without a kind=logo asset, the text title keeps showing (no regression)', async () => {
    stubEntityFetch([])
    renderComEntity()
    expect(
      await screen.findByRole('heading', { level: 1, name: 'Duna' }),
    ).toBeInTheDocument()
  })

  it.each([
    ['banner', 'b1'],
    ['disc', 'd1'],
  ] as const)(
    'with a preferred kind=%s asset, it appears (asset id=%s)',
    async (kind, id) => {
      const url = `/api/v1/entities/e1/assets/${id}/file`
      stubEntityFetch([assetFixture({ id, kind, url })])
      const { container } = renderComEntity()
      await waitFor(() =>
        expect(container.querySelector(`img[src="${url}"]`)).toBeTruthy(),
      )
    },
  )

  it('without entityId (external screen), it fetches no assets and shows no logo/banner/disc', () => {
    const fetchMock = stubFetchNaoDeveSerChamado()
    renderWithClient(<RichDetailView detail={richDetailBase()} source="tmdb" />)
    expect(fetchMock).not.toHaveBeenCalled()
  })
})

// ── "Assistido" badge ────────────────────────────────────────────────────────

describe('RichDetailView — "watched" badge (0094)', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it.each([
    ['completed', true],
    ['started', false],
  ] as const)(
    'watched event=%s: "Watched" badge visible=%s',
    async (status, deveAparecer) => {
      stubEntityFetch([], false, {
        assistir: {
          status,
          occurred_at: '2026-01-05T20:00:00Z',
          source: 'jellyfin-webhook',
        },
      })
      renderComEntity()
      if (deveAparecer) {
        expect(await screen.findByText(/Watched on/)).toBeInTheDocument()
      } else {
        await waitFor(() => expect(screen.queryByText(/Watched on/)).toBeNull())
      }
    },
  )

  it('without a watched event, the badge does not appear', async () => {
    stubEntityFetch([], false, {})
    renderComEntity()
    await waitFor(() => expect(screen.queryByText(/Watched on/)).toBeNull())
  })

  it('without entityId (external screen), the badge does not appear', () => {
    stubFetchNaoDeveSerChamado()
    renderWithClient(<RichDetailView detail={richDetailBase()} source="tmdb" />)
    expect(screen.queryByText(/Watched on/)).toBeNull()
  })
})

describe('RichDetailView — local theme song player (0082 §4.4)', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('with theme.* in the root folder, shows the "Play theme" button (no autoplay)', async () => {
    stubEntityFetch([], true)
    renderComEntity()
    const botao = await screen.findByRole('button', {
      name: 'Play theme',
    })
    expect(botao).toBeInTheDocument()
    // No autoplay: no "tocando" state (label "Pausar") before the action.
    expect(screen.queryByRole('button', { name: 'Pause theme' })).toBeNull()
  })

  it('without theme.*, the player does not appear', async () => {
    stubEntityFetch([], false)
    renderComEntity()
    await waitFor(() =>
      expect(screen.queryByRole('button', { name: /theme/ })).toBeNull(),
    )
  })
})

// ── Ratings row per source ───────────────────────────────────────────────────

describe('RichDetailView — ratings row per source (0086)', () => {
  it('appears even with a single source — it is not hidden waiting for 2+ (owner decision)', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({ ratings: [{ source: 'tmdb', value: 7.4 }] })}
      />,
    )
    // The badge of the known source (TMDB) uses the svg LOGO (`aria-hidden`), not
    // text — the rating itself (7.4) is visible text next to it.
    expect(screen.getByText('7.4')).toBeInTheDocument()
  })

  it('without a local rating or ratings, the whole row does not appear', () => {
    renderWithClient(<RichDetailView source="tmdb" detail={richDetailBase()} />)
    expect(screen.queryByText(/^\d\.\d$/)).toBeNull()
  })

  it('local rating + source WITHOUT a logo in the registry falls back to the uppercase label', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          localRating: 9,
          ratings: [{ source: 'meufornecedor', value: 6.1 }],
        })}
      />,
    )
    expect(screen.getByLabelText('Your rating: 9.0')).toBeInTheDocument()
    expect(screen.getByText('MEUFORNECEDOR')).toBeInTheDocument()
    expect(screen.getByText('6.1')).toBeInTheDocument()
  })

  it('two sources: one badge per entry, side by side', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          ratings: [
            { source: 'tmdb', value: 7.4 },
            { source: 'fake', value: 8.0 },
          ],
        })}
      />,
    )
    expect(screen.getByText('7.4')).toBeInTheDocument()
    expect(screen.getByText('8.0')).toBeInTheDocument()
    expect(screen.getByText('FAKE')).toBeInTheDocument()
  })
})

// ── Row of external links ────────────────────────────────────────────────────

describe('RichDetailView — external links row, right after the hero (0086)', () => {
  it('appears right after the hero, BEFORE the Keywords section', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          externalLinks: { tmdb: 'https://www.themoviedb.org/movie/603' },
          keywords: ['simulação'],
        })}
      />,
    )
    const link = screen.getByRole('link', { name: 'TMDB' })
    const heading = screen.getByRole('heading', { name: 'Keywords' })
    // DOCUMENT_POSITION_FOLLOWING in the result of `link.compareDocumentPosition`
    // means that `heading` comes AFTER `link` in the document.
    expect(
      link.compareDocumentPosition(heading) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
    expect(link).toHaveAttribute('href', 'https://www.themoviedb.org/movie/603')
  })

  it('without external links, the row does not appear', () => {
    renderWithClient(<RichDetailView source="tmdb" detail={richDetailBase()} />)
    expect(screen.queryByRole('link', { name: 'TMDB' })).toBeNull()
  })

  it('source without a logo in the registry falls back to the generic icon + uppercase name', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          externalLinks: { letterboxd: 'https://letterboxd.com/film/x' },
        })}
      />,
    )
    expect(screen.getByRole('link', { name: 'LETTERBOXD' })).toBeInTheDocument()
  })
})

// ── "Baixar trailer"/"Baixar tema" buttons ───────────────────────────────────

// Minimal Response-like response reused by the fetch stubs below —
// only the fields that the HTTP client of the app reads (`ok`/`status`/`json`).
function respostaJson(body: unknown, status = 200): Response {
  return { ok: status < 400, status, json: async () => body } as Response
}

// Routes the calls COMMON to any test with `entityId`: status of
// yt-dlp, state of the consumption history (watched badge) and list of
// assets (hero) — before falling into them, it delegates to the `extra` of each
// specific stub (trailer OR theme); `extra` returns `undefined` when the route
// is not its own, so the common routing goes on.
function stubFetchComYtdlp(
  ytdlpDisponivel: boolean,
  extra: (u: string) => Response | undefined,
) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      const u = String(url)
      if (u.includes('/settings/ytdlp')) {
        return respostaJson({
          disponivel: ytdlpDisponivel,
          origem: ytdlpDisponivel ? 'baixado' : 'nenhum',
          custom_binary_path: null,
          detalhe: null,
        })
      }
      const resp = extra(u)
      if (resp) return resp
      if (u.includes('/historico/estado')) {
        return respostaJson({ estado: {} })
      }
      if (u.includes('/assets')) {
        return respostaJson([])
      }
      throw new Error(`rota nao mockada: ${u}`)
    }),
  )
}

// `jaBaixado` simulates a trailer ALREADY materialized before the test starts; a
// successful POST on .../trailer/download also becomes `jaBaixado=true`
// from then on (the invalidate of the mutation redoes the HEAD).
function stubTrailerFetch({
  ytdlpDisponivel,
  jaBaixado = false,
}: {
  ytdlpDisponivel: boolean
  jaBaixado?: boolean
}) {
  let baixado = jaBaixado
  stubFetchComYtdlp(ytdlpDisponivel, (u) => {
    if (u.includes('/trailer/download')) {
      baixado = true
      return respostaJson({ local_path: '/lib/Duna (2024)-trailer.mp4' })
    }
    if (u.includes('/trailer')) {
      return baixado ? respostaJson({}) : respostaJson({}, 404)
    }
    if (u.includes('/theme')) {
      return respostaJson({}, 404)
    }
    return undefined
  })
}

describe('RichDetailView — "Download trailer" button (0102, ADR-0025)', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('without entityId (external screen), the button does not appear even with a trailer', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({ videos: [videoFixture()] })}
      />,
    )
    expect(
      screen.queryByRole('button', { name: /Download trailer/ }),
    ).toBeNull()
  })

  it('without a playable trailer, the button does not appear (even with entityId)', async () => {
    stubTrailerFetch({ ytdlpDisponivel: true })
    renderComEntity()
    await screen.findByRole('heading', { level: 1, name: 'Duna' })
    expect(
      screen.queryByRole('button', { name: /Download trailer/ }),
    ).toBeNull()
  })

  it('yt-dlp unavailable: the button does not appear', async () => {
    stubTrailerFetch({ ytdlpDisponivel: false })
    renderComEntity({ videos: [videoFixture()] })
    await screen.findByRole('button', { name: 'Watch trailer' })
    expect(
      screen.queryByRole('button', { name: /Download trailer/ }),
    ).toBeNull()
  })

  it('yt-dlp available and no local trailer: downloading ends with "trailer downloaded"', async () => {
    stubTrailerFetch({ ytdlpDisponivel: true })
    renderComEntity({ videos: [videoFixture()] })
    const botao = await screen.findByRole('button', {
      name: 'Download trailer',
    })
    fireEvent.click(botao)
    expect(
      await screen.findByRole('link', { name: /Trailer downloaded/ }),
    ).toHaveAttribute('href', '/api/v1/entities/e1/trailer')
  })

  it('trailer already downloaded before: shows the direct link, without offering a new download', async () => {
    stubTrailerFetch({ ytdlpDisponivel: true, jaBaixado: true })
    renderComEntity({ videos: [videoFixture()] })
    expect(
      await screen.findByRole('link', { name: /Trailer downloaded/ }),
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Download trailer' }),
    ).toBeNull()
  })
})

// `jaTemLocal` simulates a theme.* ALREADY materialized before the test starts; a
// successful POST on .../theme/download also becomes `jaTemLocal=true` from
// then on (the invalidate of the mutation redoes BOTH the candidate AND the HEAD
// that TemaMusicalPlayer uses).
function stubThemeDownloadFetch({
  ytdlpDisponivel,
  candidatoDisponivel,
  jaTemLocal = false,
}: {
  ytdlpDisponivel: boolean
  candidatoDisponivel: boolean
  jaTemLocal?: boolean
}) {
  let local = jaTemLocal
  stubFetchComYtdlp(ytdlpDisponivel, (u) => {
    if (u.includes('/theme/download')) {
      local = true
      return respostaJson({ local_path: '/lib/Duna (2024)/theme.mp3' })
    }
    if (u.includes('/theme/candidato')) {
      return respostaJson({
        disponivel: candidatoDisponivel,
        ja_tem_local: local,
      })
    }
    if (u.includes('/theme')) {
      return local ? respostaJson({}) : respostaJson({}, 404)
    }
    return undefined
  })
}

describe('RichDetailView — "Download theme" button (0103, ADR-0025)', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('without entityId (external screen), the button does not appear', () => {
    renderWithClient(<RichDetailView source="tmdb" detail={richDetailBase()} />)
    expect(screen.queryByRole('button', { name: /Download theme/ })).toBeNull()
  })

  it('without a ThemerrDB candidate, the button does not appear (the common case — partial coverage)', async () => {
    stubThemeDownloadFetch({
      ytdlpDisponivel: true,
      candidatoDisponivel: false,
    })
    renderComEntity()
    await screen.findByRole('heading', { level: 1, name: 'Duna' })
    await waitFor(() =>
      expect(
        screen.queryByRole('button', { name: /Download theme/ }),
      ).toBeNull(),
    )
  })

  it('yt-dlp unavailable: the button does not appear even with a candidate', async () => {
    stubThemeDownloadFetch({
      ytdlpDisponivel: false,
      candidatoDisponivel: true,
    })
    renderComEntity()
    await screen.findByRole('heading', { level: 1, name: 'Duna' })
    await waitFor(() =>
      expect(
        screen.queryByRole('button', { name: /Download theme/ }),
      ).toBeNull(),
    )
  })

  it('candidate and yt-dlp available: downloading makes the theme player appear and the button vanish', async () => {
    stubThemeDownloadFetch({
      ytdlpDisponivel: true,
      candidatoDisponivel: true,
    })
    renderComEntity()
    const botao = await screen.findByRole('button', { name: 'Download theme' })
    fireEvent.click(botao)
    expect(
      await screen.findByRole('button', { name: 'Play theme' }),
    ).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Download theme' })).toBeNull()
  })

  it('already has a local theme: the button does not appear (no `force`, never offers a re-download)', async () => {
    stubThemeDownloadFetch({
      ytdlpDisponivel: true,
      candidatoDisponivel: true,
      jaTemLocal: true,
    })
    renderComEntity()
    await screen.findByRole('button', { name: 'Play theme' })
    expect(screen.queryByRole('button', { name: 'Download theme' })).toBeNull()
  })
})
