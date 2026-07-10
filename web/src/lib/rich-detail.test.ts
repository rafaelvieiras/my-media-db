import { describe, expect, it } from 'vitest'
import { deExternalDetail, deResolvedEntity } from '@/lib/rich-detail'
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

  it('backdrop local (rota relativa) passa pela base; direto (URL) fica igual', () => {
    // Rota local do fanart cacheado → relativa (base vazia em teste = igual).
    const local = deResolvedEntity(
      baseEntity({ backdrop_url: '/api/v1/entities/e/assets/fanart' }),
    )
    expect(local.backdropUrl).toBe('/api/v1/entities/e/assets/fanart')
    // Fallback: URL direta da fonte fica intacta.
    const direto = deResolvedEntity(
      baseEntity({ backdrop_url: 'https://img.tmdb.org/b.jpg' }),
    )
    expect(direto.backdropUrl).toBe('https://img.tmdb.org/b.jpg')
    // Sem backdrop → null.
    const vazio = deResolvedEntity(baseEntity({ backdrop_url: null }))
    expect(vazio.backdropUrl).toBeNull()
  })
})
