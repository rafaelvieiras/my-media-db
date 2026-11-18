import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate, useParams, useSearch } from '@tanstack/react-router'
import { TriangleAlert } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { OwnerOrderEditor } from '@/components/owner-order-editor'
import { SuggestedMatchWarning } from '@/components/suggested-match-warning'
import type { OrderingsInfo } from '@/lib/api'
import {
  artworkQueryOptions,
  entityFieldsQueryOptions,
  entityQueryOptions,
  overrideBlobQueryOptions,
} from '@/lib/queries'
import { ehDraft, rotuloStatus } from '@/lib/entity-badges'
import {
  precisaAtencaoCatalogacao,
  rotuloItemFaltando,
  type CatalogCompleteness,
} from '@/lib/catalog-completeness-badges'
import { useLanguage } from '@/lib/language-context'
import { rotuloIdioma } from '@/lib/languages'
import { temOverrideEditado } from '@/lib/override-fields'
import { ArtGallery } from '@/components/art-gallery'
import { CollectionMembers } from '@/components/collection-members'
import { DiscografiaArtista } from '@/components/discografia-artista'
import { EntityManageMenu } from '@/components/entity-manage-menu'
import { MembrosSomenteLeitura } from '@/components/entity-members'
import {
  EpisodiosAgrupados,
  SeletorOrdenacao,
} from '@/components/episode-ordering'
import { TracklistAlbum } from '@/components/tracklist-album'
import { HistoricoSection } from '@/components/historico-section'
import { MediaInfoTable } from '@/components/media-info-table'
import {
  ErrorState,
  LoadingState,
  VoltarBiblioteca,
} from '@/components/query-states'
import { Migalhas } from '@/components/migalhas'
import { RichDetailView } from '@/components/rich-detail-view'
import { deResolvedEntity } from '@/lib/rich-detail'
import { useT } from '@/lib/i18n'

// Route `/entities/$id`: the detail of the IMPORTED entity. It uses the SAME RichDetailView as
// the external detail (hero, tagline, metadata, genres, keywords, videos, collection,
// similars, links) — the backend serves the rich fields in the ResolvedEntity (parity
// between external and entity). The entity-specific parts come in through SLOTS: the override badges (inherited ×
// edited × fallback language), the override button/editor, the artwork gallery
// and the file table with mediainfo. The reading language comes from the
// LanguageProvider and goes into the `?lang=` (re-fetch on change).

// idiomaFallback returns the effective language of a translatable field when it fell back
// (it reads only `resolved_languages`, ADR-0019): the code that served the value when
// it exists, is not the neutral base (`""`) and differs from the request — otherwise null (no badge).
function idiomaFallback(
  resolved: Record<string, string>,
  campo: string,
  lang: string,
): string | null {
  const efetivo = resolved[campo]
  if (efetivo && efetivo !== '' && efetivo !== lang) {
    return efetivo
  }
  return null
}

// SeloFallback shows "in {language}" when the field fell back to another language.
function SeloFallback({
  resolved,
  campo,
  lang,
}: {
  resolved: Record<string, string>
  campo: string
  lang: string
}) {
  const t = useT()
  const efetivo = idiomaFallback(resolved, campo, lang)
  return efetivo ? (
    <Badge variant="secondary" className="w-fit">
      {t('entity.detail.fallbackLanguage', { language: rotuloIdioma(efetivo) })}
    </Badge>
  ) : null
}

// SeloEditado marks an override of the owner IN THE CURRENT LANGUAGE for the field (read from the raw blob).
function SeloEditado({ ativo }: { ativo: boolean }) {
  const t = useT()
  return ativo ? (
    <Badge variant="default">{t('entity.detail.edited')}</Badge>
  ) : null
}

// SeloPosse shows the AGGREGATED ownership of a season/series: "has 12/13 episodes". Discreet
// (secondary). Null when there is no aggregation (the API only sends
// `ownership` for tv/season; movie/episode/collection use the simple ownership).
function SeloPosse({
  ownership,
}: {
  ownership: { owned_count: number; total_count: number } | null
}) {
  const t = useT()
  if (!ownership) {
    return null
  }
  return (
    <Badge variant="secondary" className="w-fit">
      {t('entity.detail.ownsEpisodes', {
        owned: ownership.owned_count,
        total: ownership.total_count,
      })}
    </Badge>
  )
}

// ItemFaltandoComAcao renders ONE item of `missing`. `media_file` (media file) is
// the only one that the generic button "Download missing information"
// (re-enrichment via provider) provably does NOT solve — it is only solved
// by linking a datasource/running a scan in Libraries, a different screen —
// so it gains a link next to the label. The other keys (field/
// artwork) stay text only, since the generic button already solves them.
function ItemFaltandoComAcao({ chave }: { chave: string }) {
  const t = useT()
  if (chave === 'media_file') {
    return (
      <li>
        <span>{rotuloItemFaltando(chave, t)}</span>
        {' — '}
        <Link
          to="/dashboard/library"
          className="underline underline-offset-2 hover:text-foreground"
        >
          {t('entity.action.seeLibraries')}
        </Link>
      </li>
    )
  }
  return <li>{rotuloItemFaltando(chave, t)}</li>
}

// SecaoCompletudeIncompleta lists, in the interface language, what is still missing for the item
// to meet the CATALOGING completeness ruler — DIFFERENT from ownership
// (`SeloPosse` above). It stays near the top/metadata (not hidden at
// the bottom of the page), always visible (without depending on hover — useful on touch/mobile,
// unlike the card tooltip). It only renders when incomplete; an item that meets the
// ruler (or a type without a ruler, or a draft) shows nothing — no positive confirmation,
// the same principle of "signal only what needs attention".
function SecaoCompletudeIncompleta({ cc }: { cc: CatalogCompleteness }) {
  const t = useT()
  if (!precisaAtencaoCatalogacao(cc)) {
    return null
  }
  return (
    <section
      aria-label={t('entity.detail.incomplete')}
      className="flex flex-col gap-2 rounded-xl bg-amber-500/10 p-4 ring-1 ring-amber-500/30"
    >
      <h2 className="flex items-center gap-1.5 text-sm font-semibold text-amber-600 dark:text-amber-400">
        <TriangleAlert className="size-4" aria-hidden />
        {t('entity.detail.incomplete')}
      </h2>
      <ul className="list-inside list-disc text-sm text-muted-foreground">
        {cc?.missing.map((chave) => (
          <ItemFaltandoComAcao key={chave} chave={chave} />
        ))}
      </ul>
    </section>
  )
}

// SecaoSerie decides the tree view of a SERIES according to the chosen order:
// the selector appears only when there is more than one order; the default order (or absent)
// shows the navigable season cards (the usual view); an alternative order
// shows the sections of regrouped episodes. Switching the order navigates the search param
// (going back to the default REMOVES the param — clean URL, without persistence).
//
// The OWNER order (ADR-0033) is always a grouped view, even when it is the default: its
// seasons are not materialized, and its units may hold several episodes under one number.
function SecaoSerie({
  id,
  orderings,
  ordering,
}: {
  id: string
  orderings: OrderingsInfo | null
  ordering: string | undefined
}) {
  const navigate = useNavigate()
  const t = useT()
  const def = orderings?.default
  const mostrarSeletor = (orderings?.available.length ?? 0) > 1
  // "Alternative" order = chosen and != default. Only it fires the grouped view; the
  // default (or absent) falls into the seasons view — except the owner order, which is
  // always grouped (its seasons are a projection, not entities).
  const efetiva = ordering ?? def
  const alternativa =
    efetiva && (efetiva === 'owner' || efetiva !== def) ? efetiva : null
  const [editorAberto, setEditorAberto] = useState(false)

  const trocar = (novo: string) => {
    navigate({
      to: '/entities/$id',
      params: { id },
      search: def && novo === def ? {} : { ordering: novo },
    })
  }

  const seletor = (
    <div className="flex items-center gap-2">
      {mostrarSeletor && def && orderings ? (
        <SeletorOrdenacao
          value={ordering ?? def}
          options={orderings.available}
          onChange={trocar}
        />
      ) : null}
      <Button
        type="button"
        size="sm"
        variant="outline"
        onClick={() => setEditorAberto(true)}
      >
        {t('entity.edit.ownerOrder.trigger')}
      </Button>
      <OwnerOrderEditor
        id={id}
        base={def ?? 'official'}
        open={editorAberto}
        onOpenChange={setEditorAberto}
      />
    </div>
  )

  if (alternativa) {
    return (
      <EpisodiosAgrupados id={id} ordering={alternativa} seletor={seletor} />
    )
  }
  return (
    <MembrosSomenteLeitura
      id={id}
      titulo={t('entity.detail.seasons')}
      carregando={t('entity.detail.seasonsLoading')}
      erro={t('entity.detail.seasonsError')}
      vazio={t('entity.detail.seasonsEmpty')}
      acao={seletor}
    />
  )
}

export function EntityDetailScreen() {
  const t = useT()
  const { id } = useParams({ from: '/entities/$id' })
  const { ordering } = useSearch({ from: '/entities/$id' })
  const { lang } = useLanguage()
  const { data, isPending, isError, error } = useQuery(
    entityQueryOptions(id, lang),
  )
  // RAW blob of the override + descriptors: they feed the "edited" badges (the badge reads the
  // blob and finds out whether the field is translatable from the descriptors).
  const overrideQuery = useQuery(overrideBlobQueryOptions(id))
  const blob = overrideQuery.data
  const fields = useQuery(entityFieldsQueryOptions).data
  // Artwork inventory: it feeds the ROW of badges of the top card. The
  // SAME query is read by the ArtGallery (child) — TanStack dedupes it into a single round trip.
  const artwork = useQuery(artworkQueryOptions(id)).data

  const voltar = <VoltarBiblioteca />

  if (isPending) {
    return (
      <main className="mx-auto max-w-4xl px-4 py-6">
        {voltar}
        <LoadingState label={t('entity.detail.loading')} />
      </main>
    )
  }
  if (isError) {
    return (
      <main className="mx-auto max-w-4xl px-4 py-6">
        {voltar}
        <ErrorState
          title={t('entity.detail.error')}
          message={error instanceof Error ? error.message : undefined}
        />
      </main>
    )
  }

  // Top of the screen: the "Voltar à biblioteca" and, for the nodes of the series tree
  // (season/episode), the ascending breadcrumb right below. A top-level work
  // has an empty `ancestors` → `Migalhas` does not render and the top keeps only the back link.
  const topo = (
    <div className="flex flex-col gap-2">
      {voltar}
      <Migalhas
        ancestors={data.ancestors}
        atual={{ title: data.title, type: data.type }}
      />
      <SecaoCompletudeIncompleta cc={data.catalog_completeness} />
    </div>
  )

  // `data.owned` (ADR-0027) includes ownership via the TREE for artist/album — only the
  // TRACK links directly to a media_file, so an "owned" artist/album never has a real folder
  // OF ITS OWN (resolverDestino does not know how to choose among the several
  // directories of the tracks). "Salvar informações (NFO)"/"Salvar na pasta" hit a
  // 404 (no destination folder) if enabled with the raw `owned` — which is why
  // they are disabled for these two verticals even with `owned: true`.
  const podeGravarNaPastaReal =
    data.owned && data.type !== 'album' && data.type !== 'artista'

  return (
    <main className="mx-auto max-w-4xl px-4 py-6">
      <RichDetailView
        detail={deResolvedEntity(data)}
        source="tmdb"
        back={topo}
        entityId={id}
        availableArtwork={artwork?.available}
        titleBadges={
          <>
            {ehDraft(data.status) ? (
              <Badge variant="secondary">
                {t('entity.detail.unidentified')}
              </Badge>
            ) : (
              <Badge variant="outline">{rotuloStatus(data.status, t)}</Badge>
            )}
            <SeloEditado
              ativo={temOverrideEditado(blob, 'title', lang, fields)}
            />
            <SeloPosse ownership={data.ownership} />
          </>
        }
        actions={
          <EntityManageMenu
            id={id}
            type={data.type}
            status={data.status}
            catalogCompleteness={data.catalog_completeness}
            titulo={data.title}
            owned={podeGravarNaPastaReal}
            isCollection={data.type === 'collection'}
          />
        }
        descriptionExtra={
          <>
            <div className="flex flex-wrap items-center gap-1.5">
              <SeloFallback
                resolved={data.resolved_languages}
                campo="description"
                lang={lang}
              />
              <SeloEditado
                ativo={temOverrideEditado(blob, 'description', lang, fields)}
              />
            </div>
            {/* "Did you mean this?" — the scanner could not resolve the name on disk, but a
                cheaper variant of it found a candidate. It sits in the hero, under the
                synopsis, because it is the first thing the owner needs to know about the
                item; nothing was applied. */}
            {data.match_suggestion ? (
              <SuggestedMatchWarning
                entityId={id}
                suggestion={data.match_suggestion}
              />
            ) : null}
          </>
        }
        genresExtra={
          <>
            <SeloFallback
              resolved={data.resolved_languages}
              campo="tags"
              lang={lang}
            />
            <SeloEditado
              ativo={temOverrideEditado(blob, 'tags', lang, fields)}
            />
          </>
        }
      >
        {/* Collection: it shows the MEMBERS in place of the Files (a collection has no
            media_file). The artwork gallery still applies (the collection's own artwork).
            "Excluir coleção" migrated to the "Gerenciar" dropdown (0086,
            `actions` above) — a destructive item with confirmation via AlertDialog. */}
        {data.type === 'collection' ? (
          <CollectionMembers id={id} editable />
        ) : null}

        {/* Series tree, read-only: the series lists the SEASONS
            navigable in the default order; a selector (0040) switches to an alternative
            order, which regroups the EPISODES into sections. The season lists the
            numbered EPISODES. It reuses the parent-with-children mold (/members) without the
            add/remove — Phase 1 does not edit the tree by hand. */}
        {data.type === 'tv' ? (
          <SecaoSerie
            id={id}
            orderings={data.orderings ?? null}
            ordering={ordering}
          />
        ) : null}
        {data.type === 'season' ? (
          <MembrosSomenteLeitura
            id={id}
            titulo={t('entity.detail.episodes')}
            carregando={t('entity.detail.episodesLoading')}
            erro={t('entity.detail.episodesError')}
            vazio={t('entity.detail.episodesEmpty')}
            numerar
          />
        ) : null}

        {/* Music vertical (ADR-0027): an album shows the
            tracklist (tracks via entity_relation kind=music_album); artista
            shows the discography (albums via kind=music_artista_principal,
            filtering type=album). The same branch pattern by `data.type` of the
            caller, above. */}
        {data.type === 'album' ? (
          <TracklistAlbum id={id} mbid={data.external_ids.mbid} />
        ) : null}
        {data.type === 'artista' ? <DiscografiaArtista id={id} /> : null}

        <ArtGallery
          entityId={id}
          type={data.type}
          stillUrl={data.still_url ?? null}
          owned={podeGravarNaPastaReal}
        />

        {/* Files only in the leaf nodes that have a media_file: movie and episode. Collection,
            series and season group children (without a folder of their own) — no section. */}
        {data.type === 'movie' || data.type === 'episode' ? (
          <section className="flex flex-col gap-3">
            <h2 className="font-heading text-lg font-medium">
              {t('entity.detail.files')}
            </h2>
            <MediaInfoTable entityId={id} />
          </section>
        ) : null}

        {/* Consumption history: timeline of "assistir" events (webhook
            Jellyfin webhook or manual action of the "Gerenciar" dropdown above). It disappears
            entirely without any event — the same pattern as SimilaresRail. */}
        <HistoricoSection entityId={id} />
      </RichDetailView>
    </main>
  )
}
