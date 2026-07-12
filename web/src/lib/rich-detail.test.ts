import { describe, expect, it } from 'vitest'
import {
  deExternalDetail,
  deResolvedEntity,
  rotuloSituacao,
} from '@/lib/rich-detail'
import type { ExternalDetail } from '@/lib/api'
import { baseEntity } from '@/test/entity-fixtures'

// Os adapters normalizam os dois DTOs (detalhe externo × entity resolvida) no
// mesmo RichDetail. Aqui provamos o mapeamento e as duas regras próprias da
// entity: gêneros = tags, e o backdrop local (rota relativa) × direto (URL).

const externo: ExternalDetail = {
  source: 'tmdb',
  source_id: '603',
  type: 'movie',
  lang: 'pt-BR',
  title: 'Matrix',
  original_title: 'The Matrix',
  description: 'Neo desperta.',
  tagline: 'Bem-vindo ao mundo real.',
  release_date: '1999-03-31',
  year: '1999',
  runtime: 136,
  certification: '14',
  original_language: 'en',
  vote_average: 8.2,
  genres: ['Ação', 'Ficção científica'],
  keywords: ['simulação'],
  poster_url: 'https://img.tmdb.org/p.jpg',
  backdrop_url: 'https://img.tmdb.org/b.jpg',
  videos: [],
  collection: null,
  similar: [],
  external_links: { imdb: 'https://imdb/x' },
  resolved_languages: {},
}

describe('rich-detail adapters', () => {
  it('deExternalDetail mapeia genres e usa URLs diretas', () => {
    const r = deExternalDetail(externo)
    expect(r.genres).toEqual(['Ação', 'Ficção científica'])
    expect(r.posterUrl).toBe('https://img.tmdb.org/p.jpg')
    expect(r.backdropUrl).toBe('https://img.tmdb.org/b.jpg')
    expect(r.tagline).toBe('Bem-vindo ao mundo real.')
  })

  it('deResolvedEntity usa tags como gêneros e ano da release_date', () => {
    const r = deResolvedEntity(
      baseEntity({ tags: ['Drama'], release_date: '2021-09-15' }),
    )
    expect(r.genres).toEqual(['Drama'])
    expect(r.year).toBe('2021')
  })

  it('backdrop local (rota relativa) passa pela base; cru (URL) passa pelo proxy (0044)', () => {
    // Rota local do fanart cacheado (própria ou herdada) → relativa (base vazia em
    // teste = igual).
    const local = deResolvedEntity(
      baseEntity({ backdrop_url: '/api/v1/entities/e/assets/fanart' }),
    )
    expect(local.backdropUrl).toBe('/api/v1/entities/e/assets/fanart')
    // Fallback: URL CRUA da fonte agora passa pelo image proxy (0044) — não bate no CDN.
    const cru = 'https://img.tmdb.org/b.jpg'
    const direto = deResolvedEntity(baseEntity({ backdrop_url: cru }))
    expect(direto.backdropUrl).toBe(
      `/api/v1/images?src=${encodeURIComponent(cru)}`,
    )
    // Sem backdrop → null.
    const vazio = deResolvedEntity(baseEntity({ backdrop_url: null }))
    expect(vazio.backdropUrl).toBeNull()
  })

  it('mapeia airing_status (situação de série, 0045) nas duas fontes', () => {
    // Entity resolvida: e.airing_status → airingStatus.
    const ent = deResolvedEntity(baseEntity({ airing_status: 'Ended' }))
    expect(ent.airingStatus).toBe('Ended')
    // Detalhe externo: d.airing_status → airingStatus.
    const ext = deExternalDetail({
      ...externo,
      airing_status: 'Returning Series',
    })
    expect(ext.airingStatus).toBe('Returning Series')
    // Ausente (filme/só-TVDB sem match) → null (o selo não aparece).
    expect(deResolvedEntity(baseEntity({})).airingStatus).toBeNull()
    expect(deExternalDetail(externo).airingStatus).toBeNull()
  })

  it('still do episódio: custom (rota local) recebe a base; provider (cru) passa pelo proxy (0044)', () => {
    // Still CUSTOM do dono → rota local servível (recebe só a base).
    const custom = deResolvedEntity(
      baseEntity({
        type: 'episode',
        still_url: '/api/v1/entities/e/assets/still',
      }),
    )
    expect(custom.stillUrl).toBe('/api/v1/entities/e/assets/still')
    // Still do PROVIDER → URL crua, servida pelo image proxy.
    const cru = 'https://artworks.thetvdb.com/s.jpg'
    const provider = deResolvedEntity(
      baseEntity({ type: 'episode', still_url: cru }),
    )
    expect(provider.stillUrl).toBe(
      `/api/v1/images?src=${encodeURIComponent(cru)}`,
    )
  })
})

// rotuloSituacao traduz a situação CRUA (TMDB ou TVDB) para o rótulo pt-BR + variante
// do selo. O dado fica neutro no backend; a tradução é só de UI (0045).
describe('rotuloSituacao', () => {
  it('traduz o vocabulário TMDB', () => {
    expect(rotuloSituacao('Returning Series')).toEqual({
      label: 'Em exibição',
      variant: 'default',
    })
    expect(rotuloSituacao('Ended')).toEqual({
      label: 'Encerrada',
      variant: 'secondary',
    })
    expect(rotuloSituacao('Canceled')?.label).toBe('Cancelada')
    expect(rotuloSituacao('Cancelled')?.label).toBe('Cancelada')
    expect(rotuloSituacao('In Production')?.label).toBe('Em produção')
    expect(rotuloSituacao('Planned')?.label).toBe('Planejada')
    expect(rotuloSituacao('Pilot')?.label).toBe('Piloto')
  })

  it('traduz o vocabulário TVDB', () => {
    // "Continuing" (TVDB) equivale a "Returning Series" (TMDB) → mesmo rótulo.
    expect(rotuloSituacao('Continuing')).toEqual({
      label: 'Em exibição',
      variant: 'default',
    })
    expect(rotuloSituacao('Upcoming')?.label).toBe('Em breve')
  })

  it('ausente/vazio → null (o selo não é renderizado)', () => {
    expect(rotuloSituacao(null)).toBeNull()
    expect(rotuloSituacao(undefined)).toBeNull()
    expect(rotuloSituacao('   ')).toBeNull()
  })

  it('valor cru desconhecido → mostra o cru capitalizado (fallback gracioso)', () => {
    expect(rotuloSituacao('rumored')).toEqual({
      label: 'Rumored',
      variant: 'secondary',
    })
  })
})
