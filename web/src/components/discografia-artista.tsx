import { useQuery } from '@tanstack/react-query'
import { GradeMembros } from '@/components/entity-members'
import { PosterCard } from '@/components/poster-card'
import { useLanguage } from '@/lib/language-context'
import { entityMembersQueryOptions } from '@/lib/queries'

// DiscografiaArtista lista os ÁLBUNS de um ARTISTA (briefing 0111): filhos
// por entity_relation kind=music_artista_principal (ADR-0027) — o MESMO
// endpoint genérico de membros (GET /entities/{id}/members,
// `entityMembersQueryOptions`) que já alimenta `MembrosSomenteLeitura`
// (temporadas/episódios da série, `entity-members.tsx`), reusando o MESMO
// `GradeMembros` (casca de carregando/erro/vazio + grid) e o MESMO
// `PosterCard` do grid geral (0037/0109) — nenhum card novo.
//
// FILTRO type=album: `music_artista_principal` também liga FAIXAS soltas
// (colaborações cujo artista da faixa diverge do artista principal do
// álbum, ADR-0027) direto ao artista — a discografia mostra só os filhos
// `type=album`, filtro CLIENT-SIDE sobre a mesma resposta.
//
// NOTA (0111): hoje `kindEOrdemMembros` (internal/server/entities.go) só
// reconhece `tipoPai` `tv`/`season`; para `artista` cai no `default` (kind=
// "collection"), que nunca bate com `music_artista_principal` — esta seção
// fica VAZIA contra dado real até o backend ganhar o mapeamento genérico
// (reportado no resumo da implementação, não feito aqui por instrução
// explícita do briefing §Passo 1: "endpoint hardcoded → não reescreva
// sozinho").
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
