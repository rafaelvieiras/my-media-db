import { describe, expect, it } from 'vitest'
import {
  precisaAtencaoCatalogacao,
  rotuloItemFaltando,
  rotuloTooltipCompletude,
} from '@/lib/catalog-completeness-badges'
import { translate } from '@/lib/i18n'
import type { Translate } from '@/lib/i18n/types'

// The interface defaults to English (ADR-0032); `translate` reads the real
// catalog without mounting the provider. `tPt` covers the explicit pt-BR case.
const t: Translate = (key, params) => translate('en', key, params)
const tPt: Translate = (key, params) => translate('pt-BR', key, params)

describe('precisaAtencaoCatalogacao', () => {
  it('null (draft or not evaluated yet) needs no attention', () => {
    expect(precisaAtencaoCatalogacao(null)).toBe(false)
  })

  it('complete true needs no attention', () => {
    expect(precisaAtencaoCatalogacao({ complete: true, missing: [] })).toBe(
      false,
    )
  })

  it('complete false needs attention', () => {
    expect(
      precisaAtencaoCatalogacao({ complete: false, missing: ['cast'] }),
    ).toBe(true)
  })
})

describe('rotuloItemFaltando', () => {
  it('translates every known field key', () => {
    expect(rotuloItemFaltando('description', t)).toBe('synopsis')
    expect(rotuloItemFaltando('runtime', t)).toBe('duration')
    expect(rotuloItemFaltando('certification', t)).toBe('age rating')
    expect(rotuloItemFaltando('keywords', t)).toBe('keywords')
    expect(rotuloItemFaltando('cast', t)).toBe('cast')
    expect(rotuloItemFaltando('media_file', t)).toBe('media file')
  })

  it('translates artwork:<kind> keys with the artwork prefix', () => {
    expect(rotuloItemFaltando('artwork:poster', t)).toBe('poster artwork')
    expect(rotuloItemFaltando('artwork:fanart', t)).toBe('fanart artwork')
    expect(rotuloItemFaltando('artwork:logo', t)).toBe('logo artwork')
    expect(rotuloItemFaltando('artwork:clearart', t)).toBe('clearart artwork')
    expect(rotuloItemFaltando('artwork:banner', t)).toBe('banner artwork')
    expect(rotuloItemFaltando('artwork:thumb', t)).toBe('thumb artwork')
    expect(rotuloItemFaltando('artwork:disc', t)).toBe('disc cover artwork')
  })

  it('echoes an unknown artwork kind raw, prefixed', () => {
    expect(rotuloItemFaltando('artwork:still', t)).toBe('still artwork')
  })

  it('echoes an unknown field key raw (never breaks the UI)', () => {
    expect(rotuloItemFaltando('outro_campo', t)).toBe('outro_campo')
  })

  it('follows the active locale (pt-BR)', () => {
    expect(rotuloItemFaltando('description', tPt)).toBe('sinopse')
    expect(rotuloItemFaltando('artwork:disc', tPt)).toBe('arte capa de disco')
  })
})

describe('rotuloTooltipCompletude', () => {
  it('builds the sentence with the translated list joined by commas', () => {
    expect(
      rotuloTooltipCompletude(
        {
          complete: false,
          missing: ['cast', 'artwork:fanart'],
        },
        t,
      ),
    ).toBe('Incomplete cataloging: cast, fanart artwork')
  })

  it('handles a single-item list', () => {
    expect(
      rotuloTooltipCompletude({ complete: false, missing: ['description'] }, t),
    ).toBe('Incomplete cataloging: synopsis')
  })

  it('returns the sentence with an empty list for a null cc', () => {
    expect(rotuloTooltipCompletude(null, t)).toBe('Incomplete cataloging: ')
  })
})
