import { useRef, useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import {
  Clock,
  Download,
  ExternalLink,
  ImageOff,
  Library,
  LoaderCircle,
  Music,
  Pause,
  Play,
  X,
} from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { EstadoAssistidoBadge } from '@/components/historico-section'
import {
  ApiError,
  assetFileSrc,
  downloadEntityTheme,
  downloadEntityTrailer,
  importExternal,
  themeSrc,
  trailerSrc,
  type Asset,
  type CreditCast,
  type CreditCompany,
  type CreditCrew,
  type ExternalCollection,
  type ExternalSimilar,
  type ExternalVideo,
  type ImportRequest,
} from '@/lib/api'
import { assetsPreferidosDoHero, selosDisponibilidade } from '@/lib/artwork'
import { rotuloTipo } from '@/lib/entity-badges'
import { internalIdDe } from '@/lib/internal-id'
import { rotuloIdioma } from '@/lib/languages'
import { metaDaFonte } from '@/lib/provider-meta'
import {
  assetsQueryOptions,
  entityThemeCandidatoQueryOptions,
  entityThemeQueryOptions,
  entityTrailerQueryOptions,
  ytdlpSettingsQueryOptions,
} from '@/lib/queries'
import {
  rotuloSituacao,
  type RichDetail,
  type RotuloSituacao,
} from '@/lib/rich-detail'
import { cn } from '@/lib/utils'
import { useLocale, useT } from '@/lib/i18n'
import type { Translate } from '@/lib/i18n/types'

// RichDetailView is the SHARED RICH screen between the detail of the EXTERNAL item (before
// importing) and the detail of the imported ENTITY (catalog). The two screens
// have the SAME visual and information pattern — full-bleed IMMERSIVE hero (backdrop +
// overlay + poster + synopsis + actions + ratings row),
// a row of external links right below, genres, keywords, videos, collection,
// similars — and differ only in the SLOTS: the external one brings the Import button; the entity brings
// the "Gerenciar" dropdown, the override badges, the editor, the gallery/files and (via
// `entityId`) the preferred logo/banner/disc and the player of the local theme song. The
// backend serves the SAME rich shape in both DTOs (internal/richfields), so a
// single adapter normalizes each one into `RichDetail` and the component is agnostic to the
// origin.

// formatarDuracao converts minutes into `2h 35min` (`45min` when < 1h).
function formatarDuracao(min: number): string {
  const horas = Math.floor(min / 60)
  const resto = min % 60
  if (horas === 0) {
    return `${resto}min`
  }
  return resto === 0 ? `${horas}h` : `${horas}h ${resto}min`
}

// ── Reusable poster ──────────────────────────────────────────────────────────

// PosterImg is the poster with a placeholder, shared by the hero, the collection,
// the similars and the search card. `src` is an already built URL (direct or proxied);
// an error/absence falls back to the icon. Centralizing it kills the markup duplication (jscpd).
export function PosterImg({
  src,
  alt,
  className,
}: {
  src: string | null
  alt: string
  className?: string
}) {
  const [falhou, setFalhou] = useState(false)
  const mostrarPlaceholder = !src || falhou
  return (
    <div
      className={cn(
        'relative aspect-[2/3] overflow-hidden rounded-xl bg-muted',
        className,
      )}
    >
      {mostrarPlaceholder ? (
        <div
          data-testid="poster-placeholder"
          className="flex h-full w-full items-center justify-center text-muted-foreground"
        >
          <ImageOff className="size-6" aria-hidden />
        </div>
      ) : (
        <img
          src={src ?? undefined}
          alt={alt}
          loading="lazy"
          onError={() => setFalhou(true)}
          className="h-full w-full object-cover"
        />
      )}
    </div>
  )
}

// StillFrame renders the 16:9 STILL of the EPISODE at the top of the detail: the image already
// comes served by the image proxy (fetch+cache on demand) and occupies its own `aspect-
// video` frame — not the 2:3 poster —, because the still is landscape. `loading="lazy"` +
// `onError` fall back to the placeholder (like PosterImg). Without a still (src null) it shows the
// placeholder in the same frame. Only the episode detail renders it.
export function StillFrame({ src, alt }: { src: string | null; alt: string }) {
  const [falhou, setFalhou] = useState(false)
  const mostrarPlaceholder = !src || falhou
  return (
    <div className="relative aspect-video w-full overflow-hidden bg-muted">
      {mostrarPlaceholder ? (
        <div
          data-testid="still-placeholder"
          className="flex h-full w-full items-center justify-center text-muted-foreground"
        >
          <ImageOff className="size-8" aria-hidden />
        </div>
      ) : (
        <img
          src={src ?? undefined}
          alt={alt}
          loading="lazy"
          onError={() => setFalhou(true)}
          className="h-full w-full object-cover"
        />
      )}
    </div>
  )
}

// CapaQuadrada renders the SQUARE cover (1:1) of the ALBUM/ARTIST hero
// in place of the 2:3 poster — the same pattern as the `StillFrame` above
// for the episode (it swaps only the image block, the rest of the hero stays generic).
// `circular` applies `rounded-full` (used only for the artist — an optional visual
// treatment of "photo", not required); the album keeps the normal
// rounded corner (`rounded-xl`, like the poster).
function CapaQuadrada({
  src,
  alt,
  circular = false,
}: {
  src: string | null
  alt: string
  circular?: boolean
}) {
  const [falhou, setFalhou] = useState(false)
  const mostrarPlaceholder = !src || falhou
  return (
    <div
      className={cn(
        'relative aspect-square w-40 shrink-0 overflow-hidden bg-muted sm:w-56',
        circular ? 'rounded-full' : 'rounded-xl',
      )}
    >
      {mostrarPlaceholder ? (
        <div
          data-testid="capa-placeholder"
          className="flex h-full w-full items-center justify-center text-muted-foreground"
        >
          <ImageOff className="size-8" aria-hidden />
        </div>
      ) : (
        <img
          src={src ?? undefined}
          alt={alt}
          loading="lazy"
          onError={() => setFalhou(true)}
          className="h-full w-full object-cover"
        />
      )}
    </div>
  )
}

// HeroBackdropFundo is the full-bleed BACKGROUND of the immersive hero for the types
// WITH a poster (everything except the episode — which uses the StillFrame above instead): a
// box with a height around 40% of the viewport (`min-h`, no longer the fixed `aspect-[16/6]`
// of before), filled by the image (`object-cover`) or, without a backdrop/in error,
// by the placeholder (`ImageOff`, parity with PosterImg/StillFrame). The readability
// gradient is applied by the CALLER (a single layer on top of any
// background — image, placeholder or none) so as not to duplicate the overlay per branch.
function HeroBackdropFundo({ src }: { src: string | null }) {
  const [falhou, setFalhou] = useState(false)
  const mostrarPlaceholder = !src || falhou
  return (
    <div className="relative h-full min-h-[32vh] w-full bg-muted sm:min-h-[40vh]">
      {mostrarPlaceholder ? (
        <div
          data-testid="backdrop-placeholder"
          className="absolute inset-0 flex items-center justify-center text-muted-foreground"
        >
          <ImageOff className="size-10" aria-hidden />
        </div>
      ) : (
        <img
          src={src ?? undefined}
          alt=""
          aria-hidden
          loading="lazy"
          onError={() => setFalhou(true)}
          className="absolute inset-0 h-full w-full object-cover"
        />
      )}
    </div>
  )
}

// ── Shared Importar button ───────────────────────────────────────────────────

// ImportarButton imports an external item (find-or-create) and takes you to the detail
// of the catalog. Shared by the search card and by the external screen. Owner of
// its own mutation; it invalidates `['entities']` and navigates on success; error inline.
export function ImportarButton({
  source,
  sourceId,
  type,
  parentId,
  size = 'default',
  label,
}: {
  source: string
  sourceId: string
  type: string
  // parentId: INTERNAL id of the parent album, required only
  // for type="faixa" (a loose track missing from an album already internal) — the
  // other types ignore it.
  parentId?: string
  size?: 'sm' | 'default' | 'lg'
  // Overrides the default "Import" label (e.g. "Import collection") — the copy stays
  // with the screen that owns the noun.
  label?: string
}) {
  const t = useT()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const importMutation = useMutation({
    mutationFn: () =>
      importExternal({
        source,
        source_id: sourceId,
        type: type as ImportRequest['type'],
        ...(parentId ? { parent_id: parentId } : {}),
      }),
    onSuccess: (entity) => {
      queryClient.invalidateQueries({ queryKey: ['entities'] })
      navigate({ to: '/entities/$id', params: { id: entity.id } })
    },
  })
  const mensagemErro =
    importMutation.error instanceof ApiError
      ? importMutation.error.message
      : null

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Button
        size={size}
        disabled={importMutation.isPending}
        onClick={() => importMutation.mutate()}
      >
        {importMutation.isPending ? (
          <LoaderCircle className="animate-spin" aria-hidden />
        ) : (
          <Download aria-hidden />
        )}
        {importMutation.isPending
          ? t('entity.action.importing')
          : (label ?? t('entity.action.import'))}
      </Button>
      {mensagemErro ? (
        <span role="alert" className="text-sm font-medium text-destructive">
          {mensagemErro}
        </span>
      ) : null}
    </div>
  )
}

// ── Rich sections ────────────────────────────────────────────────────────────

// ehYouTube says whether the video can be EMBEDDED: it has to be from YouTube and have a `key`
// (the `url` is dispensable — the embed is built from the key). Only those become a local
// player; the others fall back to the external link (when they have a url).
function ehYouTube(video: ExternalVideo): boolean {
  return video.site === 'YouTube' && !!video.key
}

// YouTubeEmbedFrame is the <iframe> of the `youtube-nocookie` domain (without tracking
// cookies) shared by the CLICK-TO-PLAY player of the `VideosRail` (VideoCard) and
// by the "Watch trailer" button of the hero (`TrailerModal` below) — a single
// place that builds the embed URL, never duplicated between the two uses.
function YouTubeEmbedFrame({
  videoKey,
  title,
}: {
  videoKey: string
  title: string
}) {
  return (
    <iframe
      src={`https://www.youtube-nocookie.com/embed/${videoKey}?autoplay=1`}
      title={title}
      allow="autoplay; encrypted-media; picture-in-picture"
      allowFullScreen
      className="absolute inset-0 h-full w-full border-0"
    />
  )
}

// VideoCard renders a video. For YouTube, it is a CLICK-TO-PLAY player: it shows the
// thumbnail with a play button and, on click, swaps it for the `YouTubeEmbedFrame`. Non-YouTube
// videos (rare in TMDB) keep the external link. We do not preload the iframe:
// it avoids N players and the YouTube traffic/tracking before the owner wants to watch.
function VideoCard({ video }: { video: ExternalVideo }) {
  const t = useT()
  const [tocando, setTocando] = useState(false)

  if (!ehYouTube(video)) {
    // Non-embeddable: external link. The `VideosRail` only forwards videos with a
    // `url` here (the filter is the single source of truth), so the url is present here.
    return (
      <a
        href={video.url ?? undefined}
        target="_blank"
        rel="noreferrer"
        className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"
      >
        <Play className="size-4 shrink-0" aria-hidden />
        <span className="line-clamp-1">{video.name}</span>
        <Badge variant="outline" className="shrink-0">
          {video.type}
        </Badge>
      </a>
    )
  }

  return (
    <figure className="flex flex-col gap-1.5">
      <div className="relative aspect-video overflow-hidden rounded-xl bg-muted ring-1 ring-foreground/10">
        {tocando ? (
          <YouTubeEmbedFrame videoKey={video.key} title={video.name} />
        ) : (
          <button
            type="button"
            onClick={() => setTocando(true)}
            aria-label={t('entity.action.playVideo', { name: video.name })}
            className="group absolute inset-0 h-full w-full cursor-pointer"
          >
            <img
              src={`https://img.youtube.com/vi/${video.key}/hqdefault.jpg`}
              alt=""
              aria-hidden
              loading="lazy"
              className="h-full w-full object-cover"
            />
            <span className="absolute inset-0 flex items-center justify-center bg-black/30 transition group-hover:bg-black/40">
              <span className="flex size-14 items-center justify-center rounded-full bg-background/90 text-foreground shadow-lg transition group-hover:scale-105">
                <Play className="size-6 fill-current" aria-hidden />
              </span>
            </span>
          </button>
        )}
      </div>
      <figcaption className="flex items-center gap-2 text-sm">
        <span className="line-clamp-1 font-medium">{video.name}</span>
        <Badge variant="outline" className="shrink-0">
          {video.type}
        </Badge>
      </figcaption>
    </figure>
  )
}

function VideosRail({ videos }: { videos: ExternalVideo[] }) {
  const t = useT()
  // Renderable = embeddable (YouTube+key) OR with an external link. The others disappear.
  const renderaveis = videos.filter((v) => ehYouTube(v) || v.url)
  if (renderaveis.length === 0) {
    return null
  }
  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-medium">
        {t('entity.section.videos')}
      </h2>
      <ul className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {renderaveis.map((video) => (
          <li key={`${video.site}:${video.key}`}>
            <VideoCard video={video} />
          </li>
        ))}
      </ul>
    </section>
  )
}

// primeiroTrailer finds the first PLAYABLE video of the type
// "Trailer" (TMDB convention) — only a real trailer, without falling back to Teaser/
// Clip (§4.3). `null` when there is none.
function primeiroTrailer(videos: ExternalVideo[]): ExternalVideo | null {
  return videos.find((v) => v.type === 'Trailer' && ehYouTube(v)) ?? null
}

// TrailerModal is the full-screen overlay that opens on clicking "Watch trailer"
// in the action bar of the hero: it reuses the SAME `YouTubeEmbedFrame` of the
// `VideosRail`/`VideoCard` (without duplicating the embed logic), already in playing mode (the
// click on the button already expresses the intention — without the extra step of clicking the
// thumbnail). It closes on clicking outside or on the X.
function TrailerModal({
  video,
  onClose,
}: {
  video: ExternalVideo
  onClose: () => void
}) {
  const t = useT()
  return (
    <div
      role="dialog"
      aria-label={t('entity.hero.trailerOf', { name: video.name })}
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-4"
      onClick={onClose}
    >
      <div
        className="relative aspect-video w-full max-w-3xl overflow-hidden rounded-xl bg-black shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <YouTubeEmbedFrame videoKey={video.key} title={video.name} />
      </div>
      <button
        type="button"
        onClick={onClose}
        aria-label={t('entity.action.closeTrailer')}
        className="absolute right-4 top-4 flex size-10 items-center justify-center rounded-full bg-background/90 text-foreground shadow-lg"
      >
        <X className="size-5" aria-hidden />
      </button>
    </div>
  )
}

// TrailerButton is the "Watch trailer" button of the action bar (§4.3): it disappears
// when there is no playable trailer (`primeiroTrailer` null) — without falling back to
// Teaser/Clip.
function TrailerButton({ videos }: { videos: ExternalVideo[] }) {
  const t = useT()
  const [aberto, setAberto] = useState(false)
  const trailer = primeiroTrailer(videos)
  if (!trailer) {
    return null
  }
  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setAberto(true)}>
        <Play aria-hidden />
        {t('entity.action.watchTrailer')}
      </Button>
      {aberto ? (
        <TrailerModal video={trailer} onClose={() => setAberto(false)} />
      ) : null}
    </>
  )
}

// TrailerDownloadButton is the "Download trailer" button (ADR-0025):
// it materializes on disk the SAME trailer that "Watch trailer" already plays embedded
// — purely additive, the embedded player stays the same with or without a local
// download. It only appears with ALL the conditions: (a) `entityId` present (the
// materialization needs a REAL folder in the library, which only exists for
// an already cataloged item — the external screen never shows this button), (b) there is
// a playable trailer (`primeiroTrailer`) and (c) yt-dlp is available
// (`GET /settings/ytdlp`, app cache via TanStack Query). Any condition
// missing: no button, no error (§2.1/§2.5).
function TrailerDownloadButton({
  entityId,
  videos,
}: {
  entityId: string
  videos: ExternalVideo[]
}) {
  const t = useT()
  const trailer = primeiroTrailer(videos)
  const queryClient = useQueryClient()
  // `enabled: !!trailer` avoids the TWO network calls when the item does not even have a
  // playable trailer (the common case) — without a trailer, it is not even worth knowing
  // whether yt-dlp is available.
  const { data: ytdlp } = useQuery({
    ...ytdlpSettingsQueryOptions,
    enabled: !!trailer,
  })
  const { data: jaBaixado } = useQuery({
    ...entityTrailerQueryOptions(entityId),
    enabled: !!trailer,
  })
  const downloadMutation = useMutation({
    mutationFn: () => downloadEntityTrailer(entityId),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ['entity', entityId, 'trailer'],
      })
    },
  })
  const mensagemErro =
    downloadMutation.error instanceof ApiError
      ? downloadMutation.error.message
      : null

  if (!trailer || !ytdlp?.disponivel) {
    return null
  }

  // PERSISTENT state (§2.3): already downloaded (computed on every load via
  // HEAD) or just downloaded in this session — it becomes a link to the file instead
  // of an action button, without offering an accidental re-download without `force`.
  if (jaBaixado || downloadMutation.isSuccess) {
    return (
      <Button variant="outline" size="sm" asChild>
        <a href={trailerSrc(entityId)} target="_blank" rel="noreferrer">
          <Download aria-hidden />
          {t('entity.action.trailerDownloaded')}
        </a>
      </Button>
    )
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Button
        variant="outline"
        size="sm"
        disabled={downloadMutation.isPending}
        onClick={() => downloadMutation.mutate()}
      >
        {downloadMutation.isPending ? (
          <LoaderCircle className="animate-spin" aria-hidden />
        ) : (
          <Download aria-hidden />
        )}
        {downloadMutation.isPending
          ? t('entity.action.downloadingTrailer')
          : t('entity.action.downloadTrailer')}
      </Button>
      {mensagemErro ? (
        <span role="alert" className="text-sm font-medium text-destructive">
          {mensagemErro}
        </span>
      ) : null}
    </div>
  )
}

// ColecaoBand shows the collection the item belongs to. Internal-first: when the
// collection ALREADY is an entity (`internal_id`), the card links to `/entities/{id}` (the INTERNAL
// destination, centralized); otherwise, to the external page of the collection (`/collection/
// $source/$id`). The whole card is the destination, with the same look in both cases.
function ColecaoBand({
  source,
  colecao,
}: {
  source: string
  colecao: ExternalCollection
}) {
  const t = useT()
  const internalId = internalIdDe(colecao)
  const classeCard =
    'group flex items-center gap-3 rounded-xl bg-card p-3 ring-1 ring-foreground/10 transition hover:ring-foreground/30'
  const conteudo = (
    <>
      <PosterImg
        src={colecao.poster_url}
        alt={t('entity.alt.collectionPoster', { name: colecao.name })}
        className="w-16 shrink-0"
      />
      <span className="font-medium group-hover:underline">{colecao.name}</span>
    </>
  )
  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-medium">
        {t('common.entityType.collection')}
      </h2>
      {internalId != null ? (
        <Link
          to="/entities/$id"
          params={{ id: internalId }}
          className={classeCard}
        >
          {conteudo}
        </Link>
      ) : (
        <Link
          to="/collection/$source/$id"
          params={{ source, id: colecao.source_id }}
          className={classeCard}
        >
          {conteudo}
        </Link>
      )}
    </section>
  )
}

// SimilaresRail: each card navigates to the similar one. Internal-first (extended for the
// music vertical): when the similar ALREADY is an entity (`internal_id`), the card links to `/entities/{id}` (the
// INTERNAL destination, centralized) and gains the "In the library" badge; otherwise, to the external
// detail of the target (`/external/$source/$type/$id`), navigable and not cataloged (the detail
// persists on the first visit). It applies to both screens (external and entity).
function SimilaresRail({
  source,
  similares,
}: {
  source: string
  similares: ExternalSimilar[]
}) {
  const t = useT()
  if (similares.length === 0) {
    return null
  }
  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-medium">
        {t('entity.section.similars')}
      </h2>
      <ul className="grid grid-cols-3 gap-3 sm:grid-cols-4 md:grid-cols-6">
        {similares.map((item) => (
          <li key={`${item.type}:${item.source_id}`}>
            <SimilarCard source={source} item={item} />
          </li>
        ))}
      </ul>
    </section>
  )
}

// SimilarCard renders ONE similar. The content (poster + title + year) is identical in the
// two destinations — only the route changes (internal × external) and the internal item shows the
// "In the library" badge. Extracted so as not to duplicate the markup between the two branches of the Link.
function SimilarCard({
  source,
  item,
}: {
  source: string
  item: ExternalSimilar
}) {
  const t = useT()
  const internalId = internalIdDe(item)
  const conteudo = (
    <>
      <PosterImg
        src={item.poster_url}
        alt={t('entity.alt.poster', { title: item.title })}
        className="transition group-hover:ring-2 group-hover:ring-foreground/30"
      />
      <span className="line-clamp-2 text-xs font-medium group-hover:text-foreground">
        {item.title}
      </span>
      {internalId != null ? (
        <Badge variant="secondary" className="w-fit">
          {t('entity.label.inLibrary')}
        </Badge>
      ) : item.year ? (
        <span className="text-xs text-muted-foreground">{item.year}</span>
      ) : null}
    </>
  )
  if (internalId != null) {
    return (
      <Link
        to="/entities/$id"
        params={{ id: internalId }}
        className="group flex flex-col gap-1"
      >
        {conteudo}
      </Link>
    )
  }
  return (
    <Link
      to="/external/$source/$type/$id"
      params={{ source, type: item.type, id: item.source_id }}
      className="group flex flex-col gap-1"
    >
      {conteudo}
    </Link>
  )
}

// ExternalLinksRow is the HORIZONTAL row of
// chips with the LOGO of each provider — a deliberate deviation from the reference print
// (which shows them in a side sidebar, see §1/§3): this project has no
// metadata sidebar, so the row sits right BELOW the hero (it was the last
// section of the page, after Production companies — it moved to the top). `metaDaFonte`
// resolves logo+label; a source without an entry in the registry falls back to the generic icon
// `ExternalLink` + name in uppercase — it never breaks, never hides the link.
function ExternalLinksRow({ links }: { links: Record<string, string> }) {
  const entradas = Object.entries(links)
  if (entradas.length === 0) {
    return null
  }
  return (
    <div className="flex flex-wrap items-center gap-2">
      {entradas.map(([fonte, url]) => {
        const { label, Logo } = metaDaFonte(fonte)
        return (
          <a
            key={fonte}
            href={url}
            target="_blank"
            rel="noreferrer noopener"
            className="inline-flex items-center gap-1.5 rounded-full border border-foreground/15 px-3 py-1.5 text-sm text-foreground/90 transition-colors hover:bg-foreground/5"
          >
            {Logo ? (
              <Logo className="h-4 w-auto" />
            ) : (
              <ExternalLink className="size-4" aria-hidden />
            )}
            {label}
          </a>
        )
      })}
    </div>
  )
}

// FaixaStatusPoster is the compact SITUATION band anchored at the base of the
// poster, in the visual pattern of TMDB (which shows the status next to the cover, not loose in the
// text block). It only appears when `situacao` exists (enriched series/season);
// movie/collection without `airing_status` stay without the band — the same gating as before, only
// visually moved from the metadata block to the poster. It overlaps via `absolute`
// in a `relative` wrapper around the `PosterImg` (which gains no new prop).
function FaixaStatusPoster({ situacao }: { situacao: RotuloSituacao | null }) {
  if (!situacao) {
    return null
  }
  return (
    <div className="absolute inset-x-0 bottom-0 rounded-b-xl bg-gradient-to-t from-black/85 via-black/55 to-transparent px-2 pt-6 pb-1.5">
      <span className="block truncate text-center text-[11px] font-semibold tracking-wide text-white">
        {situacao.label}
      </span>
    </div>
  )
}

// NotaMedalhao is the circular "medallion" of the hero rating, in the visual pattern of the
// TMDB rating circle — a simple SVG with `stroke-dasharray`, without a new
// dependency. It prioritizes the owner's LOCAL rating (ADR-0012, amber outline); the CALLER decides the
// fallback to the SOURCE rating (neutral outline) when there is no local rating — the same
// behavior as before (star icon + number), only in this component.
function NotaMedalhao({
  value,
  variant,
}: {
  value: number
  variant: 'local' | 'fonte'
}) {
  const t = useT()
  const ehLocal = variant === 'local'
  const raio = 16
  const circunferencia = 2 * Math.PI * raio
  const pctPreenchido = Math.max(0, Math.min(1, value / 10))
  const tracoPreenchido = pctPreenchido * circunferencia
  return (
    <div
      className="relative flex size-11 shrink-0 items-center justify-center"
      aria-label={
        ehLocal
          ? t('entity.rating.yours', { value: value.toFixed(1) })
          : undefined
      }
      title={
        ehLocal ? t('entity.rating.yoursTitle') : t('entity.rating.source')
      }
    >
      <svg viewBox="0 0 40 40" className="size-11 -rotate-90" aria-hidden>
        <circle
          cx="20"
          cy="20"
          r={raio}
          strokeWidth="4"
          fill="none"
          className="stroke-muted"
        />
        <circle
          cx="20"
          cy="20"
          r={raio}
          strokeWidth="4"
          strokeLinecap="round"
          fill="none"
          strokeDasharray={`${tracoPreenchido} ${circunferencia}`}
          className={ehLocal ? 'stroke-amber-500' : 'stroke-foreground'}
        />
      </svg>
      <span className="absolute text-xs font-semibold text-foreground">
        {value.toFixed(1)}
      </span>
    </div>
  )
}

// NotaFonteBadge is the compact badge of ONE SOURCE rating — next to the
// local `NotaMedalhao`, one per entry of `detail.ratings`. It appears
// ALWAYS when there is at least one source (decision of the owner: even with a single source,
// the common case today — only TMDB —, the row is not hidden waiting for 2+).
// `metaDaFonte` resolves logo+label; a source without a registry entry falls back to the label in
// uppercase (same fallback as the row of links).
function NotaFonteBadge({ source, value }: { source: string; value: number }) {
  const { label, Logo } = metaDaFonte(source)
  return (
    <span
      className="inline-flex items-center gap-1.5 rounded-full border border-foreground/15 px-2.5 py-1 text-sm text-foreground/90"
      title={label}
    >
      {Logo ? (
        <Logo className="h-4 w-auto" />
      ) : (
        <span className="text-xs font-medium text-muted-foreground">
          {label}
        </span>
      )}
      {value.toFixed(1)}
    </span>
  )
}

// rotuloArte translates the label of an artwork kind (the availability badges of the top
// card). A kind outside the catalog echoes raw (never breaks the UI, same idiom as
// `rotuloTipo` in entity-badges.ts).
function rotuloArte(kind: string, t: Translate): string {
  const chave = `entity.artwork.${kind}`
  const rotulo = t(chave)
  return rotulo === chave ? kind : rotulo
}

// SelosArtes is the availability ROW of the top card: it reads the
// `available: {kind: count}` of the `/artwork` endpoint and shows one badge per kind
// that EXISTS, with a translated label + count (e.g.: "Poster 23 · Fanart 40 · Logo 17").
// Empty/absent (movie without inventory, external screen) → null: the row does not appear.
function SelosArtes({
  available,
}: {
  available: Record<string, number> | undefined
}) {
  const t = useT()
  const selos = selosDisponibilidade(available)
  if (selos.length === 0) {
    return null
  }
  return (
    <div className="flex flex-wrap items-center gap-1.5 pt-1">
      {selos.map((selo) => (
        <Badge key={selo.kind} variant="outline">
          {rotuloArte(selo.kind, t)} {selo.count}
        </Badge>
      ))}
    </div>
  )
}

// ── Logo, banner, disc and theme song ────────────────────────────────────────
//
// The three artworks and the theme below only appear on the ENTITY screen (via `entityId`,
// passed only by `/entities/$id`) — the external one (before importing) has no assets
// of its own nor a local folder yet (§4.2/§4.4 of the spec).

// LogoHero replaces the TITLE IN TEXT with the preferred logo (`kind=logo`),
// overlaid on the backdrop in the Jellyfin style. Without a logo, the CALLER falls back to the usual
// `<h1>` — the same behavior as today, without regression.
function LogoHero({ logo, alt }: { logo: Asset; alt: string }) {
  return (
    <img
      src={assetFileSrc(logo.url)}
      alt={alt}
      loading="lazy"
      className="max-h-16 w-auto max-w-full object-contain drop-shadow-lg sm:max-h-24"
    />
  )
}

// BannerFaixa shows the preferred banner (`kind=banner`) as a wide, low
// band — only when it exists; without gating by type beyond "exists or not".
function BannerFaixa({ banner }: { banner: Asset }) {
  return (
    <img
      src={assetFileSrc(banner.url)}
      alt=""
      aria-hidden
      loading="lazy"
      className="aspect-[1000/185] w-56 shrink-0 overflow-hidden rounded-md object-cover shadow-lg sm:w-72"
    />
  )
}

// DiscoGiratorio is the decorative disc (`kind=disc`) near the poster, with a slow
// continuous rotation in pure CSS (`animate-spin`, arbitrary for one turn every 9s —
// much slower than the default spinner) — `motion-reduce:animate-none` turns the
// animation off for whoever asked for `prefers-reduced-motion: reduce` (native Tailwind
// variant, without extra JS). It only appears with a preferred asset of the kind.
function DiscoGiratorio({ disc }: { disc: Asset }) {
  const t = useT()
  return (
    <div
      title={t('entity.artwork.disc')}
      className="absolute -bottom-3 -right-3 size-14 overflow-hidden rounded-full shadow-lg ring-2 ring-background sm:size-16"
    >
      <img
        src={assetFileSrc(disc.url)}
        alt=""
        aria-hidden
        loading="lazy"
        className="h-full w-full animate-spin object-cover [animation-duration:9s] motion-reduce:animate-none"
      />
    </div>
  )
}

// TemaMusicalPlayer is the compact player of the local theme (`theme.mp3`/`.ogg`/`.flac`):
// the query (`entityThemeQueryOptions`, HEAD — without downloading the
// whole binary) checks the existence ONCE per screen load; without a theme
// (404/error), the component renders nothing — without an error, without reserved space.
// Play/pause is ALWAYS an action of the owner (no browser autoplays audio with sound, and
// it would be intrusive even if it allowed it); `loop` stays on in the element the whole
// time (it only matters while playing).
function TemaMusicalPlayer({ entityId }: { entityId: string }) {
  const t = useT()
  const { data: temTema } = useQuery(entityThemeQueryOptions(entityId))
  const audioRef = useRef<HTMLAudioElement>(null)
  const [tocando, setTocando] = useState(false)

  if (!temTema) {
    return null
  }

  function alternar() {
    const audio = audioRef.current
    if (!audio) {
      return
    }
    if (tocando) {
      audio.pause()
    } else {
      void audio.play()
    }
  }

  return (
    <div className="flex items-center gap-2">
      <Button
        type="button"
        variant="outline"
        size="sm"
        onClick={alternar}
        aria-label={
          tocando ? t('entity.action.pauseTheme') : t('entity.action.playTheme')
        }
      >
        {tocando ? <Pause aria-hidden /> : <Music aria-hidden />}
        {tocando ? t('entity.action.pauseTheme') : t('entity.action.playTheme')}
      </Button>
      <audio
        ref={audioRef}
        src={themeSrc(entityId)}
        loop
        onPlay={() => setTocando(true)}
        onPause={() => setTocando(false)}
      />
    </div>
  )
}

// ThemeDownloadButton is the "Download theme" button (ADR-0025):
// it materializes on disk the automatic candidate of ThemerrDB for the SAME
// theme.mp3 that TemaMusicalPlayer above starts to play — purely additive,
// it never replaces a theme the owner already placed (the server refuses with 409, and
// this action does not even appear in that case). It only appears with ALL the conditions: (a)
// there is a cached candidate (`disponivel`, GET .../theme/candidato —
// the ThemerrDB coverage is partial by nature, most items will not
// have one), (b) there is still NO local theme.* (`ja_tem_local`; if there is, the
// player above already covers the case — without offering a re-download, this endpoint has
// no `force`) and (c) yt-dlp is available. Any condition missing:
// no button, no error.
function ThemeDownloadButton({ entityId }: { entityId: string }) {
  const t = useT()
  const queryClient = useQueryClient()
  const { data: candidato } = useQuery(
    entityThemeCandidatoQueryOptions(entityId),
  )
  // `enabled` avoids the network call of the yt-dlp status when there is not even a
  // candidate to download (the common case, given the partial coverage of
  // ThemerrDB).
  const { data: ytdlp } = useQuery({
    ...ytdlpSettingsQueryOptions,
    enabled: candidato?.disponivel === true,
  })
  const downloadMutation = useMutation({
    mutationFn: () => downloadEntityTheme(entityId),
    onSuccess: () => {
      // It invalidates the prefix ['entity', id, 'theme']: it covers BOTH the HEAD that
      // TemaMusicalPlayer uses (the player starts to appear) AND this
      // very candidate (`ja_tem_local` becomes true, hiding the button).
      queryClient.invalidateQueries({
        queryKey: ['entity', entityId, 'theme'],
      })
    },
  })
  const mensagemErro =
    downloadMutation.error instanceof ApiError
      ? downloadMutation.error.message
      : null

  if (
    !candidato?.disponivel ||
    candidato.ja_tem_local ||
    downloadMutation.isSuccess ||
    !ytdlp?.disponivel
  ) {
    return null
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={downloadMutation.isPending}
        onClick={() => downloadMutation.mutate()}
      >
        {downloadMutation.isPending ? (
          <LoaderCircle className="animate-spin" aria-hidden />
        ) : (
          <Download aria-hidden />
        )}
        {downloadMutation.isPending
          ? t('entity.action.downloadingTheme')
          : t('entity.action.downloadTheme')}
      </Button>
      {mensagemErro ? (
        <span role="alert" className="text-sm font-medium text-destructive">
          {mensagemErro}
        </span>
      ) : null}
    </div>
  )
}

// ── Credits: cast, crew and production companies (read-only) ─────────────────

// TETO_ELENCO: how many actors the Cast shows BEFORE "Show all". The backend
// persists ~top 50 by billing; the UI opens with the ~24 most billed and expands on
// demand (huge grids — a movie with 76, a series with 781 — are not worth the initial scroll).
const TETO_ELENCO = 24

// Base of the person page on TMDB. In this phase the card is, at most, a link to the
// source (the person page of its own is a follow-up — §7). The `source_id` of the
// credits come from TMDB (credits/aggregate_credits), so the link is valid.
const TMDB_PESSOA = 'https://www.themoviedb.org/person/'

// sufixoEpisodios formats the episode count of the credit (series only): "12 eps"
// (singular "1 ep"). Absent/zero/movie → null (the UI does not show the suffix).
function sufixoEpisodios(n: number | null | undefined): string | null {
  if (!n || n <= 0) {
    return null
  }
  return `${n} ${n === 1 ? 'ep' : 'eps'}`
}

// SeloNaBiblioteca is the same internal-first badge already used in collection/similars
// (`Badge variant="secondary"`, text "In the library") — here compact (smaller px/py/text)
// to fit the tight cast card, with the `Library` icon reinforcing the
// meaning without repeating text. Purely decorative (`aria-hidden`): the destination is already
// announced by the `aria-label` of the Link that wraps it.
function SeloNaBiblioteca() {
  const t = useT()
  return (
    <Badge
      variant="secondary"
      className="w-fit gap-1 px-1.5 py-0 text-[10px]"
      aria-hidden
    >
      <Library className="size-3" aria-hidden />
      {t('entity.label.inLibrary')}
    </Badge>
  )
}

// PessoaCard is the reusable person card (2:3 photo + name + role), used in the grid of the
// Cast. It reuses the `PosterImg` (lazy + placeholder). The photo already comes PROXIED from the backend
// (direct use, without re-proxying). In a series, the role gains the suffix "· N eps" (episodes
// of the character).
//
// Internal-first (phase D): when the credit carries a `personId` (the person was already
// persisted by some import), the card links to the INTERNAL page of the person
// (`/person/{id}`) and gains the "In the library" badge (same pattern as the similar/collection),
// Without a `personId`, the card degrades to the usual external link (TMDB, new
// tab), without a badge — unchanged behavior.
function PessoaCard({
  sourceId,
  name,
  profileUrl,
  papel,
  eps,
  personId,
}: {
  sourceId: string
  name: string
  profileUrl: string | null
  papel: string | null
  eps: number | null | undefined
  personId?: string | null
}) {
  const t = useT()
  const sufEps = sufixoEpisodios(eps)
  const conteudo = (
    <>
      <PosterImg
        src={profileUrl}
        alt={t('entity.alt.personPhoto', { name })}
        className="transition group-hover:ring-2 group-hover:ring-foreground/30"
      />
      <span className="line-clamp-2 text-xs font-medium group-hover:text-foreground">
        {name}
      </span>
      {papel ? (
        <span className="line-clamp-2 text-xs text-muted-foreground">
          {papel}
          {sufEps ? ` · ${sufEps}` : null}
        </span>
      ) : sufEps ? (
        <span className="text-xs text-muted-foreground">{sufEps}</span>
      ) : null}
      {personId ? <SeloNaBiblioteca /> : null}
    </>
  )
  if (personId) {
    return (
      <Link
        to="/person/$personId"
        params={{ personId }}
        aria-label={t('entity.credit.viewPersonInLibrary', { name })}
        className="group flex flex-col gap-1"
      >
        {conteudo}
      </Link>
    )
  }
  return (
    <a
      href={`${TMDB_PESSOA}${sourceId}`}
      target="_blank"
      rel="noreferrer"
      className="group flex flex-col gap-1"
    >
      {conteudo}
    </a>
  )
}

// ElencoGrid shows the cast in a grid, in billing order (`order`). It opens with the
// TETO_ELENCO most billed ones; if there are more, a "Show all (N)" button expands the
// whole list (and "Show less" collapses it). Empty → the section disappears.
function ElencoGrid({ cast }: { cast: CreditCast[] }) {
  const t = useT()
  const [expandido, setExpandido] = useState(false)
  if (cast.length === 0) {
    return null
  }
  // It sorts by billing (smaller `order` = higher). The backend already delivers it sorted,
  // but we reinforce it here so the display does not depend on that. `slice()` keeps the
  // source intact; the sort is stable in V8.
  const ordenado = [...cast].sort((a, b) => a.order - b.order)
  const temMais = ordenado.length > TETO_ELENCO
  const visiveis = expandido ? ordenado : ordenado.slice(0, TETO_ELENCO)
  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-medium">
        {t('entity.section.cast')}
      </h2>
      <ul className="grid grid-cols-3 gap-3 sm:grid-cols-4 md:grid-cols-6">
        {visiveis.map((p, i) => (
          <li key={`${p.source_id}:${i}`}>
            <PessoaCard
              sourceId={p.source_id}
              name={p.name}
              profileUrl={p.profile_url ?? null}
              papel={p.character || null}
              eps={p.episode_count}
              personId={p.person_id ?? null}
            />
          </li>
        ))}
      </ul>
      {temMais ? (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="w-fit"
          onClick={() => setExpandido((v) => !v)}
        >
          {expandido
            ? t('entity.action.showLess')
            : t('entity.action.showAllCast', { n: ordenado.length })}
        </Button>
      ) : null}
    </section>
  )
}

// Departments that appear FIRST in the Crew (Directing, then Writing),
// recognized by keys normalized in pt-BR and en (the source may not translate the
// label). The other departments come out in alphabetical order.
const PRIORIDADE_DEPARTAMENTO: string[][] = [
  ['direção', 'direcao', 'directing', 'direction'],
  ['roteiro', 'escrita', 'writing'],
]

// rankDepartamento returns the priority position of the department (0 = first).
// A department outside the list → a high rank (sorted alphabetically among themselves later).
function rankDepartamento(dep: string): number {
  const norm = dep.trim().toLowerCase()
  const i = PRIORIDADE_DEPARTAMENTO.findIndex((chaves) => chaves.includes(norm))
  return i === -1 ? PRIORIDADE_DEPARTAMENTO.length : i
}

interface GrupoEquipe {
  department: string
  membros: CreditCrew[]
}

// agruparEquipe groups the crew by `department` preserving the arrival order of the
// members inside the group, and sorts the groups: Directing/Writing first, the rest in
// alphabetical order (in the active locale). An empty department falls into a "Crew" group.
function agruparEquipe(
  crew: CreditCrew[],
  t: Translate,
  locale: string,
): GrupoEquipe[] {
  const semDepartamento = t('entity.section.crew')
  const mapa = new Map<string, CreditCrew[]>()
  for (const m of crew) {
    const dep = m.department?.trim() || semDepartamento
    const lista = mapa.get(dep)
    if (lista) {
      lista.push(m)
    } else {
      mapa.set(dep, [m])
    }
  }
  return [...mapa.entries()]
    .map(([department, membros]) => ({ department, membros }))
    .sort((a, b) => {
      const ra = rankDepartamento(a.department)
      const rb = rankDepartamento(b.department)
      return ra !== rb
        ? ra - rb
        : a.department.localeCompare(b.department, locale)
    })
}

// MembroNome is the name of the crew member, linked. Internal-first (phase D):
// with a `personId`, it links to the INTERNAL page of the person (with a discreet `Library`
// icon and a descriptive `aria-label`); without a `personId`, it degrades to the usual external
// link (TMDB, new tab) — unchanged behavior. Extracted from `EquipeGrupos` so as
// not to bloat the JSX of the list.
function MembroNome({
  name,
  sourceId,
  personId,
}: {
  name: string
  sourceId: string
  personId?: string | null
}) {
  const t = useT()
  if (personId) {
    return (
      <Link
        to="/person/$personId"
        params={{ personId }}
        aria-label={t('entity.credit.viewPersonInLibrary', { name })}
        className="inline-flex items-center gap-1 font-medium hover:underline"
      >
        <Library className="size-3 text-muted-foreground" aria-hidden />
        {name}
      </Link>
    )
  }
  return (
    <a
      href={`${TMDB_PESSOA}${sourceId}`}
      target="_blank"
      rel="noreferrer"
      className="font-medium hover:underline"
    >
      {name}
    </a>
  )
}

// grupoTemFoto decides whether the DEPARTMENT gets a photo in the Crew: the same
// ones prioritized in the grouping (`PRIORIDADE_DEPARTAMENTO` — Directing/Writing),
// reusing `rankDepartamento` without changing that list/logic itself. A deliberate
// curation — it is not ALL the ~40 crew names (it would weigh on the page without a proportional
// gain; see §6 of the spec), only the roles the owner recognizes at a glance,
// in the same visual pattern as the Cast.
function grupoTemFoto(department: string): boolean {
  return rankDepartamento(department) < PRIORIDADE_DEPARTAMENTO.length
}

// GrupoEquipeFotos shows ONE prioritized department as a grid of cards with a
// photo — it reuses the SAME `PessoaCard` of the `ElencoGrid` (full visual parity,
// including the "In the library" badge and the graceful degradation without `profile_url`).
function GrupoEquipeFotos({ grupo }: { grupo: GrupoEquipe }) {
  return (
    <div className="flex flex-col gap-2">
      <h3 className="text-sm font-medium text-foreground/70">
        {grupo.department}
      </h3>
      <ul className="grid grid-cols-3 gap-3 sm:grid-cols-4 md:grid-cols-6">
        {grupo.membros.map((m, i) => (
          <li key={`${m.source_id}:${m.job}:${i}`}>
            <PessoaCard
              sourceId={m.source_id}
              name={m.name}
              profileUrl={m.profile_url ?? null}
              papel={m.job || null}
              eps={m.episode_count}
              personId={m.person_id ?? null}
            />
          </li>
        ))}
      </ul>
    </div>
  )
}

// GrupoEquipeTexto is the ORIGINAL compact textual list (name + job, with "· N
// eps" in a series) — unchanged, only extracted to alternate with `GrupoEquipeFotos`.
function GrupoEquipeTexto({ grupo }: { grupo: GrupoEquipe }) {
  return (
    <div className="flex flex-col gap-1 sm:flex-row sm:gap-3">
      <h3 className="shrink-0 text-sm font-medium text-foreground/70 sm:w-40">
        {grupo.department}
      </h3>
      <ul className="flex flex-wrap gap-x-3 gap-y-0.5 text-sm">
        {grupo.membros.map((m, i) => {
          const sufEps = sufixoEpisodios(m.episode_count)
          return (
            <li key={`${m.source_id}:${m.job}:${i}`}>
              <MembroNome
                name={m.name}
                sourceId={m.source_id}
                personId={m.person_id}
              />
              <span className="text-muted-foreground">
                {' '}
                — {m.job}
                {sufEps ? ` · ${sufEps}` : null}
              </span>
            </li>
          )
        })}
      </ul>
    </div>
  )
}

// EquipeGrupos shows the crew grouped by department: the prioritized ones
// (Directing/Writing) in a grid with photos (parity with the Cast); the others in a
// compact textual list, as it already was. Empty → the section disappears.
function EquipeGrupos({ crew }: { crew: CreditCrew[] }) {
  const t = useT()
  const locale = useLocale()
  if (crew.length === 0) {
    return null
  }
  const grupos = agruparEquipe(crew, t, locale)
  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-medium">
        {t('entity.section.crew')}
      </h2>
      <div className="flex flex-col gap-4">
        {grupos.map((grupo) =>
          grupoTemFoto(grupo.department) ? (
            <GrupoEquipeFotos key={grupo.department} grupo={grupo} />
          ) : (
            <GrupoEquipeTexto key={grupo.department} grupo={grupo} />
          ),
        )}
      </div>
    </section>
  )
}

// ProdutorasBand shows the production companies/studios in a band: the logo (already proxied) in a
// chip with a light background (so dark logos show up in the dark theme) or the name in a
// `<Badge>` when there is no logo. Empty → the section disappears.
function ProdutorasBand({ companies }: { companies: CreditCompany[] }) {
  const t = useT()
  if (companies.length === 0) {
    return null
  }
  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-medium">
        {t('entity.section.companies')}
      </h2>
      <ul className="flex flex-wrap items-center gap-3">
        {companies.map((c, i) => (
          <li key={`${c.source_id}:${i}`}>
            {c.logo_url ? (
              <span className="flex h-10 items-center justify-center rounded-md bg-white px-3 ring-1 ring-foreground/10">
                <img
                  src={c.logo_url}
                  alt={c.name}
                  loading="lazy"
                  className="max-h-8 w-auto object-contain"
                />
              </span>
            ) : (
              <Badge variant="outline">{c.name}</Badge>
            )}
          </li>
        ))}
      </ul>
    </section>
  )
}

// HeroImersivo is the FULL-BLEED hero: the background (backdrop/still) and the content
// layer (poster + text) occupy the SAME cell of a single-row grid —
// a robust "overlay without `position: absolute`" technique that lets the HEIGHT of the
// row be the larger of the two (the background has a MINIMUM height of ~40% of the viewport;
// the content grows freely beyond that when the text is long, without cutting
// anything — unlike `absolute` content, which does not push the height of the parent). The
// width bleeds to the edge of the SCREEN (`w-screen` + negative 50vw margin), but the
// inner content becomes centered again in the usual `max-w-4xl`. A
// gradient darkens the background behind the text (AA contrast in light/dark,
// criterion kept).
function HeroImersivo({
  detail,
  situacao,
  titleBadges,
  actions,
  descriptionExtra,
  genresExtra,
  availableArtwork,
  logo,
  banner,
  disc,
  entityId,
}: {
  detail: RichDetail
  situacao: RotuloSituacao | null
  titleBadges?: ReactNode
  // Actions of the item: "Watch trailer" + the "Gerenciar" dropdown (entity)
  // or "Import" (external) — now INSIDE the hero, close to the title, instead of
  // in a separate bar above it (a deliberate deviation from the reference print,
  // which uses 2-3 loose buttons — §1).
  actions?: ReactNode
  descriptionExtra?: ReactNode
  genresExtra?: ReactNode
  availableArtwork?: Record<string, number>
  logo?: Asset
  banner?: Asset
  disc?: Asset
  entityId?: string
}) {
  const t = useT()
  return (
    <div className="dark relative left-1/2 right-1/2 grid w-screen -mx-[50vw]">
      {/* Contrast fix: the `dark` class here is DELIBERATE and
          INDEPENDENT of the app theme (light/dark) — the `.dark` selector of this
          project is a plain class (`@custom-variant dark (&:is(.dark *))`
          in index.css), not a media query, so applying it to this wrapper
          FORCES the dark palette (near-black background, near-white text) on
          the whole content of the hero, without affecting the rest of the screen. Without it, in
          the LIGHT theme the `background` token used in the gradient below is white —
          that is, the overlay LIGHTENS instead of darkening the backdrop, and dark
          text (`text-foreground` of the light theme) becomes unreadable over a
          light/colorful background artwork. Backdrops are photography of unpredictable
          brightness (we do not control the artwork) — which is why the hero, as in
          TMDB/Jellyfin, ALWAYS uses light text over a dark scrim, and does not
          adapt to the overall theme of the app.

          Background: Episode uses the 16:9 STILL in place of the backdrop (the
          poster disappears — an episode has none); the other types use the backdrop,
          with a minimum height of ~40vh (HeroBackdropFundo) even without an image
          (placeholder). Explicit `z-0`/`z-10`/`z-20` (instead of trusting the
          DOM order) guarantee the background→gradient→content stack without
          ambiguity between the overlapping grid items. */}
      <div className="relative z-0 col-start-1 row-start-1">
        {detail.type === 'episode' ? (
          <StillFrame
            src={detail.stillUrl}
            alt={t('entity.alt.still', { title: detail.title })}
          />
        ) : (
          <HeroBackdropFundo src={detail.backdropUrl} />
        )}
      </div>

      {/* Overlay: vertical gradient (the text lives at the base) + horizontal on screens
          wide ones (darker on the left, in the Jellyfin/TMDB pattern). `background`
          here is always the near-black of the `.dark` forced on the wrapper (not the one of
          the app theme) — it guarantees AA without depending on the brightness of the image behind. */}
      <div className="relative z-10 col-start-1 row-start-1 bg-gradient-to-t from-background via-background/75 to-background/10" />
      <div className="relative z-10 col-start-1 row-start-1 hidden bg-gradient-to-r from-background/90 via-background/25 to-transparent sm:block" />

      {/* Content: re-centered in the usual max-w-4xl, anchored at the base of the
          grid cell (`items-end`) — it grows upwards without cutting when the
          text is taller than the minimum height of the background. */}
      <div className="relative z-20 col-start-1 row-start-1 flex items-end">
        <div className="mx-auto flex w-full max-w-4xl flex-col gap-4 px-4 py-6 sm:flex-row sm:items-end sm:gap-6 sm:py-8">
          {detail.type === 'episode' ? null : detail.type === 'album' ||
            detail.type === 'artista' ? (
            // Album/artist: SQUARE cover/photo (1:1) in place of the
            // 2:3 poster — without the situation band nor the spinning disc (they only
            // make sense for a series/movie).
            <CapaQuadrada
              src={detail.posterUrl}
              alt={
                detail.type === 'artista'
                  ? t('entity.alt.personPhoto', { name: detail.title })
                  : t('entity.alt.albumCover', { title: detail.title })
              }
              circular={detail.type === 'artista'}
            />
          ) : (
            <div className="relative w-40 shrink-0 sm:w-56">
              <PosterImg
                src={detail.posterUrl}
                alt={t('entity.alt.poster', { title: detail.title })}
              />
              <FaixaStatusPoster situacao={situacao} />
              {disc ? <DiscoGiratorio disc={disc} /> : null}
            </div>
          )}
          <div className="flex min-w-0 flex-col gap-2">
            {banner ? <BannerFaixa banner={banner} /> : null}

            {/* Title row: the LOGO (when preferred) replaces the text
                of the title — without it, it falls back to the usual `<h1>` (no regression). The year
                and the type stay next to it, as before. */}
            <div className="flex flex-wrap items-end gap-2">
              {logo ? (
                <LogoHero logo={logo} alt={detail.title} />
              ) : (
                <h1 className="font-heading text-2xl font-semibold text-foreground sm:text-3xl">
                  {detail.title}
                </h1>
              )}
              {detail.year ? (
                <span className="text-lg text-muted-foreground">
                  {detail.year}
                </span>
              ) : null}
              <Badge variant="outline">{rotuloTipo(detail.type, t)}</Badge>
              {/* "Assistido" badge: only the ENTITY screen passes `entityId` —
                  the same gate as `TemaMusicalPlayer`/logo/banner/disc above. It reads the
                  history (kind="assistir") and disappears without any `completed`
                  event, it never shows an active "não assistido". */}
              {entityId ? <EstadoAssistidoBadge entityId={entityId} /> : null}
              {titleBadges}
            </div>

            {/* Actions: trailer + "Gerenciar"/"Import", close to the title —
                it replaces the former separate `BarraAcoesHero` above the hero. It disappears
                entirely when there is neither a trailer nor `actions` (the same guard
                as before, now inside the component itself). */}
            {actions || primeiroTrailer(detail.videos) ? (
              <div className="flex flex-wrap items-center gap-2">
                <TrailerButton videos={detail.videos} />
                {/* "Baixar trailer": only on the ENTITY screen (entityId) —
                    materializing on disk requires the real library folder,
                    which the external screen (not cataloged yet) does not have. */}
                {entityId ? (
                  <TrailerDownloadButton
                    entityId={entityId}
                    videos={detail.videos}
                  />
                ) : null}
                {actions}
              </div>
            ) : null}

            {detail.originalTitle && detail.originalTitle !== detail.title ? (
              <p className="text-sm text-muted-foreground">
                {detail.originalTitle}
              </p>
            ) : null}

            {/* Genres: right below the title — compact text/badges,
                without a "Gêneros" heading of its own. */}
            {detail.genres.length > 0 ? (
              <div className="flex flex-wrap items-center gap-1.5">
                {detail.genres.map((g) => (
                  <Badge key={g} variant="outline" className="font-normal">
                    {g}
                  </Badge>
                ))}
                {genresExtra}
              </div>
            ) : null}

            <div className="flex flex-wrap items-center gap-2 pt-1">
              {detail.certification ? (
                <Badge variant="secondary">{detail.certification}</Badge>
              ) : null}
              {detail.runtime ? (
                <span className="inline-flex items-center gap-1 text-sm text-muted-foreground">
                  <Clock className="size-4" aria-hidden />
                  {formatarDuracao(detail.runtime)}
                </span>
              ) : null}
              {detail.originalLanguage ? (
                <span className="text-sm text-muted-foreground">
                  {rotuloIdioma(detail.originalLanguage)}
                </span>
              ) : null}
            </div>

            {/* Ratings row: the circular medallion of the owner's LOCAL rating
                (ADR-0012, amber outline, unchanged) + a compact badge PER
                SOURCE in `detail.ratings` (logo/label via `metaDaFonte`).
                It appears with a single source (the owner's decision: the common case today — only
                TMDB — it does not stay hidden waiting for 2+ sources). It replaces the former
                local×single-source pair. */}
            {detail.localRating != null || detail.ratings.length > 0 ? (
              <div className="flex flex-wrap items-center gap-2 pt-1">
                {detail.localRating != null ? (
                  <NotaMedalhao value={detail.localRating} variant="local" />
                ) : null}
                {detail.ratings.map((r) => (
                  <NotaFonteBadge
                    key={r.source}
                    source={r.source}
                    value={r.value}
                  />
                ))}
              </div>
            ) : null}

            {detail.tagline ? (
              <p className="text-base font-medium italic text-foreground/80">
                “{detail.tagline}”
              </p>
            ) : null}

            {/* Synopsis: it moves INSIDE the hero — it is no longer a section
                separate below the card, without a heading of its own (a prominent
                paragraph over the overlay, in the Jellyfin/TMDB pattern). */}
            {detail.description ? (
              <p className="max-w-3xl text-sm text-foreground/90 sm:text-base">
                {detail.description}
              </p>
            ) : null}
            {descriptionExtra}

            {entityId ? (
              <div className="flex flex-wrap items-center gap-2">
                <TemaMusicalPlayer entityId={entityId} />
                {/* "Baixar tema": only on the ENTITY screen (entityId) —
                    the same restriction as TrailerDownloadButton above, materializing
                    requires the real library folder. */}
                <ThemeDownloadButton entityId={entityId} />
              </div>
            ) : null}

            <SelosArtes available={availableArtwork} />
          </div>
        </div>
      </div>
    </div>
  )
}

// ── Main component ───────────────────────────────────────────────────────────

export function RichDetailView({
  detail,
  source,
  back,
  titleBadges,
  actions,
  descriptionExtra,
  genresExtra,
  availableArtwork,
  entityId,
  children,
}: {
  detail: RichDetail
  // source of the source (e.g. 'tmdb') to build the links of the similars.
  source: string
  // "back" area (Link or button) — each screen decides the destination.
  back?: ReactNode
  // Badges next to the title (status, "edited") — entity.
  titleBadges?: ReactNode
  // Actions INSIDE the hero, close to the title (Import on the external one; the
  // "Gerenciar" dropdown on the entity) — the trailer joins them at the side, computed in
  // the `HeroImersivo` itself.
  actions?: ReactNode
  // Notes after the synopsis (fallback/edited badges) — entity.
  descriptionExtra?: ReactNode
  // Notes after the genres — entity.
  genresExtra?: ReactNode
  // Artwork count per kind: the row of availability badges in the hero.
  // Only the ENTITY screen passes it (it reads `/artwork`); the external one omits it (without the row).
  availableArtwork?: Record<string, number>
  // INTERNAL id of the entity: it enables the preferred logo/banner/disc (it reads
  // `GET .../assets`) and the player of the local theme song (`GET .../theme`) — only the
  // ENTITY screen passes it; the external one (before importing) has no assets/local folder
  // yet, so it omits it (without regression, the same gating as `availableArtwork`).
  entityId?: string
  // Extra sections attached (override editor, gallery, files) — entity.
  children?: ReactNode
}) {
  // SITUATION badge of the series: it only exists when the source informed the situation
  // (enriched series). For movie/episode the field is absent → `situacao` is null and
  // the badge does not appear — without gating by type, only by the presence of the data.
  const t = useT()
  const situacao = rotuloSituacao(detail.airingStatus, t)

  // Preferred logo/banner/disc: the SAME call to `GET .../assets`
  // that the gallery (art-gallery.tsx) uses, here only to extract the three kinds of the
  // hero. `enabled: !!entityId` keeps the external screen without any call
  // (without assets of its own before importing).
  const assetsQuery = useQuery({
    ...assetsQueryOptions(entityId ?? ''),
    enabled: !!entityId,
  })
  const heroAssets = assetsPreferidosDoHero(
    entityId ? assetsQuery.data : undefined,
  )

  return (
    <div className="flex flex-col gap-6">
      {back}

      <HeroImersivo
        detail={detail}
        situacao={situacao}
        titleBadges={titleBadges}
        actions={actions}
        descriptionExtra={descriptionExtra}
        genresExtra={genresExtra}
        availableArtwork={availableArtwork}
        logo={heroAssets.logo}
        banner={heroAssets.banner}
        disc={heroAssets.disc}
        entityId={entityId}
      />

      {/* Row of external links: the FIRST section after the hero — it was the
          LAST one of the page (after Production companies); it moved to the top because it is
          outbound navigation (TMDB/IMDb/TVDB), not reading metadata. */}
      <ExternalLinksRow links={detail.externalLinks} />

      {detail.keywords.length > 0 ? (
        <section className="flex flex-col gap-2">
          <h2 className="font-heading text-lg font-medium">
            {t('entity.section.keywords')}
          </h2>
          <div className="flex flex-wrap gap-1.5">
            {detail.keywords.map((k) => (
              <Badge key={k} variant="secondary">
                {k}
              </Badge>
            ))}
          </div>
        </section>
      ) : null}

      <VideosRail videos={detail.videos} />
      {detail.collection ? (
        <ColecaoBand source={source} colecao={detail.collection} />
      ) : null}
      <SimilaresRail source={source} similares={detail.similar} />
      <ElencoGrid cast={detail.cast} />
      <EquipeGrupos crew={detail.crew} />
      <ProdutorasBand companies={detail.companies} />

      {children}
    </div>
  )
}
