// Helpers PUROS de estado de card do catálogo (posse, revisão, draft).
//
// Isolados da renderização para serem testáveis e manter a semântica dos badges
// num só lugar. O badge de REVISÃO apenas SINALIZA baixa confiança (ADR-0008);
// confirmar/corrigir o match é fluxo da Fase 3.
import type { components } from '@/lib/api-types'

export type EntityCard = components['schemas']['EntityCard']

// Limiar de confiança (escala 0–100 do scanner; ver internal/scanner/match.go).
// Título+ano casa com 70 (palpite alto, não sinaliza); título sem ano casa com
// 40 (baixo → sinaliza revisão). Abaixo do limiar, exibimos o alerta.
export const LIMIAR_BAIXA_CONFIANCA = 70

// Item não-identificado (sem match): aparece como `draft` no detalhe e no card.
export function ehDraft(status: string): boolean {
  return status === 'draft'
}

// Precisa de revisão quando há confiança conhecida, positiva e abaixo do limiar,
// e o item NÃO é um draft (esse tem rótulo próprio). Confiança `null` ou 0 não
// dispara o alerta de revisão (é ausência de match, não match duvidoso).
export function precisaRevisao(card: {
  status: string
  match_confidence: number | null
}): boolean {
  if (ehDraft(card.status)) {
    return false
  }
  const c = card.match_confidence
  return c !== null && c > 0 && c < LIMIAR_BAIXA_CONFIANCA
}

// Rótulo pt-BR do tipo de mídia para exibição.
export function rotuloTipo(type: string): string {
  const mapa: Record<string, string> = {
    movie: 'Filme',
    tv: 'Série',
  }
  return mapa[type] ?? type
}
