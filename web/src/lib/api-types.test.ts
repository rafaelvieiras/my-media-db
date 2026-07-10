import { describe, expect, it } from 'vitest'
import type { components } from '@/lib/api-types'

// Prova LEVE de que os tipos gerados do openapi.yaml (npm run gen:types) compilam
// e casam com o contrato do back (briefing 0010). Se o shape gerado divergir, o
// objeto tipado abaixo deixa de compilar no `tsc -b` — o teste é o guarda-chuva
// em runtime, a checagem de tipos é o valor real.
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
      // Campos ricos (paridade com o externo, 0022/0023).
      tagline: 'Bem-vindo ao mundo real.',
      keywords: ['simulação'],
      runtime: 136,
      certification: '14',
      vote_average: 8.2,
      backdrop_url: null,
      videos: [],
      collection: null,
      similar: [],
      external_links: { tmdb: 'https://www.themoviedb.org/movie/603' },
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
