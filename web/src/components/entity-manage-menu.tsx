import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import {
  ChevronDown,
  CloudDownload,
  Eye,
  EyeOff,
  FileText,
  ImageDown,
  Images,
  Link2,
  Pencil,
  RefreshCw,
  SearchCheck,
  Settings,
  Sparkles,
  Tag,
  Trash2,
} from 'lucide-react'
import { ManualMatchPanel } from '@/components/manual-match-panel'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {
  ApiError,
  createHistoricoEvento,
  deleteHistoricoEvento,
  downloadMissingArtwork,
  enrichEntity,
  enrichMissing,
  forceReconcile,
  listHistoricoEventos,
  materializeMissingMedia,
  purgeEntity,
  refreshEntityFiles,
  writeAudioTags,
  writeEntityNfo,
  type AudioTagsWriteResult,
  type DownloadMissingArtworkResult,
  type EnrichMissingResult,
  type ForceReconcileResult,
  type MaterializeMissingMediaResult,
  type RefreshFilesResult,
} from '@/lib/api'
import {
  precisaAtencaoCatalogacao,
  type CatalogCompleteness,
} from '@/lib/catalog-completeness-badges'
import { ehDraft } from '@/lib/entity-badges'
import { KIND_ASSISTIR } from '@/lib/historico'
import { useT } from '@/lib/i18n'
import type { Translate } from '@/lib/i18n/types'

// EntityManageMenu packs the
// actions of the item into a SINGLE "Gerenciar" dropdown — Editar (always) + "Baixar
// informações faltando"/"Baixar arte faltando" (conditional on
// `catalog_completeness`) + "Salvar informações (NFO)" + "Verificar
// Atualizações de Arquivos"/"Baixar Metadados"/"Baixar Artes Faltantes" (ALWAYS
// visible, forced actions that ignore the cache/incrementality) + "Excluir
// coleção" (only `isCollection`, migrated from `entity-detail.tsx::
// ExcluirColecao`) + "Forçar reconciliação" (only track/series IN
// DRAFT, it ignores the once-a-day throttle of the batch reconciliation). The LOGIC of
// each mutation is the SAME as before — only the presentation became menu items
// instead of loose buttons above the hero. No "choose candidate" modal:
// "flagged" only links to the already existing Review queue (the same decision as
// before).

// rotuloKind translates the label of an artwork kind (the human name used in the
// result sentence). The kind itself is data (from the provider): a kind outside the
// catalog echoes raw, the same idiom as `rotuloArte` in rich-detail-view.tsx.
function rotuloKind(kind: string, t: Translate): string {
  const chave = `entity.artwork.${kind}`
  const rotulo = t(chave)
  return rotulo === chave ? kind : rotulo
}

// montarListaBaixados builds the sentence "<kinds> downloaded." from the downloaded
// kinds (label via `rotuloKind`, the same source of truth as the
// gallery/lightbox), with a comma between the items and the conjunction of the
// locale before the last one.
function montarListaBaixados(kinds: string[], t: Translate): string {
  const rotulos = kinds.map((kind) => rotuloKind(kind, t).toLowerCase())
  const separador = t('entity.edit.manage.listAnd')
  const lista =
    rotulos.length <= 1
      ? (rotulos[0] ?? '')
      : `${rotulos.slice(0, -1).join(', ')} ${separador} ${rotulos[rotulos.length - 1]}`
  return t('entity.edit.manage.downloaded', {
    list: `${lista.charAt(0).toUpperCase()}${lista.slice(1)}`,
  })
}

// mensagemEnriquecimento resolves the outcome of POST .../enrich-missing (§4):
// applied (with the method) × flagged (link to /review) × nothing found
// (neutral) — none of the three is an error; a network error/500 is handled separately.
function mensagemEnriquecimento(resultado: EnrichMissingResult, t: Translate) {
  if (resultado.outcome === 'applied') {
    return (
      <span role="status" className="text-sm text-muted-foreground">
        {resultado.method === 'local'
          ? t('entity.edit.manage.appliedLocal')
          : t('entity.edit.manage.appliedExternal')}
      </span>
    )
  }
  if (resultado.outcome === 'flagged') {
    return (
      <span role="status" className="text-sm text-muted-foreground">
        {t('entity.edit.manage.flaggedBefore')}{' '}
        <Link to="/dashboard/review">{t('nav.dashboard.review')}</Link>
        {t('entity.edit.manage.flaggedAfter')}
      </span>
    )
  }
  return (
    <span role="status" className="text-sm text-muted-foreground">
      {t('entity.edit.manage.noneFound')}
    </span>
  )
}

// mensagemVerificarArquivos resolves the outcome of POST .../refresh-files:
// a loose, informative format (the contract itself is not strict) — files
// rechecked, local assets updated and local playback fields
// filled in from the .nfo, when there is one.
function mensagemVerificarArquivos(
  resultado: RefreshFilesResult,
  t: Translate,
) {
  const partes = [
    t('entity.edit.manage.filesRechecked', {
      n: resultado.files_rechecked,
    }),
  ]
  if (resultado.assets_found > 0) {
    partes.push(
      t('entity.edit.manage.assetsFound', { n: resultado.assets_found }),
    )
  }
  if (resultado.playback_fields_filled.length > 0) {
    partes.push(
      t('entity.edit.manage.playbackFields', {
        fields: resultado.playback_fields_filled.join(', '),
      }),
    )
  }
  return (
    <span role="status" className="text-sm text-muted-foreground">
      {partes.join(' — ')}.
    </span>
  )
}

// mensagemArtesFaltantes resolves the outcome of POST .../materialize-missing-media:
// it reuses `montarListaBaixados` (the same sentence as the gallery/lightbox) for
// the downloaded kinds, plus the .nfo when written in this call.
function mensagemArtesFaltantes(
  resultado: MaterializeMissingMediaResult,
  t: Translate,
) {
  if (resultado.artes_baixadas.length > 0) {
    return (
      <span role="status" className="text-sm text-muted-foreground">
        {montarListaBaixados(resultado.artes_baixadas, t)}
        {resultado.nfo_gravado
          ? ` ${t('entity.edit.manage.nfoAlsoWritten')}`
          : ''}
      </span>
    )
  }
  if (resultado.nfo_gravado) {
    return (
      <span role="status" className="text-sm text-muted-foreground">
        {t('entity.edit.manage.noNewArtworkButNfo')}
      </span>
    )
  }
  return (
    <span role="status" className="text-sm text-muted-foreground">
      {t('entity.edit.manage.noNewArtwork')}
    </span>
  )
}

// mensagemArte resolves the outcome of POST .../download-missing-artwork (§4):
// it downloaded at least one kind × no option available — idempotent (nothing
// missing also falls into "no option", a silent no-op does not change the UI).
function mensagemArte(resultado: DownloadMissingArtworkResult, t: Translate) {
  if (resultado.downloaded.length > 0) {
    return (
      <span role="status" className="text-sm text-muted-foreground">
        {montarListaBaixados(resultado.downloaded, t)}
      </span>
    )
  }
  if (resultado.unavailable.length > 0) {
    return (
      <span role="status" className="text-sm text-muted-foreground">
        {t('entity.edit.manage.noArtworkAvailable')}
      </span>
    )
  }
  return null
}

// mensagemGravarTag resolves the outcome of POST .../audio-tags/write:
// a loose, informative format (the contract is not strict) — how many tracks
// had their tag rewritten successfully, plus the failures when an album cascade
// has PARTIAL success (one failing track does not stop the others).
function mensagemGravarTag(resultado: AudioTagsWriteResult, t: Translate) {
  const partes = [
    t('entity.edit.manage.tracksRewritten', { n: resultado.faixas_regravadas }),
  ]
  if (resultado.falhas.length > 0) {
    partes.push(
      t('entity.edit.manage.withErrors', { n: resultado.falhas.length }),
    )
  }
  return (
    <span role="status" className="text-sm text-muted-foreground">
      {partes.join(' — ')}.
    </span>
  )
}

// mensagemReconciliar resolves the outcome of POST .../force-reconcile:
// it re-linked against the parent × it did not re-link this time (no candidate, or the type is not
// applicable) — neither of the two is an error; a network error/500 is handled separately.
function mensagemReconciliar(resultado: ForceReconcileResult, t: Translate) {
  if (resultado.linked) {
    return (
      <span role="status" className="text-sm text-muted-foreground">
        {t('entity.edit.manage.reconciled')}
      </span>
    )
  }
  return (
    <span role="status" className="text-sm text-muted-foreground">
      {t('entity.edit.manage.reconcileFailed')}
    </span>
  )
}

// The tooltip of "Save information (NFO)" explains why it is disabled when the
// entity has no present media_file — the same condition used by the art gallery for
// "save to the folder"/"set as main".

export function EntityManageMenu({
  id,
  type,
  status,
  catalogCompleteness,
  titulo = '',
  owned = true,
  isCollection = false,
}: {
  id: string
  // `entity.type`/`entity.status`: they control the visibility of
  // "Forçar reconciliação" — only a track/series IN DRAFT (`ehDraft`) makes
  // sense to re-link against an already identified parent.
  type: string
  status: string
  catalogCompleteness: CatalogCompleteness
  // Current title, used only to PREFILL the manual match search: the owner opens the
  // panel already seeing the wrong identity and edits it. Optional, so whoever does not
  // pass it keeps working (empty search).
  titulo?: string
  // Ownership (`ResolvedEntity.owned`, `EXISTS media_file present`): without it there is no
  // destination folder in the real library for the .nfo. Default
  // `true` — compatible with whoever does not pass the prop.
  owned?: boolean
  // Only a collection gains the destructive item "Excluir coleção" — movie/
  // series/season/episode do not have that action (purge is only for collections today).
  isCollection?: boolean
}) {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const t = useT()
  // State of the confirmation dialog of "Excluir coleção": a dropdown item
  // closes the menu when selected (Radix, correct for accessibility) — the
  // 2-click confirmation that existed INLINE inside the same area no longer fits
  // there; it opens this separate modal instead of mutating directly.
  const [alertaAberto, setAlertaAberto] = useState(false)
  // State of the confirmation dialog of "Gravar metadados no arquivo":
  // same 2-step pattern as the "Excluir coleção" above —
  // a destructive/irreversible action on the original file, it never fires
  // straight from the click on the dropdown item.
  const [alertaGravarTagAberto, setAlertaGravarTagAberto] = useState(false)

  // Invalidation shared by the new buttons (§4): the screen (entity),
  // the listing (entities) and the gallery (artwork) reflect the result without F5.
  const invalidar = () => {
    queryClient.invalidateQueries({ queryKey: ['entity', id] })
    queryClient.invalidateQueries({ queryKey: ['entities'] })
    queryClient.invalidateQueries({ queryKey: ['artwork', id] })
  }

  const enriquecer = useMutation({
    mutationFn: () => enrichMissing(id),
    onSuccess: invalidar,
  })
  const baixarArte = useMutation({
    mutationFn: () => downloadMissingArtwork(id),
    onSuccess: invalidar,
  })
  // "Forçar reconciliação": it fires the SAME parent-child reconciliation
  // of the batch job, but for THIS item and ignoring the once-a-day
  // throttle. The entity changes `status` when re-linked — `invalidar()` reflects it.
  const forcarReconciliar = useMutation({
    mutationFn: () => forceReconcile(id),
    onSuccess: invalidar,
  })
  // Save informações (NFO): idempotent — it always overwrites. It does not
  // need invalidating `artwork`/`entities` (it touches neither the artwork nor the fields that the
  // grid shows), but it invalidates the entity: `nfo_path`/`nfo_written_at` change.
  const salvarNfo = useMutation({
    mutationFn: () => writeEntityNfo(id),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ['entity', id] }),
  })
  // "Gravar metadados no arquivo": it rewrites the audio tag
  // (ID3v2/Vorbis comment) of the file(s) with the ALREADY RESOLVED metadata —
  // an alternative to the music NFO (which does not exist functionally, see
  // §1). It only fires after confirmation in the AlertDialog
  // (`alertaGravarTagAberto`). On success it closes the dialog and invalidates the
  // entity (the write also synchronizes `media_file.hash`/`mtime`/`size` in the
  // backend); the summary (rewritten tracks/failures) appears inline, the same
  // pattern as the ALWAYS VISIBLE actions.
  const gravarTag = useMutation({
    mutationFn: () => writeAudioTags(id),
    onSuccess: () => {
      invalidar()
      setAlertaGravarTagAberto(false)
    },
  })
  // Excluir coleção (migrated from `entity-detail.tsx::ExcluirColecao`):
  // DELETE /entities/{id} — purge with CASCADE of the relations, the member
  // movies are NOT deleted. On success, it invalidates the list and goes back to the
  // library (the usual behavior).
  const excluir = useMutation({
    mutationFn: () => purgeEntity(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['entities'] })
      navigate({ to: '/' })
    },
  })

  // The 3 actions below are ALWAYS VISIBLE — unlike
  // `enriquecer`/`baixarArte` above, they do not depend on `catalog_completeness`. Each
  // one reuses `invalidar()`: since `entityFilesQueryOptions` (queries.ts) uses the
  // key `['entity', id, 'files']`, it already falls under the `['entity', id]` prefix
  // invalidated below — without needing an extra key.
  //
  // "Verificar Atualizações de Arquivos": forced re-hash + new local asset +
  // local playback fields from the .nfo, restricted to this entity.
  const verificarArquivos = useMutation({
    mutationFn: () => refreshEntityFiles(id),
    onSuccess: invalidar,
  })
  // "Baixar Metadados": the usual endpoint (`enrich`), but with
  // `force: true` to skip the 24h cache of the external detail.
  const baixarMetadados = useMutation({
    mutationFn: () => enrichEntity(id, { force: true }),
    onSuccess: invalidar,
  })
  // "Baixar Artes Faltantes": a new query to the ArteProvider(s) + materialization
  // of whatever is missing into the real folder — the same effect as the `midias_faltantes` job,
  // on demand and only for this entity.
  const baixarArtesFaltantes = useMutation({
    mutationFn: () => materializeMissingMedia(id),
    onSuccess: invalidar,
  })

  // invalidarHistorico derives from the prefix `['historico', id]` (queries.ts) — the
  // "Assistido" badge of the hero (same key) and the "Histórico" section reflect it without a
  // manual reload, the same prefix-invalidation convention as the rest of the menu.
  const invalidarHistorico = () =>
    queryClient.invalidateQueries({ queryKey: ['historico', id] })
  // "Marcar como assistido"/"Marcar como não assistido": a manual
  // action for an item without Jellyfin (or with retroactive history). Both items of the
  // menu stay ALWAYS visible (they do not depend on knowing the current state — a UX
  // decision that avoids a query of its own just to label the item; the query of the badge/
  // section already covers the reading, this menu only WRITES). "Assistido" creates the
  // `completed` event; "não assistido" deletes the MOST RECENT `kind=assistir` event
  // (the simplest interpretation — without paginating/choosing which one to delete).
  const marcarAssistido = useMutation({
    mutationFn: () =>
      createHistoricoEvento(id, { kind: KIND_ASSISTIR, status: 'completed' }),
    onSuccess: invalidarHistorico,
  })
  const desmarcarAssistido = useMutation({
    mutationFn: async () => {
      const lista = await listHistoricoEventos(id, KIND_ASSISTIR)
      const maisRecente = lista.eventos[0]
      if (maisRecente) {
        await deleteHistoricoEvento(id, maisRecente.id)
      }
    },
    onSuccess: invalidarHistorico,
  })

  const mostrarInformacoes = precisaAtencaoCatalogacao(catalogCompleteness)
  const mostrarArte = !!catalogCompleteness?.missing.some((chave) =>
    chave.startsWith('artwork:'),
  )
  const mostrarReconciliar =
    (type === 'faixa' || type === 'tv') && ehDraft(status)
  // "Gravar metadados no arquivo": it does NOT depend on
  // `podeGravarNaPastaReal`/ownership of a real folder (that is about the NFO) — only on the
  // `type`, since an album cascades to the owned tracks in the backend.
  const mostrarGravarTag = type === 'faixa' || type === 'album'

  const erroEnriquecer =
    enriquecer.error instanceof ApiError
      ? enriquecer.error.message
      : enriquecer.isError
        ? t('entity.edit.manage.errors.enrich')
        : null
  const erroArte =
    baixarArte.error instanceof ApiError
      ? baixarArte.error.message
      : baixarArte.isError
        ? t('entity.edit.manage.errors.downloadArtwork')
        : null
  const erroReconciliar =
    forcarReconciliar.error instanceof ApiError
      ? forcarReconciliar.error.message
      : forcarReconciliar.isError
        ? t('entity.edit.manage.errors.forceReconcile')
        : null
  const erroNfo =
    salvarNfo.error instanceof ApiError
      ? salvarNfo.error.message
      : salvarNfo.isError
        ? t('entity.edit.manage.errors.saveNfo')
        : null
  const erroGravarTag =
    gravarTag.error instanceof ApiError
      ? gravarTag.error.message
      : gravarTag.isError
        ? t('entity.edit.manage.errors.writeTags')
        : null
  // Same error logic as the original `ExcluirColecao` (without a generic fallback
  // for a non-ApiError error) — migrated as is.
  const erroExcluir =
    excluir.error instanceof ApiError ? excluir.error.message : null
  const erroVerificarArquivos =
    verificarArquivos.error instanceof ApiError
      ? verificarArquivos.error.message
      : verificarArquivos.isError
        ? t('entity.edit.manage.errors.refreshFiles')
        : null
  const erroBaixarMetadados =
    baixarMetadados.error instanceof ApiError
      ? baixarMetadados.error.message
      : baixarMetadados.isError
        ? t('entity.edit.manage.errors.downloadMetadata')
        : null
  const erroBaixarArtesFaltantes =
    baixarArtesFaltantes.error instanceof ApiError
      ? baixarArtesFaltantes.error.message
      : baixarArtesFaltantes.isError
        ? t('entity.edit.manage.errors.downloadAllArtwork')
        : null
  const erroAssistido =
    marcarAssistido.error instanceof ApiError
      ? marcarAssistido.error.message
      : desmarcarAssistido.error instanceof ApiError
        ? desmarcarAssistido.error.message
        : marcarAssistido.isError || desmarcarAssistido.isError
          ? t('entity.edit.manage.errors.history')
          : null
  const atualizandoAssistido =
    marcarAssistido.isPending || desmarcarAssistido.isPending
  const [matchManualAberto, setMatchManualAberto] = useState(false)

  return (
    <div className="flex flex-col items-end gap-1.5">
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="secondary" size="sm">
            <Settings aria-hidden />
            {t('entity.edit.manage.trigger')}
            <ChevronDown aria-hidden />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem asChild>
            <Link to="/entities/$id/edit" params={{ id }}>
              <Pencil aria-hidden />
              {t('common.action.edit')}
            </Link>
          </DropdownMenuItem>
          {/* Manual match: the ONLY way to fix an identity the auto-match got WRONG.
              A wrong match above the threshold leaves the entity `cataloged`, so it
              never reaches the review queue -- without this action the owner has no
              place to say "it is this other one". */}
          <DropdownMenuItem onSelect={() => setMatchManualAberto(true)}>
            <SearchCheck aria-hidden />
            {t('entity.edit.manage.manualMatch')}
          </DropdownMenuItem>
          {mostrarInformacoes ? (
            <DropdownMenuItem
              disabled={enriquecer.isPending}
              onSelect={() => enriquecer.mutate()}
            >
              <Sparkles aria-hidden />
              {enriquecer.isPending
                ? t('entity.edit.manage.searching')
                : t('entity.edit.manage.enrichMissing')}
            </DropdownMenuItem>
          ) : null}
          {mostrarArte ? (
            <DropdownMenuItem
              disabled={baixarArte.isPending}
              onSelect={() => baixarArte.mutate()}
            >
              <ImageDown aria-hidden />
              {baixarArte.isPending
                ? t('entity.edit.manage.downloading')
                : t('entity.edit.manage.downloadMissingArtwork')}
            </DropdownMenuItem>
          ) : null}
          {mostrarReconciliar ? (
            <DropdownMenuItem
              disabled={forcarReconciliar.isPending}
              onSelect={() => forcarReconciliar.mutate()}
            >
              <Link2 aria-hidden />
              {forcarReconciliar.isPending
                ? t('entity.edit.manage.reconciling')
                : t('entity.edit.manage.forceReconcile')}
            </DropdownMenuItem>
          ) : null}
          <DropdownMenuItem
            disabled={salvarNfo.isPending || !owned}
            title={owned ? undefined : t('entity.edit.manage.noFolderTooltip')}
            onSelect={() => salvarNfo.mutate()}
          >
            <FileText aria-hidden />
            {salvarNfo.isPending
              ? t('entity.edit.manage.writing')
              : t('entity.edit.manage.saveNfo')}
          </DropdownMenuItem>
          {mostrarGravarTag ? (
            <DropdownMenuItem onSelect={() => setAlertaGravarTagAberto(true)}>
              <Tag aria-hidden />
              {t('entity.edit.manage.writeTags')}
            </DropdownMenuItem>
          ) : null}
          {/* 3 ALWAYS VISIBLE actions — they do not depend on
              `catalog_completeness`, diferente dos 2 itens condicionais acima. */}
          <DropdownMenuSeparator />
          {/* "Marcar como assistido"/"não assistido": also
              always visible — it creates/deletes the history event directly, without
              having to know the current state (the reading is already done by the badge/section
              of the detail). */}
          <DropdownMenuItem
            disabled={atualizandoAssistido}
            onSelect={() => marcarAssistido.mutate()}
          >
            <Eye aria-hidden />
            {marcarAssistido.isPending
              ? t('entity.edit.manage.marking')
              : t('entity.edit.manage.markWatched')}
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={atualizandoAssistido}
            onSelect={() => desmarcarAssistido.mutate()}
          >
            <EyeOff aria-hidden />
            {desmarcarAssistido.isPending
              ? t('entity.edit.manage.unmarking')
              : t('entity.edit.manage.markNotWatched')}
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={verificarArquivos.isPending}
            onSelect={() => verificarArquivos.mutate()}
          >
            <RefreshCw aria-hidden />
            {verificarArquivos.isPending
              ? t('entity.edit.manage.checking')
              : t('entity.edit.manage.refreshFiles')}
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={baixarMetadados.isPending}
            onSelect={() => baixarMetadados.mutate()}
          >
            <CloudDownload aria-hidden />
            {baixarMetadados.isPending
              ? t('entity.edit.manage.downloading')
              : t('entity.edit.manage.downloadMetadata')}
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={baixarArtesFaltantes.isPending}
            onSelect={() => baixarArtesFaltantes.mutate()}
          >
            <Images aria-hidden />
            {baixarArtesFaltantes.isPending
              ? t('entity.edit.manage.downloading')
              : t('entity.edit.manage.downloadAllMissingArtwork')}
          </DropdownMenuItem>
          {isCollection ? (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                variant="destructive"
                onSelect={() => setAlertaAberto(true)}
              >
                <Trash2 aria-hidden />
                {t('entity.edit.manage.deleteCollection')}
              </DropdownMenuItem>
            </>
          ) : null}
        </DropdownMenuContent>
      </DropdownMenu>

      {/* The label "Buscando…"/"Baixando…"/"Gravando…" lives in the menu item ITSELF
          (row of the trigger above) — but the item leaves the DOM as soon as the
          dropdown closes (Radix closes on select, the default behavior kept
          on purpose, §"Conventions"). Without an echo down here, the
          click would look like it did nothing until the mutation resolves — which is why
          the PENDING status also appears as inline status, next to the
          result/error (the same text as the item, only always visible). */}
      {enriquecer.isPending ? (
        <span role="status" className="text-sm text-muted-foreground">
          {t('entity.edit.manage.searching')}
        </span>
      ) : erroEnriquecer ? (
        <span role="alert" className="text-sm font-medium text-destructive">
          {erroEnriquecer}
        </span>
      ) : enriquecer.isSuccess ? (
        mensagemEnriquecimento(enriquecer.data, t)
      ) : null}
      {baixarArte.isPending ? (
        <span role="status" className="text-sm text-muted-foreground">
          {t('entity.edit.manage.downloading')}
        </span>
      ) : erroArte ? (
        <span role="alert" className="text-sm font-medium text-destructive">
          {erroArte}
        </span>
      ) : baixarArte.isSuccess ? (
        mensagemArte(baixarArte.data, t)
      ) : null}
      {forcarReconciliar.isPending ? (
        <span role="status" className="text-sm text-muted-foreground">
          {t('entity.edit.manage.reconciling')}
        </span>
      ) : erroReconciliar ? (
        <span role="alert" className="text-sm font-medium text-destructive">
          {erroReconciliar}
        </span>
      ) : forcarReconciliar.isSuccess ? (
        mensagemReconciliar(forcarReconciliar.data, t)
      ) : null}
      {salvarNfo.isPending ? (
        <span role="status" className="text-sm text-muted-foreground">
          {t('entity.edit.manage.writing')}
        </span>
      ) : erroNfo ? (
        <span role="alert" className="text-sm font-medium text-destructive">
          {erroNfo}
        </span>
      ) : salvarNfo.isSuccess ? (
        <span role="status" className="text-sm text-muted-foreground">
          {t('entity.edit.manage.nfoWrittenAt', {
            path: salvarNfo.data.nfo_path,
          })}
        </span>
      ) : null}
      {gravarTag.isPending ? (
        <span role="status" className="text-sm text-muted-foreground">
          {t('entity.edit.manage.writing')}
        </span>
      ) : gravarTag.isSuccess ? (
        mensagemGravarTag(gravarTag.data, t)
      ) : null}
      {marcarAssistido.isPending ? (
        <span role="status" className="text-sm text-muted-foreground">
          {t('entity.edit.manage.marking')}
        </span>
      ) : desmarcarAssistido.isPending ? (
        <span role="status" className="text-sm text-muted-foreground">
          {t('entity.edit.manage.unmarking')}
        </span>
      ) : erroAssistido ? (
        <span role="alert" className="text-sm font-medium text-destructive">
          {erroAssistido}
        </span>
      ) : marcarAssistido.isSuccess ? (
        <span role="status" className="text-sm text-muted-foreground">
          {t('entity.edit.manage.markedWatched')}
        </span>
      ) : desmarcarAssistido.isSuccess ? (
        <span role="status" className="text-sm text-muted-foreground">
          {t('entity.edit.manage.markedNotWatched')}
        </span>
      ) : null}
      {verificarArquivos.isPending ? (
        <span role="status" className="text-sm text-muted-foreground">
          {t('entity.edit.manage.checking')}
        </span>
      ) : erroVerificarArquivos ? (
        <span role="alert" className="text-sm font-medium text-destructive">
          {erroVerificarArquivos}
        </span>
      ) : verificarArquivos.isSuccess ? (
        mensagemVerificarArquivos(verificarArquivos.data, t)
      ) : null}
      {baixarMetadados.isPending ? (
        <span role="status" className="text-sm text-muted-foreground">
          {t('entity.edit.manage.downloading')}
        </span>
      ) : erroBaixarMetadados ? (
        <span role="alert" className="text-sm font-medium text-destructive">
          {erroBaixarMetadados}
        </span>
      ) : baixarMetadados.isSuccess ? (
        <span role="status" className="text-sm text-muted-foreground">
          {t('entity.edit.manage.metadataUpdated')}
        </span>
      ) : null}
      {baixarArtesFaltantes.isPending ? (
        <span role="status" className="text-sm text-muted-foreground">
          {t('entity.edit.manage.downloading')}
        </span>
      ) : erroBaixarArtesFaltantes ? (
        <span role="alert" className="text-sm font-medium text-destructive">
          {erroBaixarArtesFaltantes}
        </span>
      ) : baixarArtesFaltantes.isSuccess ? (
        mensagemArtesFaltantes(baixarArtesFaltantes.data, t)
      ) : null}

      {/* Manual match: applies to ANY entity -- a wrong identity is not a
          collection-only problem. */}
      <Sheet open={matchManualAberto} onOpenChange={setMatchManualAberto}>
        <SheetContent
          side="right"
          className="w-full gap-4 overflow-y-auto sm:max-w-md"
        >
          <SheetHeader>
            <SheetTitle>{t('entity.edit.manage.manualMatch')}</SheetTitle>
            <SheetDescription>
              {t('entity.edit.manage.manualMatchDescription')}
            </SheetDescription>
          </SheetHeader>
          <ManualMatchPanel
            entityId={id}
            initialQuery={titulo}
            onApplied={() => setMatchManualAberto(false)}
          />
        </SheetContent>
      </Sheet>

      {isCollection ? (
        <AlertDialog open={alertaAberto} onOpenChange={setAlertaAberto}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>
                {t('entity.edit.manage.deleteCollectionTitle')}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {t('entity.edit.manage.deleteCollectionDescription')}
              </AlertDialogDescription>
            </AlertDialogHeader>
            {erroExcluir ? (
              <span
                role="alert"
                className="text-sm font-medium text-destructive"
              >
                {erroExcluir}
              </span>
            ) : null}
            <AlertDialogFooter>
              <AlertDialogCancel disabled={excluir.isPending}>
                {t('common.action.cancel')}
              </AlertDialogCancel>
              <AlertDialogAction
                disabled={excluir.isPending}
                onClick={(e) => {
                  // preventDefault: the automatic closing of the AlertDialogAction
                  // would close the modal BEFORE the mutation resolves — we control the
                  // `open` manually (it stays open on error, showing the
                  // message; on success the `navigate` unmounts the whole screen).
                  e.preventDefault()
                  excluir.mutate()
                }}
              >
                {excluir.isPending
                  ? t('entity.edit.manage.deleting')
                  : t('entity.edit.manage.deleteCollection')}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      ) : null}

      {mostrarGravarTag ? (
        <AlertDialog
          open={alertaGravarTagAberto}
          onOpenChange={setAlertaGravarTagAberto}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>
                {t('entity.edit.manage.writeTagsTitle')}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {type === 'album'
                  ? t('entity.edit.manage.writeTagsAlbumDescription')
                  : t('entity.edit.manage.writeTagsTrackDescription')}
              </AlertDialogDescription>
            </AlertDialogHeader>
            {erroGravarTag ? (
              <span
                role="alert"
                className="text-sm font-medium text-destructive"
              >
                {erroGravarTag}
              </span>
            ) : null}
            <AlertDialogFooter>
              <AlertDialogCancel disabled={gravarTag.isPending}>
                {t('common.action.cancel')}
              </AlertDialogCancel>
              <AlertDialogAction
                disabled={gravarTag.isPending}
                onClick={(e) => {
                  // preventDefault: same reason as the "Excluir coleção" above —
                  // we control `open` manually (it stays open on error,
                  // showing the message; it closes only on success).
                  e.preventDefault()
                  gravarTag.mutate()
                }}
              >
                {gravarTag.isPending
                  ? t('entity.edit.manage.writing')
                  : t('entity.edit.manage.writeTagsAction')}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      ) : null}
    </div>
  )
}
