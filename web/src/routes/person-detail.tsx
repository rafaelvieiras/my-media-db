import type { ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import { ExternalLink } from 'lucide-react'
import {
  ApiError,
  posterSrc,
  type PersonCredit,
  type PersonExternalCredit,
} from '@/lib/api'
import { personQueryOptions } from '@/lib/queries'
import { rotuloTipo } from '@/lib/entity-badges'
import { PosterImg } from '@/components/rich-detail-view'
import {
  ErrorState,
  LoadingState,
  VoltarBiblioteca,
} from '@/components/query-states'
import { Badge } from '@/components/ui/badge'

// Rota `/person/$personId`: página MÍNIMA da pessoa (briefing 0029, fase D,
// interno-first). Cabeçalho com foto + nome (+ link externo pro TMDB quando a fonte é
// `tmdb`) e DUAS seções de filmografia: "Na sua biblioteca" (0029 fase C, campo
// `filmography`) — obras JÁ importadas, cada card navega ao detalhe INTERNO da entity —
// e "Ainda não importados" (0029 fase D, campo `filmography_external`) — obras que o
// provider trouxe mas ainda não estão no catálogo, cada card navega ao detalhe EXTERNO
// (`/external/$source/$type/$id`, mesmo destino usado pela busca/similares) para
// ver/importar. Read-only nesta fase — a pessoa não é catalogada como entity própria,
// então não há edição aqui (paridade com a leitura de elenco/equipe do detalhe rico).

const TMDB_PESSOA = 'https://www.themoviedb.org/person/'

// propsDeErro escolhe título/mensagem pelo status HTTP: 404 (pessoa inexistente no
// catálogo) × demais (genérico).
function propsDeErro(error: unknown): { title: string; message?: string } {
  const status = error instanceof ApiError ? error.status : 0
  const message = error instanceof Error ? error.message : undefined
  if (status === 404) {
    return { title: 'Pessoa não encontrada', message }
  }
  return { title: 'Não foi possível carregar a pessoa', message }
}

// papelDoCredito escolhe o texto do papel exibido no card: o PERSONAGEM quando o
// crédito é de elenco, ou o CARGO (com o departamento como fallback, só existente em
// PersonCredit) quando é de equipe. Ausente → null (o card some sem a legenda de
// papel). Tipo estrutural compartilhado pelas duas variantes de crédito (interna e
// externa), que diferem só na identidade de destino do card.
function papelDoCredito(credito: {
  kind: string
  character?: string | null
  job?: string | null
  department?: string | null
}): string | null {
  if (credito.kind === 'cast') {
    return credito.character || null
  }
  return credito.job || credito.department || null
}

// CorpoCard é o MIOLO visual compartilhado dos dois cards de filmografia (interno e
// externo): pôster + título + ano/tipo + papel + um selo opcional. Extraí-lo evita a
// duplicação do layout entre as duas variantes, que diferem só no destino do `Link`
// que o envolve (a catraca do 0005 mede duplicação com jscpd).
function CorpoCard({
  title,
  type,
  year,
  poster,
  papel,
  selo,
}: {
  title: string
  type: string
  year: string | null
  poster: string | null
  papel: string | null
  selo?: ReactNode
}) {
  return (
    <>
      <PosterImg
        src={poster}
        alt={`Pôster de ${title}`}
        className="transition group-hover:ring-2 group-hover:ring-foreground/30"
      />
      <span className="line-clamp-2 text-xs font-medium group-hover:text-foreground">
        {title}
      </span>
      <span className="line-clamp-1 text-xs text-muted-foreground">
        {[year, rotuloTipo(type)].filter(Boolean).join(' · ')}
      </span>
      {papel ? (
        <span className="line-clamp-2 text-xs text-muted-foreground">
          {papel}
        </span>
      ) : null}
      {selo}
    </>
  )
}

// FilmografiaCard é o card de UMA obra JÁ importada: navega ao detalhe INTERNO da entity
// (`/entities/{id}`). O pôster é uma rota local (via `posterSrc`).
function FilmografiaCard({ credito }: { credito: PersonCredit }) {
  return (
    <Link
      to="/entities/$id"
      params={{ id: credito.entity_id }}
      className="group flex flex-col gap-1"
    >
      <CorpoCard
        title={credito.title}
        type={credito.type}
        year={credito.year ?? null}
        poster={posterSrc(credito.poster_url)}
        papel={papelDoCredito(credito)}
      />
    </Link>
  )
}

// FilmografiaExternaCard é o card de UMA obra AINDA NÃO importada: navega ao detalhe
// EXTERNO (`/external/$source/$type/$id` — a mesma rota que a busca e os similares usam)
// e ganha o selo "Não importado". `poster_url` já vem PROXYADA do backend (ao contrário
// do pôster interno, que é uma rota local — daí não passar por `posterSrc`).
function FilmografiaExternaCard({
  credito,
}: {
  credito: PersonExternalCredit
}) {
  return (
    <Link
      to="/external/$source/$type/$id"
      params={{
        source: credito.source,
        type: credito.type,
        id: credito.source_id,
      }}
      aria-label={`Ver ${credito.title} (ainda não importado)`}
      className="group flex flex-col gap-1"
    >
      <CorpoCard
        title={credito.title}
        type={credito.type}
        year={credito.year ?? null}
        poster={credito.poster_url ?? null}
        papel={papelDoCredito(credito)}
        selo={
          <Badge variant="secondary" className="w-fit">
            Não importado
          </Badge>
        }
      />
    </Link>
  )
}

export function PersonDetailScreen() {
  const { personId } = useParams({ from: '/person/$personId' })
  const { data, isPending, isError, error } = useQuery(
    personQueryOptions(personId),
  )

  const voltar = <VoltarBiblioteca />

  if (isPending) {
    return (
      <main className="mx-auto max-w-4xl px-4 py-6">
        {voltar}
        <LoadingState label="Carregando pessoa…" />
      </main>
    )
  }
  if (isError) {
    return (
      <main className="mx-auto max-w-4xl px-4 py-6">
        {voltar}
        <ErrorState {...propsDeErro(error)} />
      </main>
    )
  }

  const externas = data.filmography_external ?? []
  const temNaBiblioteca = data.filmography.length > 0
  const temNaoImportados = externas.length > 0

  return (
    <main className="mx-auto max-w-4xl px-4 py-6">
      <div className="flex flex-col gap-6">
        {voltar}

        <div className="flex flex-wrap items-center gap-4">
          <PosterImg
            src={data.profile_url ?? null}
            alt={`Foto de ${data.name}`}
            className="w-28 shrink-0"
          />
          <div className="flex flex-col gap-1">
            <h1 className="font-heading text-2xl font-semibold">{data.name}</h1>
            {data.source === 'tmdb' && data.source_id ? (
              <a
                href={`${TMDB_PESSOA}${data.source_id}`}
                target="_blank"
                rel="noreferrer"
                className="inline-flex w-fit items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
              >
                <ExternalLink className="size-4" aria-hidden />
                TMDB
              </a>
            ) : null}
          </div>
        </div>

        {temNaBiblioteca ? (
          <section className="flex flex-col gap-3">
            <h2 className="font-heading text-lg font-medium">
              Na sua biblioteca
            </h2>
            <ul className="grid grid-cols-3 gap-3 sm:grid-cols-4 md:grid-cols-6">
              {data.filmography.map((credito) => (
                <li key={`${credito.kind}:${credito.entity_id}`}>
                  <FilmografiaCard credito={credito} />
                </li>
              ))}
            </ul>
          </section>
        ) : null}

        {temNaoImportados ? (
          <section className="flex flex-col gap-3">
            <h2 className="font-heading text-lg font-medium">
              Ainda não importados
            </h2>
            <ul className="grid grid-cols-3 gap-3 sm:grid-cols-4 md:grid-cols-6">
              {externas.map((credito) => (
                <li
                  key={`${credito.kind}:${credito.source}:${credito.source_id}`}
                >
                  <FilmografiaExternaCard credito={credito} />
                </li>
              ))}
            </ul>
          </section>
        ) : null}

        {!temNaBiblioteca && !temNaoImportados ? (
          <p className="text-sm text-muted-foreground">
            Nenhuma obra na sua biblioteca ainda.
          </p>
        ) : null}
      </div>
    </main>
  )
}
