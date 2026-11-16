import { describe, expect, it } from 'vitest'
import {
  deExternalDetail,
  deResolvedEntity,
  rotuloSituacao,
} from '@/lib/rich-detail'
import type { ExternalDetail } from '@/lib/api'
import { baseEntity } from '@/test/entity-fixtures'

// The adapters normalize both DTOs (external detail × resolved entity) into the
// same RichDetail. Here we prove the mapping and the two rules of the
// entity itself: genres = tags, and the local backdrop (relative route) × direct (URL).

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
    // Local route of the cached fanart (own or inherited) → relative (empty base in
    // the test = the same).
    const local = deResolvedEntity(
      baseEntity({ backdrop_url: '/api/v1/entities/e/assets/fanart' }),
    )
    expect(local.backdropUrl).toBe('/api/v1/entities/e/assets/fanart')
    // Fallback: the RAW URL of the source now goes through the image proxy — it does not hit the CDN.
    const cru = 'https://img.tmdb.org/b.jpg'
    const direto = deResolvedEntity(baseEntity({ backdrop_url: cru }))
    expect(direto.backdropUrl).toBe(
      `/api/v1/images?src=${encodeURIComponent(cru)}`,
    )
    // No backdrop → null.
    const vazio = deResolvedEntity(baseEntity({ backdrop_url: null }))
    expect(vazio.backdropUrl).toBeNull()
  })

  it('mapeia airing_status (situação de série, 0045) nas duas fontes', () => {
    // Resolved entity: e.airing_status → airingStatus.
    const ent = deResolvedEntity(baseEntity({ airing_status: 'Ended' }))
    expect(ent.airingStatus).toBe('Ended')
    // External detail: d.airing_status → airingStatus.
    const ext = deExternalDetail({
      ...externo,
      airing_status: 'Returning Series',
    })
    expect(ext.airingStatus).toBe('Returning Series')
    // Absent (movie/TVDB-only without a match) → null (the badge does not appear).
    expect(deResolvedEntity(baseEntity({})).airingStatus).toBeNull()
    expect(deExternalDetail(externo).airingStatus).toBeNull()
  })

  it('mapeia créditos (cast/crew/companies) nas duas fontes, com default vazio', () => {
    const cast = [
      { source_id: '1', name: 'Timothée', character: 'Paul', order: 0 },
    ]
    const crew = [
      { source_id: '9', name: 'Denis', department: 'Direção', job: 'Diretor' },
    ]
    const companies = [{ source_id: 'w', name: 'Warner' }]
    // External: they pass through (URLs already proxied by the backend).
    const ext = deExternalDetail({ ...externo, cast, crew, companies })
    expect(ext.cast).toEqual(cast)
    expect(ext.crew).toEqual(crew)
    expect(ext.companies).toEqual(companies)
    // Entity: parity — same mapping.
    const ent = deResolvedEntity(baseEntity({ cast, crew, companies }))
    expect(ent.cast).toEqual(cast)
    expect(ent.crew).toEqual(crew)
    expect(ent.companies).toEqual(companies)
    // Absent → empty lists (the section disappears in the UI).
    expect(deExternalDetail(externo).cast).toEqual([])
    expect(deResolvedEntity(baseEntity({})).companies).toEqual([])
  })

  it('still do episódio: custom (rota local) recebe a base; provider (cru) passa pelo proxy (0044)', () => {
    // CUSTOM still of the owner → servable local route (it only receives the base).
    const custom = deResolvedEntity(
      baseEntity({
        type: 'episode',
        still_url: '/api/v1/entities/e/assets/still',
      }),
    )
    expect(custom.stillUrl).toBe('/api/v1/entities/e/assets/still')
    // PROVIDER still → raw URL, served by the image proxy.
    const cru = 'https://artworks.thetvdb.com/s.jpg'
    const provider = deResolvedEntity(
      baseEntity({ type: 'episode', still_url: cru }),
    )
    expect(provider.stillUrl).toBe(
      `/api/v1/images?src=${encodeURIComponent(cru)}`,
    )
  })
})

// rotuloSituacao translates the RAW situation (TMDB or TVDB) into the pt-BR label + badge
// variant. The data stays neutral in the backend; the translation is UI only.
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
    // "Continuing" (TVDB) is equivalent to "Returning Series" (TMDB) → same label.
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
