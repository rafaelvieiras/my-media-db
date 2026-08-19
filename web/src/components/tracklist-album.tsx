import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Music } from 'lucide-react'
import { ErrorState, LoadingState } from '@/components/query-states'
import { useLanguage } from '@/lib/language-context'
import { entityMembersQueryOptions } from '@/lib/queries'
import type { EntityCard } from '@/lib/api'

// TracklistAlbum lista as FAIXAS de um ÁLBUM (briefing 0111): filhos por
// entity_relation kind=music_album (ADR-0027, sort_index=track_number) — o
// MESMO endpoint genérico de membros (GET /entities/{id}/members,
// `entityMembersQueryOptions`) que já alimenta `MembrosSomenteLeitura`
// (temporadas de série/episódios de temporada, `entity-members.tsx`). Cada
// faixa é um LINK pro seu próprio detalhe (jornada §3 do briefing).
//
// ORDENAÇÃO (gap de backend, ver briefing 0111 e a nota de implementação
// abaixo): `EntityCard` não carrega `track_number`/`disc_number` — eles ficam
// em `extension_data` (namespace core.music@1) da faixa, não exposto pelo
// card. A lista usa a ORDEM DE CHEGADA do array como posição da faixa, que
// reflete `sort_index` (`ORDER BY sort_index` de `ListarRelacoesFilhas`,
// internal/db/queries/entity_relation.sql — e `sort_index` = `track_number`
// para o kind `music_album`, internal/enrich/musica.go): correta para álbum
// de UM disco. Álbum multi-disco não agrupa por disco nem reordena por ele
// (dado ausente do card) — ver o gap reportado no briefing.
//
// NOTA (0111): hoje `kindEOrdemMembros` (internal/server/entities.go) só
// reconhece `tipoPai` `tv`/`season`; para `album` cai no `default` (kind=
// "collection"), que nunca bate com `music_album` — esta seção fica VAZIA
// contra dado real até o backend ganhar o mapeamento genérico (reportado no
// resumo da implementação, não feito aqui por instrução explícita do
// briefing §Passo 1: "endpoint hardcoded → não reescreva sozinho").
export function TracklistAlbum({ id }: { id: string }) {
  const { lang } = useLanguage()
  const { data, isPending, isError } = useQuery(
    entityMembersQueryOptions(id, lang),
  )
  const faixas = data?.filter((card) => card.type === 'faixa')

  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-medium">Faixas</h2>
      {isPending ? (
        <LoadingState label="Carregando faixas…" />
      ) : isError ? (
        <ErrorState title="Não foi possível carregar as faixas" />
      ) : !faixas || faixas.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          Este álbum ainda não tem faixas catalogadas.
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

// FaixaLinha é UMA linha da tracklist: posição (ordinal na lista, ver nota de
// ordenação acima) + ícone + título, linkando pro detalhe da faixa.
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
