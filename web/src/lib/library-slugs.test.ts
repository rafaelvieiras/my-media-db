import { describe, expect, it } from 'vitest'
import { slugParaTipo, tipoParaSlug } from '@/lib/library-slugs'

describe('slugParaTipo', () => {
  it('resolve os 3 slugs de topo conhecidos', () => {
    expect(slugParaTipo('movies')).toBe('movie')
    expect(slugParaTipo('series')).toBe('tv')
    expect(slugParaTipo('artists')).toBe('artista')
  })

  it('devolve undefined para slug desconhecido (nunca crash)', () => {
    expect(slugParaTipo('nao-existe')).toBeUndefined()
  })
})

describe('tipoParaSlug', () => {
  it('resolve o inverso dos 3 tipos de topo conhecidos', () => {
    expect(tipoParaSlug('movie')).toBe('movies')
    expect(tipoParaSlug('tv')).toBe('series')
    expect(tipoParaSlug('artista')).toBe('artists')
  })

  it('devolve undefined para um tipo sem slug mapeado (ex.: sub-nó)', () => {
    expect(tipoParaSlug('album')).toBeUndefined()
    expect(tipoParaSlug('season')).toBeUndefined()
  })
})
