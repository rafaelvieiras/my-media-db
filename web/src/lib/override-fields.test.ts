import { describe, expect, it } from 'vitest'
import { temOverrideEditado } from '@/lib/override-fields'
import { descritoresPadrao } from '@/test/entity-fixtures'

// The detection of "edited" now ITERATES the descriptors to know whether
// the field is translatable, instead of the caller passing a hardcoded boolean.

describe('temOverrideEditado', () => {
  it('traduzível: só conta com override no idioma corrente', () => {
    const blob = { title: { 'pt-BR': 'Duna' } }
    expect(temOverrideEditado(blob, 'title', 'pt-BR', descritoresPadrao)).toBe(
      true,
    )
    expect(temOverrideEditado(blob, 'title', 'en', descritoresPadrao)).toBe(
      false,
    )
  })

  it('neutro: basta a chave estar presente (vale p/ todos os idiomas)', () => {
    const blob = { original_title: 'Dune' }
    expect(
      temOverrideEditado(blob, 'original_title', 'en', descritoresPadrao),
    ).toBe(true)
  })

  it('campo ausente no blob → false', () => {
    expect(temOverrideEditado({}, 'title', 'pt-BR', descritoresPadrao)).toBe(
      false,
    )
  })

  it('sem descritores (carregando) trata como não-traduzível', () => {
    // With the key present and no descriptor, it falls into the neutral branch → true.
    expect(
      temOverrideEditado(
        { title: { 'pt-BR': 'Duna' } },
        'title',
        'pt-BR',
        undefined,
      ),
    ).toBe(true)
  })
})
