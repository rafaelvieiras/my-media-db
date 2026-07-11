import { describe, expect, it } from 'vitest'
import { temOverrideEditado } from '@/lib/override-fields'
import { descritoresPadrao } from '@/test/entity-fixtures'

// A detecção de "editado" (0016) agora ITERA os descritores (0030) para saber se
// o campo é traduzível, em vez de o chamador passar um booleano hardcoded.

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
    // Com a chave presente e sem descritor, cai no ramo neutro → true.
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
