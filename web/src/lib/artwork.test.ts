import { describe, expect, it } from 'vitest'
import {
  infoKind,
  linhasDeArtwork,
  rotuloKind,
  selosDisponibilidade,
} from '@/lib/artwork'
import type { ArtworkItem, ArtworkResponse } from '@/lib/api'

function item(over: Partial<ArtworkItem> = {}): ArtworkItem {
  return {
    url: '/api/v1/images?src=x',
    source: 'fanarttv',
    is_preferred: false,
    is_local: false,
    ...over,
  }
}

describe('artwork — taxonomia de kinds (0049)', () => {
  it('rotuloKind traduz os kinds e cai no nome cru para o desconhecido', () => {
    expect(rotuloKind('poster')).toBe('Pôster')
    expect(rotuloKind('characterart')).toBe('Character')
    expect(rotuloKind('desconhecido')).toBe('desconhecido')
  })

  it('infoKind: pôster 2:3 cover, logo wide contido, disc quadrado contido', () => {
    expect(infoKind('poster')).toEqual({
      aspect: 'aspect-[2/3]',
      contain: false,
    })
    expect(infoKind('fanart').aspect).toBe('aspect-video')
    expect(infoKind('logo').contain).toBe(true)
    expect(infoKind('disc')).toEqual({ aspect: 'aspect-square', contain: true })
    // Kind desconhecido cai no 16:9 cover (mais seguro).
    expect(infoKind('xpto')).toEqual({ aspect: 'aspect-video', contain: false })
  })

  it('linhasDeArtwork extrai só os kinds presentes, na ordem canônica', () => {
    const art: ArtworkResponse = {
      fanart: [item()],
      poster: [item(), item()],
      logo: [],
      available: { poster: 2, fanart: 1 },
    }
    const linhas = linhasDeArtwork(art)
    // Pôster antes de fanart (ordem canônica); logo vazio é omitido.
    expect(linhas.map((l) => l.kind)).toEqual(['poster', 'fanart'])
    expect(linhas[0].items).toHaveLength(2)
  })

  it('linhasDeArtwork com undefined devolve vazio', () => {
    expect(linhasDeArtwork(undefined)).toEqual([])
  })

  it('selosDisponibilidade: só os kinds com count > 0, na ordem canônica', () => {
    const selos = selosDisponibilidade({ fanart: 40, poster: 23, disc: 0 })
    expect(selos).toEqual([
      { kind: 'poster', label: 'Pôster', count: 23 },
      { kind: 'fanart', label: 'Fanart', count: 40 },
    ])
  })

  it('selosDisponibilidade sem available devolve vazio', () => {
    expect(selosDisponibilidade(undefined)).toEqual([])
  })
})
