import { useEffect, useRef, useState, type ChangeEvent } from 'react'
import {
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from '@tanstack/react-query'
import { FolderCheck, Star, Upload } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Select } from '@/components/ui/select'
import { ErrorState } from '@/components/query-states'
import { ArtLightbox, type AcoesLightbox } from '@/components/art-lightbox'
import { ArtworkImage } from '@/components/artwork-image'
import {
  ApiError,
  arteSrc,
  deleteAsset,
  kindsParaTipo,
  materializeAsset,
  preferAsset,
  selectArtwork,
  uploadAsset,
  MIMES_ARTE,
  type ArtworkItem,
  type KindArte,
  type SelectArtworkRequest,
} from '@/lib/api'
import {
  infoKind,
  larguraKind,
  linhasDeArtwork,
  type LinhaArte,
} from '@/lib/artwork'
import { useT } from '@/lib/i18n'
import type { Translate } from '@/lib/i18n/types'
import { artworkQueryOptions } from '@/lib/queries'

// Artwork gallery (a rewrite of the earlier artwork screens): it consumes the GROUPED
// endpoint `GET /artwork` and renders ONE ROW (horizontal carousel) per kind
// present, each card in the correct ASPECT of the type (2:3 poster, 16:9
// fanart/thumb/still, wide transparent logo/clearart, square disc/character, banner strip).
// It is SOURCE-AGNOSTIC: TMDB, fanart.tv and the owner's assets enter the same rows;
// the origin becomes just a badge. Clicking an artwork opens the lightbox (high resolution + zoom +
// navigation + actions). The owner can also UPLOAD a custom artwork.
//
// Actions and routing (contract, extended later):
//   • Set as PRINCIPAL (is_principal, source of truth) → asset already existing:
//     PUT .../assets/{asset_id}/prefer (the backend materializes+renames on its own, if
//     needed); INVENTORY candidate (without asset_id): chains POST
//     .../artwork/select {prefer:false} (creates the asset) + the PUT prefer above.
//   • Save INTO the real library folder (is_local_in_library) → same chaining
//     as the inventory candidate when needed, ending in POST
//     .../assets/{asset_id}/materialize.
//   • Download to the internal CACHE (materialize without preferring/touching the real folder) →
//     POST .../artwork/select {prefer:false}.
//   • Remove (OWNER only, source==='user') → DELETE .../assets/{asset_id}.

// Acceptance of the <input file> and of the validation on the client (same set as the backend).
const ACCEPT = MIMES_ARTE.join(',')

// Kind label through the catalog: an unknown kind echoes its raw name (it never turns
// into a blank). `lib/artwork.ts` keeps the same fallback for the screens that were
// not extracted yet.
function rotuloArte(kind: string, t: Translate): string {
  const chave = `media.artworkKind.${kind}`
  const rotulo = t(chave)
  return rotulo === chave ? kind : rotulo
}

// Invalidation after changing the artwork: the gallery/badges (`['artwork', id]`), the detail
// (`['entity', id]`, prefix — every lang variation) and the grid (`['entities']`)
// — they all read the served poster, which follows the new precedence.
function invalidarAposArte(qc: QueryClient, id: string) {
  qc.invalidateQueries({ queryKey: ['artwork', id] })
  qc.invalidateQueries({ queryKey: ['entity', id] })
  qc.invalidateQueries({ queryKey: ['entities'] })
}

// Lightbox state: the open kind and the index within it (or null = closed).
interface Aberto {
  kind: string
  index: number
}

// identidadeItem returns a STABLE identifier of the item across revalidations of the
// gallery: `asset_id` when the item already is a materialized asset;
// otherwise `src_url` (inventory candidate, still without asset_id). The lightbox uses
// that identity to re-anchor itself on the SAME artwork after the list reorders
// (e.g. materializing a candidate moves it to another position), instead of staying stuck to
// a numeric index that now points at another artwork.
function identidadeItem(item: ArtworkItem): string {
  return item.asset_id ?? item.src_url ?? ''
}

// garantirAssetId resolves the `asset_id` of a gallery item: if the item ALREADY is an
// asset (local or not), it returns the `asset_id` directly; if it is still only an
// INVENTORY candidate (fanart.tv/TMDB, no asset yet), it creates the asset first (POST select,
// without preferring) and returns the newly created id. Shared by `preferir` and
// `materializar` (journey §2 step 4): both actions may need
// that same step before calling PUT prefer / POST materialize.
async function garantirAssetId(
  entityId: string,
  kind: string,
  item: ArtworkItem,
): Promise<string> {
  if (item.asset_id) {
    return item.asset_id
  }
  const asset = await selectArtwork(entityId, {
    kind: kind as SelectArtworkRequest['kind'],
    url: item.src_url ?? item.url,
    prefer: false,
  })
  return asset.id
}

// LinhaGaleria renders ONE row (kind): label + horizontal scroll of cards in the
// aspect of the type. Each card is a button that opens the lightbox at that index. The
// preferred artwork gains a ring + badge. Extracted so the card markup is not duplicated (jscpd).
function LinhaGaleria({
  linha,
  onAbrir,
}: {
  linha: LinhaArte
  onAbrir: (kind: string, index: number, item: ArtworkItem) => void
}) {
  const t = useT()
  const info = infoKind(linha.kind)
  const ajuste = info.contain ? 'object-contain' : 'object-cover'
  const fundo = info.contain ? 'bg-muted/40' : 'bg-muted'
  const rotulo = rotuloArte(linha.kind, t)
  return (
    <section className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <h3 className="text-sm font-medium">{rotulo}</h3>
        <Badge variant="secondary">{linha.items.length}</Badge>
      </div>
      <ul className="flex gap-3 overflow-x-auto pb-2">
        {linha.items.map((item, i) => (
          <li key={`${item.url}:${i}`} className="shrink-0">
            <button
              type="button"
              onClick={() => onAbrir(linha.kind, i, item)}
              aria-label={
                item.is_principal
                  ? t('media.gallery.viewArtworkPrincipal', {
                      kind: rotulo,
                      n: i + 1,
                    })
                  : t('media.gallery.viewArtwork', { kind: rotulo, n: i + 1 })
              }
              className={`group relative block ${larguraKind(linha.kind)} overflow-hidden rounded-lg ${fundo} ring-1 transition ${
                item.is_principal
                  ? 'ring-2 ring-amber-500'
                  : 'ring-foreground/10 hover:ring-foreground/30'
              }`}
            >
              <ArtworkImage
                src={item.url}
                alt={t('media.gallery.artworkAlt', {
                  kind: rotulo,
                  n: i + 1,
                })}
                className={`${info.aspect} w-full ${ajuste}`}
              />
              {item.is_principal ? (
                <span
                  title={t('media.gallery.principal')}
                  className="absolute left-1.5 top-1.5 flex size-6 items-center justify-center rounded-full bg-amber-500 text-white shadow"
                >
                  <Star className="size-3.5 fill-current" aria-hidden />
                </span>
              ) : null}
              {item.is_local_in_library ? (
                <span
                  title={t('media.gallery.savedInFolder')}
                  className={`absolute top-1.5 flex size-6 items-center justify-center rounded-full bg-emerald-700 text-white shadow ${
                    item.is_principal ? 'left-8' : 'left-1.5'
                  }`}
                >
                  <FolderCheck className="size-3.5" aria-hidden />
                </span>
              ) : null}
            </button>
          </li>
        ))}
      </ul>
    </section>
  )
}

export function ArtGallery({
  entityId,
  type = 'movie',
  stillUrl = null,
  owned = true,
}: {
  entityId: string
  // Type of the entity: it decides the kinds offered in the UPLOAD (episode → only `still`) and
  // whether there is a provider still to show. Default `movie` — compat.
  type?: string
  // EPISODE still coming from the detail: RAW from the provider or local route of the
  // custom one. Read-only "provider" card when the inventory does not have a still yet.
  stillUrl?: string | null
  // Ownership: `EXISTS media_file present` (the same `ResolvedEntity.
  // owned`). Without it there is no destination FOLDER in the real library — "Salvar na
  // pasta"/"Definir como principal" are disabled with a tooltip (journey §2
  // step 7). Default `true`: whoever does not pass it (older screens) keeps the buttons
  // enabled, as before.
  owned?: boolean
}) {
  const qc = useQueryClient()
  const t = useT()
  const { data, isPending, isError, error } = useQuery(
    artworkQueryOptions(entityId),
  )

  // Upload of the owner: kinds according to the type; validation error on the client.
  // The error is stored as a catalog KEY, not as text, so it follows a language change.
  const kindsUpload = kindsParaTipo(type)
  const [kindUpload, setKindUpload] = useState<KindArte>(kindsUpload[0])
  const [erroCliente, setErroCliente] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  // Open lightbox (kind + index) or null. `identidadeAbertaRef` follows the
  // IDENTITY (identidadeItem) of the open item — it survives reorderings of the
  // list (it is not cleared by a render, only by opening/navigating/closing).
  const [aberto, setAberto] = useState<Aberto | null>(null)
  const identidadeAbertaRef = useRef<string | null>(null)

  function abrirLightbox(kind: string, index: number, item: ArtworkItem) {
    identidadeAbertaRef.current = identidadeItem(item)
    setAberto({ kind, index })
  }

  function navegarLightbox(index: number, item: ArtworkItem | undefined) {
    identidadeAbertaRef.current = item ? identidadeItem(item) : null
    setAberto((prev) => (prev ? { kind: prev.kind, index } : prev))
  }

  // marcarIdentidadeEInvalidar re-anchors the tracked identity of the OPEN item on
  // the newly resolved `asset.id` before invalidating the query: the three
  // mutations below may have just MATERIALIZED an inventory candidate
  // (identity so far = src_url) — without this, the reconciler of the useEffect
  // would look for the old src_url in the already reordered/updated list and would not
  // find it anymore (the new item only carries asset_id), closing the lightbox for nothing.
  function marcarIdentidadeEInvalidar(asset: { id: string }) {
    identidadeAbertaRef.current = asset.id
    invalidarAposArte(qc, entityId)
  }

  const enviar = useMutation({
    mutationFn: ({ file, kind }: { file: File; kind: KindArte }) =>
      uploadAsset(entityId, file, kind),
    onSuccess: () => invalidarAposArte(qc, entityId),
  })

  // Set PRINCIPAL (source of truth `is_principal`): an already
  // existing asset → PUT prefer directly (the backend materializes+renames on its own, if
  // needed); INVENTORY candidate (without `asset_id` yet) → it creates the asset
  // first (POST select, without preferring) and only then PUT prefer — a single click
  // of the owner chains the two calls when necessary (journey §2 step 4).
  const preferir = useMutation({
    mutationFn: async ({ kind, item }: { kind: string; item: ArtworkItem }) =>
      preferAsset(entityId, await garantirAssetId(entityId, kind, item)),
    onSuccess: marcarIdentidadeEInvalidar,
  })

  // Save INTO the real library folder — distinct from `baixar`
  // (which only caches internally, without touching the real folder). Same chaining as
  // `preferir` for a candidate still without `asset_id`, but it ends in POST
  // materialize (it does not change which one is the principal).
  const materializar = useMutation({
    mutationFn: async ({ kind, item }: { kind: string; item: ArtworkItem }) =>
      materializeAsset(entityId, await garantirAssetId(entityId, kind, item)),
    onSuccess: marcarIdentidadeEInvalidar,
  })

  // Download (materialize ONLY in the internal cache, without preferring nor touching the real
  // library folder) — only inventory artwork has a `src_url`.
  const baixar = useMutation({
    mutationFn: ({ kind, item }: { kind: string; item: ArtworkItem }) =>
      selectArtwork(entityId, {
        kind: kind as SelectArtworkRequest['kind'],
        url: item.src_url ?? item.url,
        prefer: false,
      }),
    onSuccess: marcarIdentidadeEInvalidar,
  })

  // Remove (owner only): DELETE of the asset.
  const remover = useMutation({
    mutationFn: ({ item }: { kind: string; item: ArtworkItem }) =>
      deleteAsset(entityId, item.asset_id ?? ''),
    onSuccess: () => invalidarAposArte(qc, entityId),
  })

  function onEscolherArquivo(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    e.target.value = ''
    if (!file) {
      return
    }
    if (!(MIMES_ARTE as readonly string[]).includes(file.type)) {
      setErroCliente('media.gallery.unsupportedType')
      return
    }
    setErroCliente(null)
    enviar.mutate({ file, kind: kindUpload })
  }

  const ocupado =
    preferir.isPending ||
    materializar.isPending ||
    baixar.isPending ||
    remover.isPending

  // Error of the lightbox ACTIONS (message from the API envelope) — shown in the footer of the lightbox.
  const erroAcao =
    preferir.error instanceof ApiError
      ? preferir.error.message
      : materializar.error instanceof ApiError
        ? materializar.error.message
        : baixar.error instanceof ApiError
          ? baixar.error.message
          : remover.error instanceof ApiError
            ? remover.error.message
            : preferir.isError ||
                materializar.isError ||
                baixar.isError ||
                remover.isError
              ? t('media.gallery.actionError')
              : null

  // Error of the UPLOAD (client > envelope > generic).
  const erroUpload =
    (erroCliente ? t(erroCliente) : null) ??
    (enviar.error instanceof ApiError
      ? enviar.error.message
      : enviar.isError
        ? t('media.gallery.uploadError')
        : null)

  const linhas = linhasDeArtwork(data)
  // PROVIDER still (episode): read-only card while the inventory does not
  // have a still (neither from the owner nor downloaded). Shown only on the episode, with stillUrl.
  const temStill = linhas.some((l) => l.kind === 'still')
  const mostrarProviderStill = type === 'episode' && !temStill && !!stillUrl

  // Row open in the lightbox (recalculated on every render — it reflects the post-mutation state).
  const linhaAberta = aberto
    ? linhas.find((l) => l.kind === aberto.kind)
    : undefined

  // Re-anchors the lightbox by IDENTITY whenever `data` revalidates
  // (prefer/materialize/download/remove invalidate `['artwork', id]`): it looks for the
  // NEW position of the item with the same identity in `linhaAberta.items` and fixes
  // `aberto.index`; if the item no longer exists (it was removed), it closes the lightbox in
  // stead of showing what was left at that index.
  useEffect(() => {
    if (!aberto) {
      return
    }
    const identidade = identidadeAbertaRef.current
    if (!linhaAberta || identidade === null) {
      setAberto(null)
      return
    }
    const novoIndex = linhaAberta.items.findIndex(
      (item) => identidadeItem(item) === identidade,
    )
    if (novoIndex === -1) {
      identidadeAbertaRef.current = null
      setAberto(null)
      return
    }
    if (novoIndex !== aberto.index) {
      setAberto({ kind: aberto.kind, index: novoIndex })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data])

  const acoes: AcoesLightbox = {
    onPreferir: (item) =>
      aberto && preferir.mutate({ kind: aberto.kind, item }),
    onMaterializar: (item) =>
      aberto && materializar.mutate({ kind: aberto.kind, item }),
    onBaixar: (item) => aberto && baixar.mutate({ kind: aberto.kind, item }),
    onRemover: (item) => aberto && remover.mutate({ kind: aberto.kind, item }),
    ocupado,
    // Without a present media_file there is no destination folder in the real library (journey
    // §2 step 7) — "Salvar na pasta"/"Definir como principal" are disabled
    // with a tooltip; "Baixar pro cache"/"Remover" remain free (they do not touch the
    // real folder).
    podeSalvarNaPasta: owned,
    erro: erroAcao,
  }

  return (
    <section className="mt-6 flex flex-col gap-4">
      <h2 className="font-heading text-lg font-medium">
        {t('media.gallery.title')}
      </h2>

      {/* Upload of the owner: kind selector + file input. */}
      <div className="flex flex-wrap items-end gap-2 rounded-xl bg-card p-4 ring-1 ring-foreground/10">
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-muted-foreground">
            {t('common.label.type')}
          </span>
          <Select
            value={kindUpload}
            onChange={(e) => setKindUpload(e.target.value as KindArte)}
            aria-label={t('media.gallery.typeLabel')}
            className="w-full sm:w-36"
          >
            {kindsUpload.map((k) => (
              <option key={k} value={k}>
                {rotuloArte(k, t)}
              </option>
            ))}
          </Select>
        </label>
        <input
          ref={inputRef}
          type="file"
          accept={ACCEPT}
          aria-label={t('media.gallery.fileLabel')}
          className="hidden"
          onChange={onEscolherArquivo}
        />
        <Button
          type="button"
          onClick={() => inputRef.current?.click()}
          disabled={enviar.isPending}
        >
          <Upload aria-hidden />
          {enviar.isPending
            ? t('media.gallery.uploading')
            : t('media.gallery.upload')}
        </Button>
      </div>

      {erroUpload ? (
        <p role="alert" className="text-sm font-medium text-destructive">
          {erroUpload}
        </p>
      ) : null}

      {isPending ? (
        // Skeleton: a row of placeholders (the gallery loads as a block).
        <div className="flex flex-col gap-2" aria-hidden>
          <div className="h-4 w-24 animate-pulse rounded bg-muted" />
          <div className="flex gap-3">
            {[0, 1, 2, 3].map((i) => (
              <div
                key={i}
                className="aspect-[2/3] w-52 shrink-0 animate-pulse rounded-lg bg-muted"
              />
            ))}
          </div>
        </div>
      ) : isError ? (
        <ErrorState
          title={t('media.gallery.loadError')}
          message={error instanceof Error ? error.message : undefined}
        />
      ) : linhas.length === 0 && !mostrarProviderStill ? (
        <p className="text-sm text-muted-foreground">
          {type === 'episode'
            ? t('media.gallery.emptyEpisode')
            : t('media.gallery.empty')}
        </p>
      ) : (
        <div className="flex flex-col gap-5">
          {/* Provider still (episode): read-only, it does not open the lightbox nor has actions. */}
          {mostrarProviderStill && stillUrl ? (
            <section className="flex flex-col gap-2">
              <div className="flex items-center gap-2">
                <h3 className="text-sm font-medium">
                  {rotuloArte('still', t)}
                </h3>
                <Badge variant="outline">{t('media.gallery.provider')}</Badge>
              </div>
              <div className="w-80 overflow-hidden rounded-lg bg-muted ring-1 ring-foreground/10">
                <ArtworkImage
                  src={arteSrc(stillUrl) ?? stillUrl}
                  alt={t('media.gallery.providerStillAlt')}
                  className="aspect-video w-full object-cover"
                />
              </div>
            </section>
          ) : null}
          {linhas.map((linha) => (
            <LinhaGaleria
              key={linha.kind}
              linha={linha}
              onAbrir={abrirLightbox}
            />
          ))}
        </div>
      )}

      {/* Lightbox: only when a row is open (it disappears on its own if the row empties). */}
      {aberto && linhaAberta ? (
        <ArtLightbox
          kind={aberto.kind}
          items={linhaAberta.items}
          index={Math.min(aberto.index, linhaAberta.items.length - 1)}
          onIndex={(i) => navegarLightbox(i, linhaAberta.items[i])}
          onClose={() => {
            identidadeAbertaRef.current = null
            setAberto(null)
          }}
          acoes={acoes}
        />
      ) : null}
    </section>
  )
}
