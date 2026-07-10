// internalIdDe lê o campo ADITIVO `internal_id` (briefing 0026) de um objeto de DTO
// externo, de forma DEFENSIVA: devolve o id da entity INTERNA correspondente quando
// o item externo já foi importado, ou `null` quando não está na biblioteca (ou o
// campo ainda não existe no tipo gerado).
//
// O acesso vai por um tipo estreito local em vez do tipo gerado do OpenAPI porque o
// campo só entra nos tipos gerados após a regeneração do backend (feita em paralelo).
// O acesso é superset-compatível: quando os tipos ganham `internal_id`, continua
// válido sem mudança. Só `tmdb` resolve hoje; outras fontes vêm null (ver 0026).
export function internalIdDe(x: unknown): string | null {
  if (x && typeof x === 'object' && 'internal_id' in x) {
    const v = (x as { internal_id?: string | null }).internal_id
    return v ?? null
  }
  return null
}
