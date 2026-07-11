import type { FieldDescriptor, OverrideBlob } from '@/lib/api'

// Helpers de LEITURA do blob CRU de override (briefing 0016) — sem JSX, para o
// detalhe e o editor compartilharem a detecção de "editado" POR IDIOMA sem
// esbarrar na regra react-refresh (que exige que módulos com componentes só
// exportem componentes).

// temOverrideEditado diz se há override NO IDIOMA ATUAL para o campo, lendo o
// blob CRU. Se o campo é traduzível ou não vem dos DESCRITORES (briefing 0030) —
// não mais de um booleano hardcoded pelo chamador:
//   - traduzível → `blob[campo]` é um objeto (mapa por idioma) que TEM a chave
//     `lang` (mesmo que o valor seja `null` = "definir vazio");
//   - neutro → a chave `blob[campo]` está presente (um valor para todos os idiomas).
// Distinto do selo de fallback "em {idioma}" (herdado): herdado × editado são
// estados diferentes e podem conviver.
export function temOverrideEditado(
  blob: OverrideBlob | undefined,
  campo: string,
  lang: string,
  fields: FieldDescriptor[] | undefined,
): boolean {
  if (!blob || !(campo in blob)) {
    return false
  }
  const traduzivel =
    fields?.find((f) => f.name === campo)?.translatable ?? false
  if (!traduzivel) {
    return true
  }
  const valor = blob[campo]
  return (
    typeof valor === 'object' &&
    valor !== null &&
    !Array.isArray(valor) &&
    lang in (valor as Record<string, unknown>)
  )
}
