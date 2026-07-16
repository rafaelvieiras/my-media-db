// Helpers PUROS de completude de CATALOGAÇÃO (0058): campos/artes preenchidos
// conforme a régua configurada em /settings (0057). Conceito NOVO, DIFERENTE de
// completude de POSSE (`web/src/lib/entity-badges.ts`, 0036/0037) — este módulo
// NÃO reusa nada de lá (nem `estadoCompletude`/`temCompletude`/`unidadeCompletude`),
// decisão herdada do 0057 §6 e repetida no 0058 §6, para manter os dois conceitos
// claramente separados no código, não só na UI.
import type { components } from '@/lib/api-types'

export type CatalogCompleteness =
  components['schemas']['CatalogCompletenessResult'] | null

// Mesmo mapa de rótulos de kind usado no formulário do 0057
// (`web/src/components/catalog-completeness-form.tsx`, `KINDS_ARTE_UI`).
const ROTULO_KIND_ARTE: Record<string, string> = {
  poster: 'Poster',
  fanart: 'Fanart',
  logo: 'Logo',
  clearart: 'Clearart',
  banner: 'Banner',
  thumb: 'Thumb',
  disc: 'Capa de disco',
}

// Mapa PT-BR das chaves ESTÁVEIS de `missing` (0058 §4). Chave desconhecida ecoa
// crua (nunca quebra a UI, mesmo padrão de `rotuloTipo` em entity-badges.ts).
const ROTULO_CAMPO: Record<string, string> = {
  description: 'sinopse',
  runtime: 'duração',
  certification: 'classificação indicativa',
  keywords: 'palavras-chave',
  cast: 'elenco',
  media_file: 'arquivo de mídia',
}

// precisaAtencaoCatalogacao diz se o selo/seção deve aparecer: só quando há um
// resultado avaliado (`cc` não-nulo) e ele é incompleto. `null` (draft, ou ainda
// não avaliado pelo job) e `complete: true` nunca disparam o alerta — capa/detalhe
// limpos, mesmo princípio do selo de posse (0038).
export function precisaAtencaoCatalogacao(cc: CatalogCompleteness): boolean {
  return !!cc && !cc.complete
}

// rotuloItemFaltando traduz UMA chave de `missing` para PT-BR. Chaves `artwork:
// <kind>` extraem o kind e reusam o mapa de rótulos de arte do formulário 0057
// (`Poster`/`Fanart`/…), em minúsculas para casar com o estilo dos demais rótulos
// (todos minúsculos, ex. "sinopse"/"elenco") e com o exemplo do briefing (§2.3):
// "Catalogação incompleta: elenco, arte fanart". Kind desconhecido ecoa cru (ex.
// "artwork:still" → "arte still"). Chave de campo desconhecida ecoa crua.
export function rotuloItemFaltando(chave: string): string {
  if (chave.startsWith('artwork:')) {
    const kind = chave.slice('artwork:'.length)
    const rotulo = ROTULO_KIND_ARTE[kind] ?? kind
    return `arte ${rotulo.toLowerCase()}`
  }
  return ROTULO_CAMPO[chave] ?? chave
}

// rotuloTooltipCompletude monta a frase completa do tooltip/aria-label do selo:
// "Catalogação incompleta: elenco, arte fanart". `cc` nulo/completo devolve a
// frase com lista vazia (a UI não deve chamar isso quando `!precisaAtencaoCatalogacao`,
// mas a função não quebra se chamada de qualquer forma).
export function rotuloTooltipCompletude(cc: CatalogCompleteness): string {
  const faltando = cc?.missing ?? []
  return `Catalogação incompleta: ${faltando.map(rotuloItemFaltando).join(', ')}`
}
