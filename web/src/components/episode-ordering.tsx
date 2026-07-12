import type { ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Select } from '@/components/ui/select'
import { CardNumerado, GradeMembros } from '@/components/entity-members'
import { ErrorState, LoadingState } from '@/components/query-states'
import { useLanguage } from '@/lib/language-context'
import { entityEpisodesQueryOptions } from '@/lib/queries'

// Seletor de ordenação de episódios (0040): na tela da série, troca a ordem de
// exibição (official/dvd/absolute…). A ordem default mostra os cards de temporada
// navegáveis (a view atual); ordens alternativas reagrupam os episódios em SEÇÕES
// "Temporada N (ORDEM)" — cabeçalhos NÃO-navegáveis (ADR-0021: só o episódio é uma
// entidade), com os cards de episódio numerados na ordem escolhida. A ordem vive no
// search param da rota; este módulo só consome/dispara a troca.

// rotuloOrdem dá o nome amigável de uma ordem: mapeia as conhecidas e capitaliza as
// demais (uma ordem nova da fonte aparece legível sem tocar aqui). Interno ao módulo
// (react-refresh: o arquivo de componente só exporta componentes) — exercitado pelos
// rótulos das opções do SeletorOrdenacao.
function rotuloOrdem(ordem: string): string {
  switch (ordem) {
    case 'official':
      return 'Oficial'
    case 'dvd':
      return 'DVD'
    case 'absolute':
      return 'Absoluto'
    default:
      return ordem.charAt(0).toUpperCase() + ordem.slice(1)
  }
}

// SeletorOrdenacao é o dropdown das ordens disponíveis. O caller só o renderiza
// quando há mais de uma ordem (senão não há escolha). `onChange` recebe a ordem crua
// (o caller decide navegar/remover o param).
export function SeletorOrdenacao({
  value,
  options,
  onChange,
}: {
  value: string
  options: string[]
  onChange: (ordem: string) => void
}) {
  return (
    <Select
      aria-label="Ordem de exibição dos episódios"
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className="w-auto"
    >
      {options.map((o) => (
        <option key={o} value={o}>
          {rotuloOrdem(o)}
        </option>
      ))}
    </Select>
  )
}

// EpisodiosAgrupados renderiza a view de ordem ALTERNATIVA: busca
// /entities/{id}/episodes?ordering=X e desenha SEÇÕES por grupo — cada uma com um
// cabeçalho não-navegável (`label`: "Temporada N", "Episódios" no absoluto, ou "Sem
// numeração") e o grid de episódios navegáveis (reusa `GradeMembros`/`CardNumerado` do
// 0039, com o `numbering` já projetado na ordem X pelo backend). O `seletor` é passado
// pelo caller e mora ao lado do título. Estados carregando/erro/vazio.
export function EpisodiosAgrupados({
  id,
  ordering,
  seletor,
}: {
  id: string
  ordering: string
  seletor: ReactNode
}) {
  const { lang } = useLanguage()
  const { data, isPending, isError } = useQuery(
    entityEpisodesQueryOptions(id, ordering, lang),
  )
  return (
    <section className="flex flex-col gap-4">
      <div className="flex items-center justify-between gap-3">
        <h2 className="font-heading text-lg font-medium">Episódios</h2>
        {seletor}
      </div>
      {isPending ? <LoadingState label="Carregando episódios…" /> : null}
      {isError ? (
        <ErrorState title="Não foi possível carregar os episódios" />
      ) : null}
      {data && data.groups.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          Esta série ainda não tem episódios nesta ordem.
        </p>
      ) : null}
      {data?.groups.map((grupo, indice) => (
        <div key={`${grupo.season}-${indice}`} className="flex flex-col gap-3">
          <h3 className="text-sm font-medium text-muted-foreground">
            {grupo.label}
          </h3>
          <GradeMembros
            itens={grupo.episodes}
            isPending={false}
            isError={false}
            carregando=""
            erro=""
            vazio="Nenhum episódio."
            render={(card) => <CardNumerado card={card} />}
          />
        </div>
      ))}
    </section>
  )
}
