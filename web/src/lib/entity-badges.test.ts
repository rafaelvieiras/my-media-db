import { describe, expect, it } from 'vitest'
import { translate } from '@/lib/i18n'
import {
  LIMIAR_BAIXA_CONFIANCA,
  ehDraft,
  estadoCompletude,
  precisaRevisao,
  rotuloStatus,
  rotuloTipo,
  rotuloTipoPlural,
  temCompletude,
  unidadeCompletude,
} from '@/lib/entity-badges'

// The label helpers take the translator (ADR-0032) — the catalog decides the
// language, so the test walks the three of them instead of hardcoding Portuguese.
const en = (key: string) => translate('en', key)
const pt = (key: string) => translate('pt-BR', key)

describe('ehDraft', () => {
  it('reconhece o status draft', () => {
    expect(ehDraft('draft')).toBe(true)
    expect(ehDraft('cataloged')).toBe(false)
  })
})

describe('precisaRevisao', () => {
  it('sinaliza confiança positiva abaixo do limiar', () => {
    expect(precisaRevisao({ status: 'cataloged', match_confidence: 40 })).toBe(
      true,
    )
  })

  it('não sinaliza confiança alta (>= limiar)', () => {
    expect(
      precisaRevisao({
        status: 'cataloged',
        match_confidence: LIMIAR_BAIXA_CONFIANCA,
      }),
    ).toBe(false)
    expect(precisaRevisao({ status: 'cataloged', match_confidence: 100 })).toBe(
      false,
    )
  })

  it('não sinaliza ausência de match (null ou zero)', () => {
    expect(
      precisaRevisao({ status: 'cataloged', match_confidence: null }),
    ).toBe(false)
    expect(precisaRevisao({ status: 'cataloged', match_confidence: 0 })).toBe(
      false,
    )
  })

  it('draft nunca precisa de revisão (tem rótulo próprio)', () => {
    expect(precisaRevisao({ status: 'draft', match_confidence: 40 })).toBe(
      false,
    )
  })
})

describe('rotuloStatus', () => {
  it('translates cataloged and echoes an unknown value', () => {
    expect(rotuloStatus('cataloged', en)).toBe('cataloged')
    expect(rotuloStatus('cataloged', pt)).toBe('catalogado')
    expect(rotuloStatus('outro', en)).toBe('outro')
  })
})

describe('rotuloTipo', () => {
  it('translates known types and echoes an unknown one', () => {
    expect(rotuloTipo('movie', en)).toBe('Movie')
    expect(rotuloTipo('tv', pt)).toBe('Série')
    expect(rotuloTipo('other', en)).toBe('other')
  })

  it('translates the types of the series tree (0031)', () => {
    expect(rotuloTipo('collection', en)).toBe('Collection')
    expect(rotuloTipo('season', en)).toBe('Season')
    expect(rotuloTipo('episode', en)).toBe('Episode')
  })

  it('translates the types of the music vertical (0105-0107)', () => {
    expect(rotuloTipo('album', en)).toBe('Album')
    expect(rotuloTipo('artista', en)).toBe('Artist')
    expect(rotuloTipo('faixa', en)).toBe('Track')
  })
})

describe('rotuloTipoPlural', () => {
  it('translates known types in the plural, music included (0109/0110)', () => {
    expect(rotuloTipoPlural('movie', en)).toBe('Movies')
    expect(rotuloTipoPlural('tv', pt)).toBe('Séries')
    expect(rotuloTipoPlural('album', en)).toBe('Albums')
    expect(rotuloTipoPlural('artista', en)).toBe('Artists')
    expect(rotuloTipoPlural('faixa', en)).toBe('Tracks')
  })

  it('falls back to the singular for a type with no registered plural', () => {
    expect(rotuloTipoPlural('other', en)).toBe('other')
  })
})

describe('temCompletude', () => {
  it('só série e temporada agregam posse rasa (têm selo)', () => {
    expect(temCompletude('tv')).toBe(true)
    expect(temCompletude('season')).toBe(true)
  })

  it('movie/episode/collection não têm selo de completude', () => {
    expect(temCompletude('movie')).toBe(false)
    expect(temCompletude('episode')).toBe(false)
    expect(temCompletude('collection')).toBe(false)
    expect(temCompletude('outro')).toBe(false)
  })
})

describe('unidadeCompletude', () => {
  it('series count seasons and a season counts episodes (0037)', () => {
    expect(unidadeCompletude('tv', en)).toBe('seasons')
    expect(unidadeCompletude('season', pt)).toBe('episódios')
  })

  it('types without completeness fall back to the neutral plural', () => {
    expect(unidadeCompletude('movie', en)).toBe('items')
  })
})

describe('estadoCompletude', () => {
  it('completa quando possui todos os catalogados', () => {
    expect(estadoCompletude({ owned_count: 10, total_count: 10 })).toBe(
      'completa',
    )
  })

  it('parcial quando possui alguns', () => {
    expect(estadoCompletude({ owned_count: 3, total_count: 10 })).toBe(
      'parcial',
    )
  })

  it('vazia quando não possui nenhum, mas há catalogados', () => {
    expect(estadoCompletude({ owned_count: 0, total_count: 10 })).toBe('vazia')
  })

  it('sem selo (null) quando total é zero ou não há posse agregada', () => {
    expect(estadoCompletude({ owned_count: 0, total_count: 0 })).toBeNull()
    expect(estadoCompletude(null)).toBeNull()
    expect(estadoCompletude(undefined)).toBeNull()
  })
})
