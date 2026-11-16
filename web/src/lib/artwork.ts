import type { Asset, ArtworkItem, ArtworkResponse } from '@/lib/api'

// Taxonomy of artwork kinds, centralized: pt-BR label, framing
// (aspect) and fit (contain × cover) per kind. It lives outside the components so the
// gallery, the lightbox and the badges of the top card share the SAME source of
// truth — it extends the `ROTULO_KIND`/aspect that used to be local in the gallery (3 kinds)
// to the 9 kinds of fanart.tv, without duplicating the table in every consumer.

// pt-BR label of each kind — used in the gallery rows, in the upload selector, in the
// lightbox and in the availability badges. A kind outside the table falls back to its own name.
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

// rotuloKind resolves the pt-BR label of the kind (raw name as fallback — it never disappears).
export function rotuloKind(kind: string): string {
  return ROTULO_KIND[kind] ?? kind
}

// ORDEM_KINDS fixes the order of the gallery ROWS and of the BADGES: poster and the
// "brand" artwork (logo/clearart) first, then the landscapes (fanart/thumb/banner),
// disc, character and still. A kind outside the list goes to the end (stable order).
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

// Framing of a kind: the `aspect-*` class of the frame and whether the image must be
// CONTAINED (transparencies — logo/clearart/character/disc — cannot be cropped)
// or COVER the frame (poster/fanart/thumb/still/banner are "full" rectangles).
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

// infoKind returns the framing of the kind (fallback 16:9 cover — the safest
// for an unknown kind, the same decision as before).
export function infoKind(kind: string): InfoKind {
  return INFO_KIND[kind] ?? { aspect: 'aspect-video', contain: false }
}

// Card width in the gallery per kind: "bigger cards" asked for by the owner —
// portrait/square ~208px, landscape ~320px, logo ~288px, banner (strip) ~384px. The
// HEIGHT comes from the aspect (infoKind); here only the width of the carousel column.
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

// larguraKind returns the width class of the card (fallback w-52 ~208px).
export function larguraKind(kind: string): string {
  return LARGURA_KIND[kind] ?? 'w-52'
}

// LinhaArte is ONE row (carousel) of the gallery: the kind and its artwork, already in the order that
// the backend served (preferred/language first).
export interface LinhaArte {
  kind: string
  items: ArtworkItem[]
}

// Kinds that are artwork arrays in the envelope (everything except `available`).
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

// linhasDeArtwork extracts the PRESENT rows (kind with at least one artwork) in the canonical
// order (`ORDEM_KINDS`). Empty/absent kinds disappear — the gallery only renders what exists.
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

// SeloArte is an availability badge of the top card: kind, label and count.
export interface SeloArte {
  kind: string
  label: string
  count: number
}

// selosDisponibilidade reads the `available: {kind: count}` of the endpoint and returns the
// badges of the kinds that EXIST (count > 0), in the canonical order. Empty when there is no
// inventory — the row does not appear.
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

// HeroAssets are the PREFERRED logo/banner/disc: the immersive hero only
// EXHIBITS the choice already made by the owner in the gallery — without
// any new auto-selection here. `undefined` per kind when there is no preferred one.
export interface HeroAssets {
  logo: Asset | undefined
  banner: Asset | undefined
  disc: Asset | undefined
}

// assetVencedor replicates in TS the SERVICE PRECEDENCE defined for assets
// (`escolherAssetServido`/`coletarCandidatos` in internal/server/assets.go):
// preferred (preferred:true) -> most recent origin='user' (last of the array,
// which comes sorted by created_at asc) -> origin='external' (first) ->
// anyone (base, e.g. local artwork from the scanner). Without this precedence, artwork
// imported from the real folder (origin='external', preferred:false — the
// normal case of the local media flow) never "won" and the hero stayed
// without a logo/banner/disc even with the asset present on disk.
function assetVencedor(assets: Asset[], kind: string): Asset | undefined {
  const doKind = assets.filter((a) => a.kind === kind)
  if (doKind.length === 0) return undefined
  return (
    doKind.find((a) => a.preferred) ??
    doKind.findLast((a) => a.origin === 'user') ??
    doKind.find((a) => a.origin === 'external') ??
    doKind[0]
  )
}

// assetsPreferidosDoHero extracts, from ONE call to `GET .../assets` (the SAME
// list consumed by the gallery via `listAssets`/`assetsQueryOptions`), the WINNING
// artwork (assetVencedor) of each of the three hero kinds
// (logo/banner/disc). Pure — no I/O.
export function assetsPreferidosDoHero(
  assets: Asset[] | undefined,
): HeroAssets {
  const lista = assets ?? []
  return {
    logo: assetVencedor(lista, 'logo'),
    banner: assetVencedor(lista, 'banner'),
    disc: assetVencedor(lista, 'disc'),
  }
}
