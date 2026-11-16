// "Human" number formatting (pt-BR) shared between screens. Extracted
// so as not to duplicate the inline bytes/duration logic on every screen (jscpd) —
// today only `/health` uses it, but it is generic enough for other screens.

const UNIDADES_BYTES = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'] as const

// formatBytes converts a byte total into a short pt-BR label (decimal
// comma), e.g. "128,4 MB"/"1,86 TB". It goes up a unit every 1024; the decimal
// place grows (0 for B/KB, 2 above GB) so as not to lose precision on
// large sizes nor exaggerate on small ones.
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) {
    return '—'
  }
  if (bytes < 1024) {
    return `${bytes} B`
  }
  let valor = bytes
  let unidade = 0
  while (valor >= 1024 && unidade < UNIDADES_BYTES.length - 1) {
    valor /= 1024
    unidade += 1
  }
  const casas = unidade <= 1 ? 0 : unidade >= 3 ? 2 : 1
  return `${valor.toLocaleString('pt-BR', {
    minimumFractionDigits: casas,
    maximumFractionDigits: casas,
  })} ${UNIDADES_BYTES[unidade]}`
}

// formatDuration converts seconds into a short uptime label, e.g. "5d 3h",
// "3h 12min", "42s". It shows only the two largest non-zero components —
// enough for the owner to have a notion of "for how long" without an exact clock.
export function formatDuration(totalSeconds: number): string {
  if (!Number.isFinite(totalSeconds) || totalSeconds < 0) {
    return '—'
  }
  const segundos = Math.floor(totalSeconds)
  const dias = Math.floor(segundos / 86400)
  const horas = Math.floor((segundos % 86400) / 3600)
  const minutos = Math.floor((segundos % 3600) / 60)
  const restoSegundos = segundos % 60

  const partes: string[] = []
  if (dias > 0) {
    partes.push(`${dias}d`)
  }
  if (horas > 0) {
    partes.push(`${horas}h`)
  }
  if (minutos > 0) {
    partes.push(`${minutos}min`)
  }
  if (partes.length === 0) {
    partes.push(`${restoSegundos}s`)
  }
  return partes.slice(0, 2).join(' ')
}
