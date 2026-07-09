import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  ApiError,
  createLibrary,
  deleteLibrary,
  getEntityFiles,
  importExternal,
  listEntities,
  posterSrc,
  searchMedia,
  startScan,
  getHealth,
} from '@/lib/api'

// Mocka `fetch` por teste; restaura depois para não vazar entre casos.
afterEach(() => {
  vi.restoreAllMocks()
})

describe('getHealth', () => {
  it('retorna o corpo em resposta 2xx', async () => {
    const body = { status: 'ok', version: '1.2.3' }
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => body,
    } as Response)
    vi.stubGlobal('fetch', fetchMock)

    const health = await getHealth()

    expect(health).toEqual(body)
    // Caminho relativo (base vazio por padrão): /api/v1/health.
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/health')
  })

  it('lança em resposta não-2xx', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 503,
      json: async () => ({}),
    } as Response)
    vi.stubGlobal('fetch', fetchMock)

    await expect(getHealth()).rejects.toThrow('health respondeu HTTP 503')
  })
})

function okJson<T>(body: T, status = 200) {
  return { ok: true, status, json: async () => body } as Response
}

describe('createLibrary', () => {
  it('envia POST com o corpo e devolve a fonte criada', async () => {
    const source = {
      id: '1',
      path: '/m',
      type: 'movie',
      state: 'online',
      last_scan: null,
    }
    const fetchMock = vi.fn().mockResolvedValue(okJson(source, 201))
    vi.stubGlobal('fetch', fetchMock)

    const out = await createLibrary({ path: '/m', type: 'movie' })

    expect(out).toEqual(source)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/v1/library')
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body)).toEqual({ path: '/m', type: 'movie' })
  })

  it('lança ApiError com code/message do envelope em erro', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 422,
      json: async () => ({
        error: { code: 'invalid_path', message: 'caminho inválido' },
      }),
    } as Response)
    vi.stubGlobal('fetch', fetchMock)

    await expect(createLibrary({ path: '/x', type: 'movie' })).rejects.toThrow(
      'caminho inválido',
    )
    try {
      await createLibrary({ path: '/x', type: 'movie' })
    } catch (e) {
      expect(e).toBeInstanceOf(ApiError)
      expect((e as ApiError).code).toBe('invalid_path')
      expect((e as ApiError).status).toBe(422)
    }
  })

  it('cai em ApiError genérico quando o corpo não é o envelope', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => {
        throw new Error('sem corpo')
      },
    } as unknown as Response)
    vi.stubGlobal('fetch', fetchMock)

    await expect(createLibrary({ path: '/x', type: 'movie' })).rejects.toThrow(
      'HTTP 500',
    )
  })
})

describe('listEntities', () => {
  it('monta a querystring só com os params presentes', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(okJson({ items: [], total: 0, limit: 24, offset: 0 }))
    vi.stubGlobal('fetch', fetchMock)

    await listEntities({ type: 'movie', owned: true, limit: 24, offset: 48 })

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/entities?type=movie&owned=true&limit=24&offset=48',
    )
  })

  it('sem params consulta a rota nua', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(okJson({ items: [], total: 0, limit: 50, offset: 0 }))
    vi.stubGlobal('fetch', fetchMock)

    await listEntities()

    expect(fetchMock).toHaveBeenCalledWith('/api/v1/entities')
  })
})

describe('startScan / deleteLibrary / getEntityFiles', () => {
  it('startScan faz POST no endpoint de scan', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(okJson({ job_id: 'j1', status: 'accepted' }, 202))
    vi.stubGlobal('fetch', fetchMock)

    const out = await startScan('7')

    expect(out.job_id).toBe('j1')
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/library/7/scan', {
      method: 'POST',
    })
  })

  it('deleteLibrary usa DELETE e resolve no 204', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue({ ok: true, status: 204 } as Response)
    vi.stubGlobal('fetch', fetchMock)

    await expect(deleteLibrary('7')).resolves.toBeUndefined()
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/library/7', {
      method: 'DELETE',
    })
  })

  it('getEntityFiles lê a lista de arquivos', async () => {
    const fetchMock = vi.fn().mockResolvedValue(okJson({ items: [] }))
    vi.stubGlobal('fetch', fetchMock)

    const out = await getEntityFiles('9')

    expect(out.items).toEqual([])
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/entities/9/files')
  })
})

describe('searchMedia', () => {
  it('monta a querystring com q (e type quando presente)', async () => {
    const body = {
      query: 'matrix',
      local: [],
      external: [],
      external_available: true,
    }
    const fetchMock = vi.fn().mockResolvedValue(okJson(body))
    vi.stubGlobal('fetch', fetchMock)

    const out = await searchMedia('matrix', 'movie')

    expect(out).toEqual(body)
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/search?q=matrix&type=movie')
  })

  it('sem type consulta só com q', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      okJson({
        query: 'a b',
        local: [],
        external: [],
        external_available: false,
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await searchMedia('a b')

    // Espaço é codificado como + pela URLSearchParams.
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/search?q=a+b')
  })

  it('lança ApiError do envelope quando o backend rejeita', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 400,
      json: async () => ({
        error: { code: 'invalid_query', message: 'q obrigatório' },
      }),
    } as Response)
    vi.stubGlobal('fetch', fetchMock)

    await expect(searchMedia('x')).rejects.toThrow('q obrigatório')
  })
})

describe('importExternal', () => {
  it('faz POST com o corpo e devolve a entity resolvida', async () => {
    const entity = { id: 'ent-1', type: 'movie', title: 'Matrix' }
    const fetchMock = vi.fn().mockResolvedValue(okJson(entity, 201))
    vi.stubGlobal('fetch', fetchMock)

    const out = await importExternal({
      source: 'tmdb',
      source_id: '603',
      type: 'movie',
    })

    expect(out).toEqual(entity)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/v1/import')
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body)).toEqual({
      source: 'tmdb',
      source_id: '603',
      type: 'movie',
    })
  })

  it('resolve tanto no 201 (criado) quanto no 200 (já existia)', async () => {
    const entity = { id: 'ent-2', type: 'tv', title: 'Existente' }
    const fetchMock = vi.fn().mockResolvedValue(okJson(entity, 200))
    vi.stubGlobal('fetch', fetchMock)

    const out = await importExternal({ source_id: '9', type: 'tv' })

    expect(out.id).toBe('ent-2')
  })
})

describe('posterSrc', () => {
  it('respeita a base (vazia) e devolve a rota do pôster', () => {
    expect(posterSrc('/api/v1/entities/9/assets/poster')).toBe(
      '/api/v1/entities/9/assets/poster',
    )
  })

  it('devolve null sem arte', () => {
    expect(posterSrc(null)).toBeNull()
    expect(posterSrc(undefined)).toBeNull()
  })
})
