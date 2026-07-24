import { describe, expect, it } from 'vitest'
import {
  assetsPreferidosDoHero,
  infoKind,
  larguraKind,
  linhasDeArtwork,
  rotuloKind,
  selosDisponibilidade,
} from '@/lib/artwork'
import type { Asset, ArtworkItem, ArtworkResponse } from '@/lib/api'

function item(over: Partial<ArtworkItem> = {}): ArtworkItem {
  return {
    url: '/api/v1/images?src=x',
    source: 'fanarttv',
    is_preferred: false,
    is_local: false,
    is_local_in_library: false,
    is_principal: false,
    ...over,
  }
}

function asset(over: Partial<Asset> = {}): Asset {
  return {
    id: 'a1',
    kind: 'logo',
    origin: 'external',
    preferred: false,
    is_principal: true,
    local_path: null,
    url: '/api/v1/entities/e1/assets/a1/file',
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

  it('larguraKind: retrato/quadrado menores, paisagem/banner maiores, fallback w-52', () => {
    expect(larguraKind('poster')).toBe('w-52')
    expect(larguraKind('disc')).toBe('w-52')
    expect(larguraKind('fanart')).toBe('w-80')
    expect(larguraKind('banner')).toBe('w-96')
    expect(larguraKind('xpto')).toBe('w-52')
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

describe('assetsPreferidosDoHero — precedencia de servico (0017/0082)', () => {
  it('sem nenhuma arte preferida, cai na origin=external (caso normal de midia local importada)', () => {
    // Reproduz o caso de producao que motivou o fix: asset materializado da
    // pasta real via scanner/import, nunca passou pelo fluxo de "preferir".
    const assets = [
      asset({ kind: 'logo', origin: 'external', preferred: false }),
    ]
    expect(assetsPreferidosDoHero(assets).logo).toEqual(assets[0])
  })

  it('com uma preferida, ela vence mesmo havendo origin=user mais recente', () => {
    const preferida = asset({ id: 'p', kind: 'banner', preferred: true })
    const maisRecente = asset({ id: 'u', kind: 'banner', origin: 'user' })
    expect(assetsPreferidosDoHero([preferida, maisRecente]).banner).toEqual(
      preferida,
    )
  })

  it('sem preferida, origin=user (a mais recente/ultima do array) vence a external', () => {
    const externa = asset({ id: 'e', kind: 'disc', origin: 'external' })
    const doDono = asset({ id: 'u', kind: 'disc', origin: 'user' })
    expect(assetsPreferidosDoHero([externa, doDono]).disc).toEqual(doDono)
  })

  it('sem nenhum asset do kind, devolve undefined (sem regressao)', () => {
    const resultado = assetsPreferidosDoHero([])
    expect(resultado.logo).toBeUndefined()
    expect(resultado.banner).toBeUndefined()
    expect(resultado.disc).toBeUndefined()
  })

  it('assets undefined devolve os tres kinds undefined', () => {
    const resultado = assetsPreferidosDoHero(undefined)
    expect(resultado).toEqual({
      logo: undefined,
      banner: undefined,
      disc: undefined,
    })
  })
})
