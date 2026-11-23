import { useCallback, useEffect, useRef, useState } from 'react'
import {
  ChevronLeft,
  ChevronRight,
  Download,
  Maximize2,
  Minimize2,
  Save,
  Star,
  Trash2,
  X,
} from 'lucide-react'
import { ArtworkImage } from '@/components/artwork-image'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { rotuloIdioma } from '@/lib/languages'
import { infoKind } from '@/lib/artwork'
import { useT } from '@/lib/i18n'
import type { Translate } from '@/lib/i18n/types'
import type { ArtworkItem } from '@/lib/api'

// ArtLightbox: near-full overlay to see ONE artwork at high resolution, with prev/next
// navigation WITHIN the kind (keyboard arrows + buttons), Esc/backdrop-click
// to close, trapped focus (accessibility) and a zoom toggle (fit ↔ 100%).
// It shows language/likes/origin and triggers the ACTIONS — the MUTATION lives in the parent (the gallery),
// which injects the handlers and the `ocupado`/`erro` state; the lightbox only decides WHICH
// buttons fit each item (prefer/download/remove, according to local × inventory
// × owner).

export interface AcoesLightbox {
  // Set as PRINCIPAL (source of truth `is_principal`): in the
  // parent, it creates the asset first if the item is still only an inventory candidate
  // (without `asset_id`), then calls PUT prefer — which materializes (if needed) and
  // renames the physical files. A single click chains the two calls
  // when necessary (journey §2 step 4).
  onPreferir: (item: ArtworkItem) => void
  // Save INTO the real library folder: creates the asset first if
  // needed, then materializes (downloads to the cache if needed + copies to disk).
  // Distinct from `onBaixar` (which only caches internally, without touching the real folder).
  onMaterializar: (item: ArtworkItem) => void
  // Download to the internal CACHE (materialize without preferring, without touching the real folder):
  // POST select {prefer:false}. It only makes sense for artwork not cached yet.
  onBaixar: (item: ArtworkItem) => void
  // Remove: only for an asset of the OWNER (source==='user'); DELETE of the asset.
  onRemover: (item: ArtworkItem) => void
  // Some mutation in flight — it disables the action buttons.
  ocupado: boolean
  // Ownership (`ResolvedEntity.owned`): without a present media_file there is no
  // destination folder in the real library — it disables "Salvar na pasta"/"Definir
  // como principal" with an explanatory tooltip (journey §2 step 7). It does not affect
  // "Baixar pro cache"/"Remover" (they do not touch the real folder).
  podeSalvarNaPasta: boolean
  // Error message of an action (message from the API envelope), shown in the footer.
  erro: string | null
}

// Catalog key of the tooltip that explains why the materialize buttons are disabled
// when the entity has no present media_file (catalog only).
const CHAVE_SEM_PASTA = 'media.lightbox.noFolderTooltip'

// Kind label through the catalog: an unknown kind echoes its raw name (it never turns
// into a blank), the same fallback `lib/artwork.ts` keeps for the screens that were not
// extracted yet.
function rotuloArte(kind: string, t: Translate): string {
  const chave = `media.artworkKind.${kind}`
  const rotulo = t(chave)
  return rotulo === chave ? kind : rotulo
}

// Selectors of the focusable elements for the "focus trap" (Tab trap) of the dialog.
const FOCAVEIS =
  'button:not([disabled]), [href], input, [tabindex]:not([tabindex="-1"])'

export function ArtLightbox({
  kind,
  items,
  index,
  onIndex,
  onClose,
  acoes,
}: {
  kind: string
  items: ArtworkItem[]
  // CURRENT index within the kind (controlled by the parent — the same list as the rows).
  index: number
  onIndex: (i: number) => void
  onClose: () => void
  acoes: AcoesLightbox
}) {
  const containerRef = useRef<HTMLDivElement>(null)
  const t = useT()
  // Zoom: `false` = fit the screen (contain); `true` = 100% (natural resolution,
  // with scroll in the frame). It resets when the artwork changes.
  const [zoom, setZoom] = useState(false)

  const item = items[index]
  const total = items.length

  const irAnterior = useCallback(() => {
    setZoom(false)
    onIndex((index - 1 + total) % total)
  }, [index, total, onIndex])

  const irProxima = useCallback(() => {
    setZoom(false)
    onIndex((index + 1) % total)
  }, [index, total, onIndex])

  // Keyboard: Esc closes; arrows navigate; Tab stays TRAPPED in the focusables of the dialog.
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
        return
      }
      if (e.key === 'ArrowLeft') {
        e.preventDefault()
        irAnterior()
        return
      }
      if (e.key === 'ArrowRight') {
        e.preventDefault()
        irProxima()
        return
      }
      if (e.key === 'Tab') {
        const focaveis =
          containerRef.current?.querySelectorAll<HTMLElement>(FOCAVEIS)
        if (!focaveis || focaveis.length === 0) {
          return
        }
        const primeiro = focaveis[0]
        const ultimo = focaveis[focaveis.length - 1]
        const ativo = document.activeElement
        if (e.shiftKey && ativo === primeiro) {
          e.preventDefault()
          ultimo.focus()
        } else if (!e.shiftKey && ativo === ultimo) {
          e.preventDefault()
          primeiro.focus()
        }
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [irAnterior, irProxima, onClose])

  // Focus trap: it focuses the dialog on open and returns the focus to the previous element on close.
  useEffect(() => {
    const anterior = document.activeElement as HTMLElement | null
    containerRef.current?.focus()
    return () => anterior?.focus?.()
  }, [])

  if (!item) {
    return null
  }

  const info = infoKind(kind)
  const rotulo = rotuloArte(kind, t)
  const legenda = t('media.lightbox.caption', {
    kind: rotulo,
    n: index + 1,
    total,
  })

  return (
    // Its own TooltipProvider (it does not depend on the ancestor): the lightbox is mounted in
    // several places (gallery, tests) and the two disabled buttons below
    // need the design system tooltip always working.
    <TooltipProvider>
      <div
        role="dialog"
        aria-modal="true"
        aria-label={legenda}
        tabIndex={-1}
        ref={containerRef}
        // Backdrop: a click OUTSIDE the image/panel closes. The children stop the propagation.
        onClick={onClose}
        // Backdrop ALWAYS dark (it values the artwork), even in the light theme: the `dark` class
        // forces the shadcn tokens (text/buttons) to light-on-dark in the lightbox subtree.
        className="dark fixed inset-0 z-50 flex flex-col bg-black/85 text-foreground outline-none backdrop-blur-sm"
      >
        {/* Top bar: caption + zoom + close. */}
        <div
          className="flex items-center gap-2 p-3"
          onClick={(e) => e.stopPropagation()}
        >
          <span className="text-sm font-medium text-muted-foreground">
            {legenda}
          </span>
          <div className="ml-auto flex items-center gap-1.5">
            <Button
              type="button"
              variant="outline"
              size="sm"
              aria-label={
                zoom
                  ? t('media.lightbox.fitToScreen')
                  : t('media.lightbox.zoomToFull')
              }
              aria-pressed={zoom}
              onClick={() => setZoom((z) => !z)}
            >
              {zoom ? <Minimize2 aria-hidden /> : <Maximize2 aria-hidden />}
              {zoom
                ? t('media.lightbox.fitShort')
                : t('media.lightbox.zoomShort')}
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              aria-label={t('common.action.close')}
              onClick={onClose}
            >
              <X aria-hidden />
            </Button>
          </div>
        </div>

        {/* Stage: the artwork at high resolution + navigation arrows. */}
        <div className="relative flex min-h-0 flex-1 items-center justify-center px-2">
          {total > 1 ? (
            <Button
              type="button"
              variant="outline"
              size="icon"
              aria-label={t('media.lightbox.previousArtwork')}
              className="absolute left-3 z-10"
              onClick={(e) => {
                e.stopPropagation()
                irAnterior()
              }}
            >
              <ChevronLeft aria-hidden />
            </Button>
          ) : null}

          <div
            className={`flex h-full w-full items-center justify-center ${
              zoom ? 'overflow-auto' : ''
            } ${info.contain ? 'bg-muted/40' : ''}`}
            onClick={(e) => e.stopPropagation()}
          >
            {/* `key` per URL: the placeholder state of a broken artwork belongs to the
                URL, not to the position in the kind. */}
            <ArtworkImage
              key={item.url}
              src={item.url}
              alt={t('media.lightbox.imageAlt', { kind: rotulo, n: index + 1 })}
              className={
                zoom
                  ? 'min-h-24 max-w-none'
                  : 'min-h-24 max-h-full max-w-full object-contain'
              }
            />
          </div>

          {total > 1 ? (
            <Button
              type="button"
              variant="outline"
              size="icon"
              aria-label={t('media.lightbox.nextArtwork')}
              className="absolute right-3 z-10"
              onClick={(e) => {
                e.stopPropagation()
                irProxima()
              }}
            >
              <ChevronRight aria-hidden />
            </Button>
          ) : null}
        </div>

        {/* Footer: metadata (language/likes/origin/preferred) + actions. */}
        <div
          className="flex flex-col gap-2 border-t border-foreground/10 p-3"
          onClick={(e) => e.stopPropagation()}
        >
          <div className="flex flex-wrap items-center gap-1.5">
            <Badge variant="outline">{rotulo}</Badge>
            {item.is_principal ? (
              <Badge variant="secondary">{t('media.lightbox.principal')}</Badge>
            ) : null}
            {item.is_local_in_library ? (
              <Badge variant="outline">
                {t('media.lightbox.savedInFolder')}
              </Badge>
            ) : null}
            {item.lang ? (
              <Badge variant="secondary">{rotuloIdioma(item.lang)}</Badge>
            ) : (
              <Badge variant="outline">{t('media.lightbox.textless')}</Badge>
            )}
            {item.likes ? (
              <span className="inline-flex items-center gap-1 text-sm text-muted-foreground">
                <Star className="size-4" aria-hidden />
                {item.likes}
              </span>
            ) : null}
            <Badge variant="outline">{item.source}</Badge>
            {item.disc_type ? (
              <Badge variant="outline">{item.disc_type}</Badge>
            ) : null}
            {item.season ? (
              <Badge variant="outline">
                {t('media.lightbox.seasonNumber', { n: item.season })}
              </Badge>
            ) : null}
          </div>

          <div className="flex flex-wrap items-center gap-1.5">
            {!item.is_principal ? (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    type="button"
                    size="sm"
                    disabled={acoes.ocupado || !acoes.podeSalvarNaPasta}
                    onClick={() => acoes.onPreferir(item)}
                  >
                    <Star aria-hidden />
                    {t('media.lightbox.setPrincipal')}
                  </Button>
                </TooltipTrigger>
                {acoes.podeSalvarNaPasta ? null : (
                  <TooltipContent>{t(CHAVE_SEM_PASTA)}</TooltipContent>
                )}
              </Tooltip>
            ) : null}
            {!item.is_local_in_library ? (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={acoes.ocupado || !acoes.podeSalvarNaPasta}
                    onClick={() => acoes.onMaterializar(item)}
                  >
                    <Save aria-hidden />
                    {t('media.lightbox.saveToFolder')}
                  </Button>
                </TooltipTrigger>
                {acoes.podeSalvarNaPasta ? null : (
                  <TooltipContent>{t(CHAVE_SEM_PASTA)}</TooltipContent>
                )}
              </Tooltip>
            ) : null}
            {!item.is_local ? (
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={acoes.ocupado}
                onClick={() => acoes.onBaixar(item)}
              >
                <Download aria-hidden />
                {t('media.lightbox.downloadToCache')}
              </Button>
            ) : null}
            {item.source === 'user' && item.asset_id ? (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                aria-label={t('media.lightbox.removeArtwork')}
                disabled={acoes.ocupado}
                onClick={() => acoes.onRemover(item)}
              >
                <Trash2 aria-hidden />
                {t('common.action.remove')}
              </Button>
            ) : null}
          </div>

          {acoes.erro ? (
            <p role="alert" className="text-sm font-medium text-destructive">
              {acoes.erro}
            </p>
          ) : null}
        </div>
      </div>
    </TooltipProvider>
  )
}
