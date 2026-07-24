import type { Asset, ArtworkItem, ArtworkResponse } from '@/lib/api'

// Taxonomia de kinds de arte (0048/0049), centralizada: rótulo pt-BR, enquadramento
// (aspect) e ajuste (contain × cover) por kind. Fica fora dos componentes para a
// galeria, o lightbox e os selos do card do topo compartilharem a MESMA fonte de
// verdade — estende o `ROTULO_KIND`/aspect que o 0044 tinha local na galeria (3 kinds)
// para os 9 kinds da fanart.tv, sem duplicar a tabela em cada consumidor.

// Rótulo pt-BR de cada kind — usado nas linhas da galeria, no seletor de upload, no
// lightbox e nos selos de disponibilidade. Kind fora da tabela cai no próprio nome.
export const ROTULO_KIND: Record<string, string> = {
  poster: 'Pôster',
  fanart: 'Fanart',
  still: 'Still',
  logo: 'Logo',
  clearart: 'Clearart',
  disc: 'Disco',
  banner: 'Banner',
  thumb: 'Thumb',
  characterart: 'Character',
}

// rotuloKind resolve o rótulo pt-BR do kind (nome cru como fallback — nunca some).
export function rotuloKind(kind: string): string {
  return ROTULO_KIND[kind] ?? kind
}

// ORDEM_KINDS fixa a ordem das LINHAS da galeria e dos SELOS: pôster e as artes
// "de marca" (logo/clearart) primeiro, depois as paisagens (fanart/thumb/banner),
// disco, character e still. Kind fora da lista vai para o fim (ordem estável).
export const ORDEM_KINDS = [
  'poster',
  'logo',
  'clearart',
  'fanart',
  'thumb',
  'banner',
  'disc',
  'characterart',
  'still',
] as const

// Enquadramento de um kind: a classe `aspect-*` do frame e se a imagem deve ser
// CONTIDA (transparências — logo/clearart/character/disc — não podem ser cropadas)
// ou COBRIR o frame (pôster/fanart/thumb/still/banner são retangulares "cheios").
export interface InfoKind {
  aspect: string
  contain: boolean
}

const INFO_KIND: Record<string, InfoKind> = {
  poster: { aspect: 'aspect-[2/3]', contain: false },
  fanart: { aspect: 'aspect-video', contain: false },
  thumb: { aspect: 'aspect-video', contain: false },
  still: { aspect: 'aspect-video', contain: false },
  clearart: { aspect: 'aspect-video', contain: true },
  logo: { aspect: 'aspect-[16/6]', contain: true },
  banner: { aspect: 'aspect-[1000/185]', contain: false },
  disc: { aspect: 'aspect-square', contain: true },
  characterart: { aspect: 'aspect-square', contain: true },
}

// infoKind devolve o enquadramento do kind (fallback 16:9 cover — o mais seguro
// para um kind desconhecido, mesma decisão do 0044).
export function infoKind(kind: string): InfoKind {
  return INFO_KIND[kind] ?? { aspect: 'aspect-video', contain: false }
}

// Largura do card na galeria por kind (0049): "cards maiores" pedidos pelo dono —
// retrato/quadrado ~208px, paisagem ~320px, logo ~288px, banner (faixa) ~384px. A
// ALTURA sai do aspect (infoKind); aqui só a largura da coluna do carrossel.
const LARGURA_KIND: Record<string, string> = {
  poster: 'w-52',
  disc: 'w-52',
  characterart: 'w-52',
  fanart: 'w-80',
  thumb: 'w-80',
  still: 'w-80',
  clearart: 'w-80',
  logo: 'w-72',
  banner: 'w-96',
}

// larguraKind devolve a classe de largura do card (fallback w-52 ~208px).
export function larguraKind(kind: string): string {
  return LARGURA_KIND[kind] ?? 'w-52'
}

// LinhaArte é UMA linha (carrossel) da galeria: o kind e suas artes, já na ordem que
// o backend serviu (preferida/idioma primeiro).
export interface LinhaArte {
  kind: string
  items: ArtworkItem[]
}

// Kinds que são arrays de arte no envelope (tudo menos `available`).
type KindResposta =
  | 'poster'
  | 'fanart'
  | 'logo'
  | 'clearart'
  | 'disc'
  | 'banner'
  | 'thumb'
  | 'characterart'
  | 'still'

// linhasDeArtwork extrai as linhas PRESENTES (kind com ao menos uma arte) na ordem
// canônica (`ORDEM_KINDS`). Kinds vazios/ausentes somem — a galeria só rende o que há.
export function linhasDeArtwork(art: ArtworkResponse | undefined): LinhaArte[] {
  if (!art) {
    return []
  }
  const linhas: LinhaArte[] = []
  for (const kind of ORDEM_KINDS) {
    const items = art[kind as KindResposta]
    if (items && items.length > 0) {
      linhas.push({ kind, items })
    }
  }
  return linhas
}

// SeloArte é um selo de disponibilidade do card do topo: kind, rótulo e contagem.
export interface SeloArte {
  kind: string
  label: string
  count: number
}

// selosDisponibilidade lê o `available: {kind: count}` do endpoint e devolve os
// selos dos kinds que EXISTEM (count > 0), na ordem canônica. Vazio quando não há
// inventário — a faixa não aparece.
export function selosDisponibilidade(
  available: Record<string, number> | undefined,
): SeloArte[] {
  if (!available) {
    return []
  }
  const selos: SeloArte[] = []
  for (const kind of ORDEM_KINDS) {
    const count = available[kind]
    if (count && count > 0) {
      selos.push({ kind, label: rotuloKind(kind), count })
    }
  }
  return selos
}

// HeroAssets são a logo/banner/disco PREFERIDOS (0082): o hero imersivo só
// EXIBE a escolha já feita pelo dono na galeria (0048/0049) — sem
// auto-seleção nova aqui. `undefined` por kind quando não há preferida.
export interface HeroAssets {
  logo: Asset | undefined
  banner: Asset | undefined
  disc: Asset | undefined
}

// assetsPreferidosDoHero extrai, de UMA chamada a `GET .../assets` (a MESMA
// lista consumida pela galeria via `listAssets`/`assetsQueryOptions`), a
// primeira arte `preferred:true` de cada um dos três kinds do hero
// (logo/banner/disc). Puro — sem I/O.
export function assetsPreferidosDoHero(
  assets: Asset[] | undefined,
): HeroAssets {
  const porKind = (kind: string): Asset | undefined =>
    assets?.find((a) => a.kind === kind && a.preferred)
  return {
    logo: porKind('logo'),
    banner: porKind('banner'),
    disc: porKind('disc'),
  }
}
