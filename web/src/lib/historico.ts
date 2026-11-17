// Pure helpers for the consumption history (ADR-0022) —
// kept apart from `components/historico-section.tsx` because a module that
// exports a component AND a const/function triggers `react-refresh/only-export-
// components` (the same reason `test/link-mock.tsx` stays isolated).
import type { Translate } from '@/lib/i18n/types'

// KIND_ASSISTIR is the only kind emitted today (movie/series) — the Jellyfin webhook
// and the manual action of the "Gerenciar" dropdown write under it. The other
// components (list, badge) stay agnostic of kind; only the gate of the
// "watched" badge and the manual action need to know which kind to observe/write.
export const KIND_ASSISTIR = 'assistir'

// STATUS_COMPLETED is the status that marks the consumption as DONE — the only one that
// lights the "watched" badge (started/paused/stopped do not count as completed).
export const STATUS_COMPLETED = 'completed'

// Catalog keys of the status vocabulary SUGGESTED by the contract (started/paused/
// stopped/completed) — not locked in the schema (free TEXT), so a status outside
// the list falls back (capitalizes the 1st letter) instead of vanishing.
const CHAVE_STATUS: Record<string, string> = {
  started: 'review.history.status.started',
  paused: 'review.history.status.paused',
  stopped: 'review.history.status.stopped',
  completed: 'review.history.status.completed',
}

export function rotuloStatusHistorico(status: string, t: Translate): string {
  const chave = CHAVE_STATUS[status]
  if (chave) {
    return t(chave)
  }
  return status.length > 0 ? status[0].toUpperCase() + status.slice(1) : status
}

// rotuloOrigemHistorico describes the `source` of the event — the owner needs to distinguish
// "came from Jellyfin" (automatic) from "I recorded it by hand" (manual), an explicit
// acceptance criterion. The provider name is data: it is written the same way in the
// three interface languages, so it is not a catalog key. A source outside the map
// falls back to the raw value (it never hides the provenance).
const CHAVE_ORIGEM: Record<string, string> = {
  manual: 'review.history.origin.manual',
}

const NOME_ORIGEM: Record<string, string> = {
  'jellyfin-webhook': 'Jellyfin',
}

export function rotuloOrigemHistorico(source: string, t: Translate): string {
  const chave = CHAVE_ORIGEM[source]
  if (chave) {
    return t(chave)
  }
  return NOME_ORIGEM[source] ?? source
}

// formatarCarimboHistorico returns date+time in the interface locale (ADR-0032) —
// without a date lib (same choice as `job-card.tsx`, no precedent for a lib in the
// project). The locale defaults to English, the default interface language.
export function formatarCarimboHistorico(iso: string, locale = 'en'): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) {
    return iso
  }
  return d.toLocaleString(locale, { dateStyle: 'short', timeStyle: 'short' })
}
