import { useQuery } from '@tanstack/react-query'
import { GradeMembros } from '@/components/entity-members'
import { PosterCard } from '@/components/poster-card'
import { useLanguage } from '@/lib/language-context'
import { entityMembersQueryOptions } from '@/lib/queries'

// DiscografiaArtista lists the ALBUMS of an ARTIST: children
// by entity_relation kind=music_artista_principal (ADR-0027) — the SAME
// generic members endpoint (GET /entities/{id}/members,
// `entityMembersQueryOptions`) that already feeds `MembrosSomenteLeitura`
// (seasons/episodes of the series, `entity-members.tsx`), reusing the SAME
// `GradeMembros` (loading/error/empty shell + grid) and the SAME
// `PosterCard` of the general grid — no new card.
//
// FILTER type=album: `music_artista_principal` also links loose TRACKS
// (collaborations whose track artist diverges from the main artist of the
// album, ADR-0027) straight to the artist — the discography shows only the
// `type=album` children, a CLIENT-SIDE filter over the same response.
//
// NOTE: today `kindEOrdemMembros` (internal/server/entities.go) only
// recognizes the `tipoPai` `tv`/`season`; for `artista` it falls into the `default` (kind=
// "collection"), which never matches `music_artista_principal` — this section
// stays EMPTY against real data until the backend gains the generic mapping
// (reported in the implementation summary, not done here on the explicit
// instruction of §Step 1: "endpoint hardcoded → do not rewrite it
// on your own").
export function DiscografiaArtista({ id }: { id: string }) {
  const { lang } = useLanguage()
  const { data, isPending, isError } = useQuery(
    entityMembersQueryOptions(id, lang),
  )
  const albuns = data?.filter((card) => card.type === 'album')

  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-medium">Discografia</h2>
      <GradeMembros
        itens={albuns}
        isPending={isPending}
        isError={isError}
        carregando="Carregando discografia…"
        erro="Não foi possível carregar a discografia"
        vazio="Este artista ainda não tem álbuns catalogados."
        render={(card) => <PosterCard card={card} />}
      />
    </section>
  )
}
