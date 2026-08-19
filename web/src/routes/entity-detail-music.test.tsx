import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import { EntityDetailScreen } from '@/routes/entity-detail'
import { LanguageProvider } from '@/lib/language-context'
import type { EntityCard, ResolvedEntity } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import {
  criarFetchRouterEntity,
  entityCardBase as card,
} from '@/test/detail-harness'
import { baseEntity, okJson } from '@/test/entity-fixtures'

// erroMembros simula GET /entities/{id}/members falhando (500) — para exercitar
// o estado de erro de TracklistAlbum/DiscografiaArtista (mesmo shape de erro
// que o cliente HTTP, `lerJson`, espera).
function erroMembros(): Response {
  return {
    ok: false,
    status: 500,
    json: async () => ({
      error: { code: 'erro_interno', message: 'falha ao listar membros' },
    }),
  } as Response
}

// Testa as SEÇÕES DE MÚSICA (briefing 0111, ADR-0027) via EntityDetailScreen:
// álbum mostra a tracklist (TracklistAlbum), artista mostra a discografia
// (DiscografiaArtista) — e o teste de regressão garante que filme/série NÃO
// ganham essas seções (mesmo padrão de entity-detail-series.test.tsx).
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
    // Posição ordinal (1, 2) na ordem de chegada do array.
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

  it('não mostra a Discografia (seção de artista) no álbum', async () => {
    renderDetail(
      baseEntity({ id: 'e1', type: 'album', title: 'Álbum Teste' }),
      [faixa1],
    )
    await waitFor(() => expect(screen.getByText('Faixas')).toBeInTheDocument())
    expect(screen.queryByText('Discografia')).toBeNull()
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
    // A faixa solta ligada por music_artista_principal (ADR-0027) NÃO entra
    // na grade de discografia (só type=album).
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
