import { describe, expect, it } from 'vitest'
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
  it('traduz cataloged e ecoa valor desconhecido', () => {
    expect(rotuloStatus('cataloged')).toBe('catalogado')
    expect(rotuloStatus('outro')).toBe('outro')
  })
})

describe('rotuloTipo', () => {
  it('traduz tipos conhecidos e ecoa desconhecidos', () => {
    expect(rotuloTipo('movie')).toBe('Filme')
    expect(rotuloTipo('tv')).toBe('Série')
    expect(rotuloTipo('other')).toBe('other')
  })

  it('traduz os tipos da árvore de séries (0031)', () => {
    expect(rotuloTipo('collection')).toBe('Coleção')
    expect(rotuloTipo('season')).toBe('Temporada')
    expect(rotuloTipo('episode')).toBe('Episódio')
  })

  it('traduz os tipos da vertical de música (0105-0107)', () => {
    expect(rotuloTipo('album')).toBe('Álbum')
    expect(rotuloTipo('artista')).toBe('Artista')
    expect(rotuloTipo('faixa')).toBe('Faixa')
  })
})

describe('rotuloTipoPlural', () => {
  it('traduz tipos conhecidos no plural, incluindo música (0109/0110)', () => {
    expect(rotuloTipoPlural('movie')).toBe('Filmes')
    expect(rotuloTipoPlural('tv')).toBe('Séries')
    expect(rotuloTipoPlural('album')).toBe('Álbuns')
    expect(rotuloTipoPlural('artista')).toBe('Artistas')
    expect(rotuloTipoPlural('faixa')).toBe('Faixas')
  })

  it('cai no singular de rotuloTipo para tipo sem plural cadastrado', () => {
    expect(rotuloTipoPlural('other')).toBe('other')
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
  it('série conta temporadas e temporada conta episódios (0037)', () => {
    expect(unidadeCompletude('tv')).toBe('temporadas')
    expect(unidadeCompletude('season')).toBe('episódios')
  })

  it('tipos sem completude caem no plural neutro', () => {
    expect(unidadeCompletude('movie')).toBe('itens')
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
