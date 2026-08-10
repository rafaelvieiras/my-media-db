// Helpers puros do histórico de consumo (briefing 0092/0094, ADR-0022) —
// separados de `components/historico-section.tsx` porque um módulo que
// exporta componente E const/função dispara `react-refresh/only-export-
// components` (mesmo motivo de `test/link-mock.tsx` ficar isolado).

// KIND_ASSISTIR é o único kind emitido hoje (filme/série) — o webhook Jellyfin
// (0093) e a ação manual do dropdown "Gerenciar" gravam sob ele. Os demais
// componentes (lista, badge) continuam agnósticos de kind; só o gate do selo de
// "assistido" e a ação manual precisam saber qual kind observar/escrever.
export const KIND_ASSISTIR = 'assistir'

// STATUS_COMPLETED é o status que marca o consumo como CONCLUÍDO — o único que
// acende o selo "Assistido" (started/paused/stopped não contam como concluído).
export const STATUS_COMPLETED = 'completed'

// ROTULOS_STATUS traduz o vocabulário SUGERIDO pelo contrato (started/paused/
// stopped/completed) — não travado no schema (TEXT livre), então um status fora
// da lista cai no fallback (capitaliza a 1ª letra) em vez de sumir.
const ROTULOS_STATUS: Record<string, string> = {
  started: 'Começou a assistir',
  paused: 'Pausou',
  stopped: 'Parou',
  completed: 'Assistiu',
}

export function rotuloStatusHistorico(status: string): string {
  const rotulo = ROTULOS_STATUS[status]
  if (rotulo) {
    return rotulo
  }
  return status.length > 0 ? status[0].toUpperCase() + status.slice(1) : status
}

// rotuloOrigemHistorico traduz a `source` do evento — o dono precisa distinguir
// "veio do Jellyfin" (automático) de "registrei na mão" (manual), critério de
// aceite explícito do briefing. Fonte fora do mapa cai no valor cru (nunca
// esconde a proveniência).
const ROTULOS_ORIGEM: Record<string, string> = {
  manual: 'Registrado manualmente',
  'jellyfin-webhook': 'Jellyfin',
}

export function rotuloOrigemHistorico(source: string): string {
  return ROTULOS_ORIGEM[source] ?? source
}

// formatarCarimboHistorico devolve data+hora no formato local pt-BR — sem lib de
// data (mesma escolha do `job-card.tsx`, sem precedente de lib no projeto).
export function formatarCarimboHistorico(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) {
    return iso
  }
  return d.toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' })
}
