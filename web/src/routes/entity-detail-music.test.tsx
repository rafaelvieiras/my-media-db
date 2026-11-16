import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { EntityDetailScreen } from '@/routes/entity-detail'
import { LanguageProvider } from '@/lib/language-context'
import type { EntityCard, ResolvedEntity } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import {
  criarFetchRouterEntity,
  entityCardBase as card,
} from '@/test/detail-harness'
import { baseEntity, okJson } from '@/test/entity-fixtures'

// erroMembros simulates GET /entities/{id}/members failing (500) — to exercise
// the error state of TracklistAlbum/DiscografiaArtista (same error shape
// that the HTTP client, `lerJson`, expects).
function erroMembros(): Response {
  return {
    ok: false,
    status: 500,
    json: async () => ({
      error: { code: 'erro_interno', message: 'falha ao listar membros' },
    }),
  } as Response
}

// Tests the MUSIC SECTIONS via EntityDetailScreen:
// an album shows the tracklist (TracklistAlbum), an artist shows the discography
// (DiscografiaArtista) — and the regression test guarantees that a movie/series does NOT
// get those sections (same pattern as entity-detail-series.test.tsx).
vi.mock('@tanstack/react-router', async () => ({
  useParams: () => ({ id: 'e1' }),
  useSearch: () => ({ ordering: undefined }),
  useNavigate: () => vi.fn(),
  Link: (await import('@/test/link-mock')).LinkMock,
}))

function renderDetail(
  entity: ResolvedEntity,
  membros: EntityCard[] = [],
  membrosResp?: () => Response,
) {
  vi.stubGlobal(
    'fetch',
    criarFetchRouterEntity(entity, membrosResp ?? (() => okJson(membros))),
  )
  return renderWithClient(
    <LanguageProvider>
      <EntityDetailScreen />
    </LanguageProvider>,
  )
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  vi.restoreAllMocks()
})

const faixa1 = card({ id: 'fx1', type: 'faixa', title: 'Rio' })
const faixa2 = card({ id: 'fx2', type: 'faixa', title: 'My Own Way' })
const album1 = card({ id: 'al1', type: 'album', title: 'Rio' })
const album2 = card({
  id: 'al2',
  type: 'album',
  title: 'Seven and the Ragged Tiger',
})
const faixaSolta = card({
  id: 'fx9',
  type: 'faixa',
  title: 'Colaboração avulsa',
})

describe('EntityDetailScreen — álbum: tracklist (0111)', () => {
  it('mostra as faixas do álbum, na ordem devolvida pela API', async () => {
    renderDetail(
      baseEntity({ id: 'e1', type: 'album', title: 'Álbum Teste' }),
      [faixa1, faixa2],
    )
    await waitFor(() => expect(screen.getByText('Faixas')).toBeInTheDocument())
    const secao = screen.getByText('Faixas').closest('section') as HTMLElement
    expect(within(secao).getByText('Rio')).toBeInTheDocument()
    expect(within(secao).getByText('My Own Way')).toBeInTheDocument()
    // Ordinal position (1, 2) in the arrival order of the array.
    expect(within(secao).getByText('1')).toBeInTheDocument()
    expect(within(secao).getByText('2')).toBeInTheDocument()
  })

  it('álbum sem faixas: estado vazio, sem quebrar', async () => {
    renderDetail(
      baseEntity({ id: 'e1', type: 'album', title: 'Álbum Teste' }),
      [],
    )
    await waitFor(() => expect(screen.getByText('Faixas')).toBeInTheDocument())
    expect(
      screen.getByText('Este álbum ainda não tem faixas catalogadas.'),
    ).toBeInTheDocument()
  })

  it('cada faixa linka para o seu próprio detalhe', async () => {
    renderDetail(
      baseEntity({ id: 'e1', type: 'album', title: 'Álbum Teste' }),
      [faixa1],
    )
    const link = await screen.findByText('Rio')
    expect(link.closest('a')?.getAttribute('href')).toBe('/entities/fx1')
  })

  it('não mostra a Discografia (seção de artista) no álbum, nem "Ver faixas faltando" sem mbid', async () => {
    renderDetail(
      baseEntity({ id: 'e1', type: 'album', title: 'Álbum Teste' }),
      [faixa1],
    )
    await waitFor(() => expect(screen.getByText('Faixas')).toBeInTheDocument())
    expect(screen.queryByText('Discografia')).toBeNull()
    expect(screen.queryByText('Ver faixas faltando')).toBeNull()
  })

  it('erro ao carregar as faixas: mostra o estado de erro, sem quebrar', async () => {
    renderDetail(
      baseEntity({ id: 'e1', type: 'album', title: 'Álbum Teste' }),
      [],
      erroMembros,
    )
    expect(
      await screen.findByText('Não foi possível carregar as faixas'),
    ).toBeInTheDocument()
  })

  // "Ver faixas faltando" (ADR-0029): link to the external
  // album screen, only when the album has external_ids.mbid (MusicBrainz).
  it('álbum com mbid: mostra o link "Ver faixas faltando" pro externo', async () => {
    renderDetail(
      baseEntity({
        id: 'e1',
        type: 'album',
        title: 'Álbum Teste',
        external_ids: { mbid: 'rg-1' },
      }),
      [faixa1],
    )
    const link = await screen.findByRole('link', {
      name: /Ver faixas faltando/,
    })
    expect(link.getAttribute('href')).toBe('/external/musicbrainz/album/rg-1')
  })
})

describe('EntityDetailScreen — artista: discografia (0111)', () => {
  it('mostra os ÁLBUNS do artista, filtrando faixas soltas fora', async () => {
    renderDetail(
      baseEntity({ id: 'e1', type: 'artista', title: 'Duran Duran' }),
      [album1, album2, faixaSolta],
    )
    await waitFor(() =>
      expect(screen.getByText('Discografia')).toBeInTheDocument(),
    )
    expect(screen.getByText('Rio')).toBeInTheDocument()
    expect(screen.getByText('Seven and the Ragged Tiger')).toBeInTheDocument()
    // The loose track linked by music_artista_principal (ADR-0027) does NOT enter
    // the discography grid (only type=album).
    expect(screen.queryByText('Colaboração avulsa')).toBeNull()
  })

  it('artista sem álbuns: estado vazio, sem quebrar', async () => {
    renderDetail(
      baseEntity({ id: 'e1', type: 'artista', title: 'Duran Duran' }),
      [],
    )
    await waitFor(() =>
      expect(screen.getByText('Discografia')).toBeInTheDocument(),
    )
    expect(
      screen.getByText('Este artista ainda não tem álbuns catalogados.'),
    ).toBeInTheDocument()
  })

  it('não mostra a tracklist (seção de álbum) no artista', async () => {
    renderDetail(
      baseEntity({ id: 'e1', type: 'artista', title: 'Duran Duran' }),
      [album1],
    )
    await waitFor(() =>
      expect(screen.getByText('Discografia')).toBeInTheDocument(),
    )
    expect(screen.queryByText('Faixas')).toBeNull()
  })
})

// ADR-0027: only the TRACK links directly to a media_file — an album/artist "owned"
// (owned:true via the tree, completarOwnedViaArvore) never has a real folder
// OF ITS OWN (resolverDestino does not choose among the directories of the several
// tracks). "Salvar informações (NFO)" would hit a 404 (no destination folder)
// if enabled with the raw `owned` — which is why the screen forces those two
// items to disabled in these two verticals, even with owned:true.
describe('EntityDetailScreen — música: "Salvar informações (NFO)" continua desabilitado mesmo com owned:true (regressão)', () => {
  it('álbum possuído via árvore: item fica desabilitado com tooltip', async () => {
    const user = userEvent.setup()
    renderDetail(
      baseEntity({
        id: 'e1',
        type: 'album',
        title: 'Álbum Teste',
        owned: true,
      }),
      [faixa1],
    )
    await waitFor(() => expect(screen.getByText('Faixas')).toBeInTheDocument())
    await user.click(screen.getByRole('button', { name: /Gerenciar/ }))
    const item = screen.getByRole('menuitem', {
      name: /Salvar informações \(NFO\)/,
    })
    expect(item).toHaveAttribute('aria-disabled', 'true')
  })

  it('artista possuído via árvore: item fica desabilitado com tooltip', async () => {
    const user = userEvent.setup()
    renderDetail(
      baseEntity({
        id: 'e1',
        type: 'artista',
        title: 'Duran Duran',
        owned: true,
      }),
      [album1],
    )
    await waitFor(() =>
      expect(screen.getByText('Discografia')).toBeInTheDocument(),
    )
    await user.click(screen.getByRole('button', { name: /Gerenciar/ }))
    const item = screen.getByRole('menuitem', {
      name: /Salvar informações \(NFO\)/,
    })
    expect(item).toHaveAttribute('aria-disabled', 'true')
  })
})

describe('EntityDetailScreen — regressão: filme/série sem seções de música (0111)', () => {
  it('filme: nem "Faixas" nem "Discografia" aparecem', async () => {
    renderDetail(baseEntity({ id: 'e1', type: 'movie', title: 'Dune' }))
    await waitFor(() => expect(screen.getByText('Dune')).toBeInTheDocument())
    expect(screen.queryByText('Faixas')).toBeNull()
    expect(screen.queryByText('Discografia')).toBeNull()
  })

  it('série: nem "Faixas" nem "Discografia" aparecem (a seção de série segue igual)', async () => {
    renderDetail(baseEntity({ id: 'e1', type: 'tv', title: 'Série X' }), [
      card({ id: 'se1', type: 'season', title: 'Temporada 1' }),
    ])
    await waitFor(() =>
      expect(screen.getByText('Temporadas')).toBeInTheDocument(),
    )
    expect(screen.queryByText('Faixas')).toBeNull()
    expect(screen.queryByText('Discografia')).toBeNull()
  })
})
