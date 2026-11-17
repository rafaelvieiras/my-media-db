import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ListMusic, Music } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { ErrorState, LoadingState } from '@/components/query-states'
import { useLanguage } from '@/lib/language-context'
import { useT } from '@/lib/i18n'
import { entityMembersQueryOptions } from '@/lib/queries'
import type { EntityCard } from '@/lib/api'

// Fixed source of the music vertical: today only MusicBrainz models
// artist/album/track — the same hardcode that the backend uses in provider.Nome().
const SOURCE_MUSICBRAINZ = 'musicbrainz'

// TracklistAlbum lists the TRACKS of an ALBUM: children by
// entity_relation kind=music_album (ADR-0027, sort_index=track_number) — the
// SAME generic members endpoint (GET /entities/{id}/members,
// `entityMembersQueryOptions`) that already feeds `MembrosSomenteLeitura`
// (series seasons/season episodes, `entity-members.tsx`). Each
// track is a LINK to its own detail (journey §3).
//
// ORDERING (backend gap, see the implementation note
// below): `EntityCard` does not carry `track_number`/`disc_number` — they live
// in `extension_data` (namespace core.music@1) of the track, not exposed by the
// card. The list uses the ARRIVAL ORDER of the array as the track position, which
// reflects `sort_index` (`ORDER BY sort_index` of `ListarRelacoesFilhas`,
// internal/db/queries/entity_relation.sql — and `sort_index` = `track_number`
// for the `music_album` kind, internal/enrich/musica.go): correct for a
// single-disc album. A multi-disc album does not group by disc nor reorder by it
// (the data is absent from the card) — see the gap reported in the notes.
//
// NOTE: today `kindEOrdemMembros` (internal/server/entities.go) only
// recognizes the `tipoPai` `tv`/`season`; for `album` it falls into the `default` (kind=
// "collection"), which never matches `music_album` — this section stays EMPTY
// against real data until the backend gains the generic mapping (reported in
// the implementation summary, not done here on the explicit
// instruction of §Step 1: "endpoint hardcoded → do not rewrite it on your own".
export function TracklistAlbum({ id, mbid }: { id: string; mbid?: string }) {
  const { lang } = useLanguage()
  const t = useT()
  const { data, isPending, isError } = useQuery(
    entityMembersQueryOptions(id, lang),
  )
  const faixas = data?.filter((card) => card.type === 'faixa')

  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="font-heading text-lg font-medium">
          {t('common.entityTypePlural.faixa')}
        </h2>
        {/* "Ver faixas faltando" (ADR-0029): same UX as
            external item not owned that movie/series/collection already use — it only
            appears when the album has an mbid (MusicBrainz), the only source that
            models the selective search of an album today. */}
        {mbid ? (
          <Button asChild variant="outline" size="sm">
            <Link
              to="/external/$source/$type/$id"
              params={{ source: SOURCE_MUSICBRAINZ, type: 'album', id: mbid }}
            >
              <ListMusic aria-hidden />
              {t('media.tracks.seeMissing')}
            </Link>
          </Button>
        ) : null}
      </div>
      {isPending ? (
        <LoadingState label={t('media.tracks.loading')} />
      ) : isError ? (
        <ErrorState title={t('media.tracks.loadError')} />
      ) : !faixas || faixas.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          {t('media.tracks.empty')}
        </p>
      ) : (
        <ol className="flex flex-col divide-y divide-foreground/10 rounded-xl ring-1 ring-foreground/10">
          {faixas.map((card, indice) => (
            <li key={card.id}>
              <FaixaLinha posicao={indice + 1} card={card} />
            </li>
          ))}
        </ol>
      )}
    </section>
  )
}

// FaixaLinha is ONE row of the tracklist: position (ordinal in the list, see the ordering
// note above) + icon + title, linking to the detail of the track.
function FaixaLinha({ posicao, card }: { posicao: number; card: EntityCard }) {
  return (
    <Link
      to="/entities/$id"
      params={{ id: card.id }}
      className="group flex items-center gap-3 px-3 py-2.5 transition hover:bg-foreground/5"
    >
      <span className="w-6 shrink-0 text-right text-sm tabular-nums text-muted-foreground">
        {posicao}
      </span>
      <Music className="size-4 shrink-0 text-muted-foreground" aria-hidden />
      <span className="line-clamp-1 flex-1 text-sm font-medium group-hover:underline">
        {card.title}
      </span>
    </Link>
  )
}
