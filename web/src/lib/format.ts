// Formatação de números "humanos" (pt-BR) compartilhada entre telas. Extraído
// para não duplicar a lógica de bytes/duração inline em cada tela (jscpd) —
// hoje só `/health` (0055) usa, mas é genérico o bastante para outras telas.

const UNIDADES_BYTES = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'] as const

// formatBytes converte um total de bytes num rótulo curto em pt-BR (vírgula
// decimal), ex. "128,4 MB"/"1,86 TB". Sobe de unidade a cada 1024; a casa
// decimal cresce (0 em B/KB, 2 acima de GB) para não perder precisão em
// tamanhos grandes nem exagerar em pequenos.
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

// formatDuration converte segundos num rótulo curto de uptime, ex. "5d 3h",
// "3h 12min", "42s". Mostra só os dois maiores componentes não-zero — o
// suficiente para o dono ter noção de "há quanto tempo" sem um relógio exato.
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
