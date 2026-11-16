import type { FieldDescriptor, OverrideBlob } from '@/lib/api'

// READ helpers for the RAW override blob — no JSX, so the
// detail page and the editor share the detection of "edited" PER LANGUAGE without
// tripping the react-refresh rule (which requires modules with components to
// export only components).

// temOverrideEditado says whether there is an override IN THE CURRENT LANGUAGE for the field, reading the
// RAW blob. Whether the field is translatable or not comes from the DESCRIPTORS —
// no longer from a boolean hardcoded by the caller:
//   - translatable → `blob[campo]` is an object (map by language) that HAS the key
//     `lang` (even when the value is `null` = "set empty");
//   - neutral → the key `blob[campo]` is present (a single value for all languages).
// Distinct from the "em {idioma}" fallback badge (inherited): inherited × edited are
// different states and can coexist.
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
