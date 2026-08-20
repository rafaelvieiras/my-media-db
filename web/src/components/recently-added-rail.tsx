import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { entitiesQueryOptions, entityTypesQueryOptions } from '@/lib/queries'
import { PosterCard } from '@/components/poster-card'
import { ErrorState, LoadingState } from '@/components/query-states'
import { rotuloTipoPlural } from '@/lib/entity-badges'
import { tipoParaSlug } from '@/lib/library-slugs'
import {
  Carousel,
  CarouselContent,
  CarouselItem,
  CarouselNext,
  CarouselPrevious,
} from '@/components/ui/carousel'

const ITENS_POR_CARROSSEL = 10

// RecentlyAddedRail é o carrossel "adicionados recentemente" de UM schema
// (briefing 0109): dispara SUA PRÓPRIA useQuery, independente de qualquer
// outra instância — requisito explícito do dono ("cada slide deve puxar
// separadamente"), não um pai que busca tudo de uma vez e distribui. Ordena
// por created_at desc (`sort: '-created_at'`), até 10 itens, só o que está NA
// biblioteca (`owned: true`). Reaproveita o PosterCard já usado pelo grid
// (mesmo EntityCard, mesmos badges de posse/revisão) e o Carousel do shadcn
// (embla) — sem card nem carrossel novos do zero.
//
// Vazio: quando a busca volta zerada o componente não renderiza NADA (nem o
// título) — a home (`routes/index.tsx`) já só monta um rail por `type`
// devolvido por `GET /entities/types` (que filtra tipo sem dado), mas este
// componente também é defensivo por conta própria, para nunca sobrar uma
// seção "Adicionados recentemente" fantasma.
export function RecentlyAddedRail({
  type,
  label,
  lang,
}: {
  type: string
  label: string
  lang?: string
}) {
  const { data, isPending, isError, isSuccess, error } = useQuery(
    entitiesQueryOptions(
      { type, sort: '-created_at', limit: ITENS_POR_CARROSSEL, owned: true },
      lang,
    ),
  )

  if (isSuccess && data.items.length === 0) {
    return null
  }

  return (
    <section className="flex flex-col gap-3">
      {isPending ? (
        <>
          <RailTitulo type={type} label={label} />
          <LoadingState label={`Carregando ${label.toLowerCase()}…`} />
        </>
      ) : isError ? (
        <>
          <RailTitulo type={type} label={label} />
          <ErrorState
            title={`Não foi possível carregar "${label}"`}
            message={error instanceof Error ? error.message : undefined}
          />
        </>
      ) : (
        <Carousel opts={{ align: 'start' }} className="w-full">
          <div className="mb-3 flex items-center justify-between gap-3">
            <RailTitulo type={type} label={label} />
            <div className="flex gap-1">
              <CarouselPrevious className="static translate-y-0" />
              <CarouselNext className="static translate-y-0" />
            </div>
          </div>
          <CarouselContent>
            {data.items.map((card) => (
              <CarouselItem
                key={card.id}
                className="basis-1/2 sm:basis-1/3 md:basis-1/4 lg:basis-1/6"
              >
                <PosterCard card={card} />
              </CarouselItem>
            ))}
          </CarouselContent>
        </Carousel>
      )}
    </section>
  )
}

// RailTitulo (briefing 0114/ADR-0030) ganha um link "Ver Todos" ao lado do
// título, apontando para a rota `/library/$entitySlug` do tipo daquele
// carrossel — mesmo mapa slug↔type de `app-header.tsx` (`library-slugs.ts`).
// Tipo sem slug mapeado (defensivo — hoje `GET /entities/types` já exclui
// sub-nós, então isso não deveria acontecer na prática) só omite o link, sem
// quebrar o título.
function RailTitulo({ type, label }: { type: string; label: string }) {
  const slug = tipoParaSlug(type)
  return (
    <div className="flex items-center gap-3">
      <h2 className="font-heading text-lg font-medium">{label}</h2>
      {slug ? (
        <Link
          to="/library/$entitySlug"
          params={{ entitySlug: slug }}
          className="text-sm text-muted-foreground hover:text-foreground hover:underline"
        >
          Ver Todos
        </Link>
      ) : null}
    </div>
  )
}

// RecentlyAddedSection é a seção da home (briefing 0109): busca os `type`
// com dado catalogado (`GET /entities/types`, `entityTypesQueryOptions`) e
// monta um RecentlyAddedRail por tipo — SEM lista fixa de tipos hardcoded
// aqui, cobre qualquer vertical nova (ex.: música) assim que o backend tiver
// dado dela. O rótulo de cada seção vem de `rotuloTipoPlural` (mapa central
// de `entity-badges.ts`, compartilhado com o resto da UI). Biblioteca zerada
// (`GET /entities/types` devolve `[]`) → a seção inteira não renderiza nada
// (sem cabeçalho "Adicionados recentemente" fantasma). Erro na descoberta de
// tipos também não renderiza nada: é uma falha discreta — o grid paginado
// abaixo continua funcionando normalmente e já tem seu próprio estado de erro.
export function RecentlyAddedSection({ lang }: { lang?: string } = {}) {
  const { data, isSuccess } = useQuery(entityTypesQueryOptions)

  if (!isSuccess || data.length === 0) {
    return null
  }

  return (
    <div className="mb-8 flex flex-col gap-8">
      {data.map((tipo) => (
        <RecentlyAddedRail
          key={tipo.type}
          type={tipo.type}
          label={rotuloTipoPlural(tipo.type)}
          lang={lang}
        />
      ))}
    </div>
  )
}
