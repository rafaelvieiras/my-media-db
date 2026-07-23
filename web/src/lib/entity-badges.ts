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

// Estado de COMPLETUDE de uma temporada/série a partir da posse agregada (0036):
// `completa` (tem todos os episódios catalogados), `parcial` (tem alguns), `vazia`
// (não tem nenhum na biblioteca, mas há catalogados) ou `null` (sem selo — total
// desconhecido/zero). É a regra ÚNICA que a UI lê para escolher o selo e o realce.
export type EstadoCompletude = 'completa' | 'parcial' | 'vazia' | null

// estadoCompletude classifica a posse agregada do card. total==0 → null (nada a
// dizer). owned>=total (total>0) → completa. 0<owned<total → parcial. owned==0 (com
// total>0) → vazia. Ausência de `ownership` (movie/episode/collection) → null.
export function estadoCompletude(
  ownership: { owned_count: number; total_count: number } | null | undefined,
): EstadoCompletude {
  if (!ownership || ownership.total_count <= 0) {
    return null
  }
  const { owned_count, total_count } = ownership
  if (owned_count >= total_count) {
    return 'completa'
  }
  if (owned_count > 0) {
    return 'parcial'
  }
  return 'vazia'
}

// temCompletude diz se o tipo do card tem selo de completude (posse rasa): só a série
// (conta temporadas) e a temporada (conta episódios). movie/episode/collection nunca —
// o gate único que a UI usa para NÃO exibir selo/recuo nesses tipos, mesmo que um
// ownership vaze no DTO.
export function temCompletude(type: string): boolean {
  return type === 'tv' || type === 'season'
}

// Unidade contada pelo selo de completude conforme o tipo do card (0037): o card
// `tv` conta TEMPORADAS (posse rasa), o card `season` conta EPISÓDIOS (0036). É o
// que muda o rótulo do selo entre a home (série) e a lista de membros (temporada).
// Qualquer outro tipo não tem selo de completude — cai no plural neutro "itens".
export function unidadeCompletude(type: string): string {
  if (type === 'tv') {
    return 'temporadas'
  }
  if (type === 'season') {
    return 'episódios'
  }
  return 'itens'
}

// Rótulo pt-BR do status de catalogação do item. O backend só usa dois valores
// (`draft`/`cataloged` — ver internal/scanner/match.go,
// internal/server/completeness.go); `draft` já tem tratamento especial
// (`ehDraft` → "não identificado"), esta função cobre o restante. Valor
// desconhecido ecoa cru (nunca quebra a UI).
export function rotuloStatus(status: string): string {
  const mapa: Record<string, string> = {
    cataloged: 'catalogado',
  }
  return mapa[status] ?? status
}

// Rótulo pt-BR do tipo de mídia para exibição. Cobre os tipos canônicos: os de
// topo (filme/série/coleção) e os nós da árvore de séries (temporada/episódio,
// 0031). Tipo desconhecido ecoa cru (nunca quebra a UI).
export function rotuloTipo(type: string): string {
  const mapa: Record<string, string> = {
    movie: 'Filme',
    tv: 'Série',
    collection: 'Coleção',
    season: 'Temporada',
    episode: 'Episódio',
  }
  return mapa[type] ?? type
}
