import { describe, expect, it } from 'vitest'
import {
  LIMIAR_BAIXA_CONFIANCA,
  ehDraft,
  precisaRevisao,
  rotuloTipo,
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

describe('rotuloTipo', () => {
  it('traduz tipos conhecidos e ecoa desconhecidos', () => {
    expect(rotuloTipo('movie')).toBe('Filme')
    expect(rotuloTipo('tv')).toBe('Série')
    expect(rotuloTipo('other')).toBe('other')
  })
})
