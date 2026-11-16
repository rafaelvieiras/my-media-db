// Pure helpers for the consumption history (ADR-0022) —
// kept apart from `components/historico-section.tsx` because a module that
// exports a component AND a const/function triggers `react-refresh/only-export-
// components` (the same reason `test/link-mock.tsx` stays isolated).

// KIND_ASSISTIR is the only kind emitted today (movie/series) — the Jellyfin webhook
// and the manual action of the "Gerenciar" dropdown write under it. The other
// components (list, badge) stay agnostic of kind; only the gate of the
// "watched" badge and the manual action need to know which kind to observe/write.
export const KIND_ASSISTIR = 'assistir'

// STATUS_COMPLETED is the status that marks the consumption as DONE — the only one that
// lights the "Assistido" badge (started/paused/stopped do not count as completed).
export const STATUS_COMPLETED = 'completed'

// ROTULOS_STATUS translates the vocabulary SUGGESTED by the contract (started/paused/
// stopped/completed) — not locked in the schema (free TEXT), so a status outside
// the list falls back (capitalizes the 1st letter) instead of vanishing.
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

// rotuloOrigemHistorico translates the `source` of the event — the owner needs to distinguish
// "came from Jellyfin" (automatic) from "I recorded it by hand" (manual), an explicit
// acceptance criterion. A source outside the map falls back to the raw value (it never
// hides the provenance).
const ROTULOS_ORIGEM: Record<string, string> = {
  manual: 'Registrado manualmente',
  'jellyfin-webhook': 'Jellyfin',
}

export function rotuloOrigemHistorico(source: string): string {
  return ROTULOS_ORIGEM[source] ?? source
}

// formatarCarimboHistorico returns date+time in the local pt-BR format — without a date
// lib (same choice as `job-card.tsx`, no precedent for a lib in the project).
export function formatarCarimboHistorico(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) {
    return iso
  }
  return d.toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' })
}
