// internalIdDe reads the ADDITIVE `internal_id` field of an external DTO
// object, DEFENSIVELY: it returns the id of the corresponding INTERNAL entity when
// the external item was already imported, or `null` when it is not in the library (or the
// field does not exist in the generated type yet).
//
// The access goes through a local narrow type instead of the OpenAPI generated type because the
// field only enters the generated types after the backend regeneration (done in parallel).
// The access is superset-compatible: when the types gain `internal_id`, it stays
// valid without change. Only `tmdb` resolves today; other sources come back null.
export function internalIdDe(x: unknown): string | null {
  if (x && typeof x === 'object' && 'internal_id' in x) {
    const v = (x as { internal_id?: string | null }).internal_id
    return v ?? null
  }
  return null
}
