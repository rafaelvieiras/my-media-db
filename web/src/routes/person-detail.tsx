import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import { ArrowLeft, ExternalLink } from 'lucide-react'
import { ApiError, posterSrc, type PersonCredit } from '@/lib/api'
import { personQueryOptions } from '@/lib/queries'
import { rotuloTipo } from '@/lib/entity-badges'
import { PosterImg } from '@/components/rich-detail-view'
import { ErrorState, LoadingState } from '@/components/query-states'

// Rota `/person/$personId`: página MÍNIMA da pessoa (briefing 0029, fase D,
// interno-first). Cabeçalho com foto + nome (+ link externo pro TMDB quando a fonte é
// `tmdb`) e a FILMOGRAFIA = as obras JÁ importadas que a creditam (0029 fase C, campo
// `filmography` do `GET /person/{id}`), num grid navegável ao detalhe de cada uma.
// Read-only nesta fase — a pessoa não é catalogada como entity própria, então não há
// edição aqui (paridade com a leitura de elenco/equipe do detalhe rico).

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
// crédito é de elenco, ou o CARGO (com o departamento como fallback) quando é de
// equipe. Ausente → null (o card some sem a legenda de papel).
function papelDoCredito(credito: PersonCredit): string | null {
  if (credito.kind === 'cast') {
    return credito.character || null
  }
  return credito.job || credito.department || null
}

// FilmografiaCard é o card de UMA obra da filmografia: pôster (rota local da entity,
// via `posterSrc`) + título + ano/tipo + papel. Navega ao detalhe interno da entity
// (`/entities/{id}`) — a filmografia só lista obras JÁ importadas (0029 fase C), então
// o destino é sempre interno (sem ramo externo).
function FilmografiaCard({ credito }: { credito: PersonCredit }) {
  const papel = papelDoCredito(credito)
  return (
    <Link
      to="/entities/$id"
      params={{ id: credito.entity_id }}
      className="group flex flex-col gap-1"
    >
      <PosterImg
        src={posterSrc(credito.poster_url)}
        alt={`Pôster de ${credito.title}`}
        className="transition group-hover:ring-2 group-hover:ring-foreground/30"
      />
      <span className="line-clamp-2 text-xs font-medium group-hover:text-foreground">
        {credito.title}
      </span>
      <span className="line-clamp-1 text-xs text-muted-foreground">
        {[credito.year, rotuloTipo(credito.type)].filter(Boolean).join(' · ')}
      </span>
      {papel ? (
        <span className="line-clamp-2 text-xs text-muted-foreground">
          {papel}
        </span>
      ) : null}
    </Link>
  )
}

export function PersonDetailScreen() {
  const { personId } = useParams({ from: '/person/$personId' })
  const { data, isPending, isError, error } = useQuery(
    personQueryOptions(personId),
  )

  const voltar = (
    <Link
      to="/"
      className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
    >
      <ArrowLeft className="size-4" aria-hidden />
      Voltar à biblioteca
    </Link>
  )

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

        <section className="flex flex-col gap-3">
          <h2 className="font-heading text-lg font-medium">Filmografia</h2>
          {data.filmography.length > 0 ? (
            <ul className="grid grid-cols-3 gap-3 sm:grid-cols-4 md:grid-cols-6">
              {data.filmography.map((credito) => (
                <li key={`${credito.kind}:${credito.entity_id}`}>
                  <FilmografiaCard credito={credito} />
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-sm text-muted-foreground">
              Nenhuma obra na sua biblioteca ainda.
            </p>
          )}
        </section>
      </div>
    </main>
  )
}
