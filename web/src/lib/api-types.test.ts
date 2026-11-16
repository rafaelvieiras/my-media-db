import { describe, expect, it } from 'vitest'
import type { components } from '@/lib/api-types'

// LIGHTWEIGHT proof that the generated types from openapi.yaml (npm run gen:types) compile
// and match the back contract. If the generated shape diverges, the
// typed object below stops compiling in `tsc -b` — the test is the umbrella
// at runtime, the type check is the real value.
type ResolvedEntity = components['schemas']['ResolvedEntity']
type EntityList = components['schemas']['EntityList']

describe('api-types gerado', () => {
  it('modela a entidade resolvida com o shape final', () => {
    const ent: ResolvedEntity = {
      id: 'x',
      type: 'movie',
      title: 'Matrix',
      original_title: 'The Matrix',
      description: null,
      release_date: '1999-03-31',
      status: 'cataloged',
      tags: ['ação'],
      external_ids: { tmdb: '603' },
      owned: true,
      file_count: 1,
      poster_url: '/api/v1/entities/x/assets/poster',
      extension_data: {},
      lang: 'pt-BR',
      original_language: 'en',
      resolved_languages: { title: 'pt-BR', description: 'en' },
      // Rich fields (parity with the external detail).
      tagline: 'Bem-vindo ao mundo real.',
      keywords: ['simulação'],
      runtime: 136,
      certification: '14',
      vote_average: 8.2,
      local_rating: 9,
      backdrop_url: null,
      videos: [],
      collection: null,
      similar: [],
      external_links: { tmdb: 'https://www.themoviedb.org/movie/603' },
      ancestors: [],
      ownership: null,
      catalog_completeness: null,
      ratings: [{ source: 'tmdb', value: 8.2 }],
    }
    expect(ent.owned).toBe(true)
    expect(ent.external_ids.tmdb).toBe('603')
    expect(ent.resolved_languages.title).toBe('pt-BR')
  })

  it('modela a lista paginada de cards', () => {
    const page: EntityList = {
      items: [
        {
          id: 'x',
          type: 'movie',
          title: 'Matrix',
          status: 'cataloged',
          owned: true,
          file_count: 1,
          match_confidence: 0.9,
          poster_url: null,
          catalog_completeness: null,
        },
      ],
      total: 1,
      limit: 50,
      offset: 0,
    }
    expect(page.items).toHaveLength(1)
    expect(page.total).toBe(1)
  })
})
