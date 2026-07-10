import { useQuery } from '@tanstack/react-query'
import { PosterCard } from '@/components/poster-card'
import { ErrorState, LoadingState } from '@/components/query-states'
import { useLanguage } from '@/lib/language-context'
import { entityMembersQueryOptions } from '@/lib/queries'

// CollectionMembers lista os ITENS de uma coleção (0025): as entities ligadas por
// entity_relation (kind='collection'), servidas pelo backend como cards internos
// (EntityCard) ordenados por ano. Reusa o `PosterCard` da biblioteca — cada membro
// navega para `/entities/{id}` (destino INTERNO, não /external). Usado pela tela da
// entity quando `type==='collection'`, no lugar de similares. Trata carregando/erro/
// vazio. O idioma ativo entra na busca (re-busca ao trocar).
export function CollectionMembers({ id }: { id: string }) {
  const { lang } = useLanguage()
  const { data, isPending, isError } = useQuery(
    entityMembersQueryOptions(id, lang),
  )

  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-medium">Itens da coleção</h2>
      {isPending ? (
        <LoadingState label="Carregando itens da coleção…" />
      ) : isError ? (
        <ErrorState title="Não foi possível carregar os itens da coleção" />
      ) : data.length > 0 ? (
        <ul className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-4">
          {data.map((card) => (
            <li key={card.id}>
              <PosterCard card={card} />
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-sm text-muted-foreground">
          Esta coleção ainda não tem itens.
        </p>
      )}
    </section>
  )
}
