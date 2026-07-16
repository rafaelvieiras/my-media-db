import { describe, expect, it } from 'vitest'
import {
  precisaAtencaoCatalogacao,
  rotuloItemFaltando,
  rotuloTooltipCompletude,
} from '@/lib/catalog-completeness-badges'

describe('precisaAtencaoCatalogacao', () => {
  it('null (draft ou ainda não avaliado) não precisa de atenção', () => {
    expect(precisaAtencaoCatalogacao(null)).toBe(false)
  })

  it('complete true não precisa de atenção', () => {
    expect(precisaAtencaoCatalogacao({ complete: true, missing: [] })).toBe(
      false,
    )
  })

  it('complete false precisa de atenção', () => {
    expect(
      precisaAtencaoCatalogacao({ complete: false, missing: ['cast'] }),
    ).toBe(true)
  })
})

describe('rotuloItemFaltando', () => {
  it('traduz cada chave de campo conhecida', () => {
    expect(rotuloItemFaltando('description')).toBe('sinopse')
    expect(rotuloItemFaltando('runtime')).toBe('duração')
    expect(rotuloItemFaltando('certification')).toBe('classificação indicativa')
    expect(rotuloItemFaltando('keywords')).toBe('palavras-chave')
    expect(rotuloItemFaltando('cast')).toBe('elenco')
    expect(rotuloItemFaltando('media_file')).toBe('arquivo de mídia')
  })

  it('traduz chaves artwork:<kind> prefixadas com "arte "', () => {
    expect(rotuloItemFaltando('artwork:poster')).toBe('arte poster')
    expect(rotuloItemFaltando('artwork:fanart')).toBe('arte fanart')
    expect(rotuloItemFaltando('artwork:logo')).toBe('arte logo')
    expect(rotuloItemFaltando('artwork:clearart')).toBe('arte clearart')
    expect(rotuloItemFaltando('artwork:banner')).toBe('arte banner')
    expect(rotuloItemFaltando('artwork:thumb')).toBe('arte thumb')
    expect(rotuloItemFaltando('artwork:disc')).toBe('arte capa de disco')
  })

  it('kind de arte desconhecido ecoa cru, prefixado', () => {
    expect(rotuloItemFaltando('artwork:still')).toBe('arte still')
  })

  it('chave de campo desconhecida ecoa crua (nunca quebra a UI)', () => {
    expect(rotuloItemFaltando('outro_campo')).toBe('outro_campo')
  })
})

describe('rotuloTooltipCompletude', () => {
  it('monta a frase com a lista traduzida e separada por vírgula', () => {
    expect(
      rotuloTooltipCompletude({
        complete: false,
        missing: ['cast', 'artwork:fanart'],
      }),
    ).toBe('Catalogação incompleta: elenco, arte fanart')
  })

  it('lista com um único item', () => {
    expect(
      rotuloTooltipCompletude({ complete: false, missing: ['description'] }),
    ).toBe('Catalogação incompleta: sinopse')
  })

  it('cc nulo devolve a frase com lista vazia', () => {
    expect(rotuloTooltipCompletude(null)).toBe('Catalogação incompleta: ')
  })
})
