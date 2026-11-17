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
import { useT } from '@/lib/i18n'
import type { Translate } from '@/lib/i18n/types'

// Route `/person/$personId`: MINIMAL person page (phase D,
// internal-first). Header with photo + name (+ external link to TMDB when the source is
// `tmdb`) and TWO filmography sections: "In your library" (phase C, field
// `filmography`) — works ALREADY imported, each card navigates to the INTERNAL detail of the entity —
// and "Not imported yet" (phase D, field `filmography_external`) — works that the
// provider brought but are not in the catalog yet, each card navigates to the EXTERNAL detail
// (`/external/$source/$type/$id`, the same destination used by the search/similars) to
// see/import. Read-only in this phase — the person is not cataloged as an entity of its own,
// so there is no editing here (parity with the cast/crew reading of the rich detail).

const TMDB_PESSOA = 'https://www.themoviedb.org/person/'

// propsDeErro chooses title/message by HTTP status: 404 (person absent from the
// catalog) × the rest (generic).
function propsDeErro(
  error: unknown,
  t: Translate,
): { title: string; message?: string } {
  const status = error instanceof ApiError ? error.status : 0
  const message = error instanceof Error ? error.message : undefined
  if (status === 404) {
    return { title: t('entity.person.notFound'), message }
  }
  return { title: t('entity.person.error'), message }
}

// papelDoCredito chooses the text of the role shown on the card: the CHARACTER when the
// credit is from the cast, or the JOB (with the department as fallback, only existing in
// PersonCredit) when it is from the crew. Absent → null (the card disappears without the role
// caption). A structural type shared by the two credit variants (internal and
// external), which differ only in the destination identity of the card.
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

// CorpoCard is the shared visual CORE of the two filmography cards (internal and
// external): poster + title + year/type + role + an optional badge. Extracting it avoids the
// duplication of the layout between the two variants, which differ only in the destination of the `Link`
// that wraps them (the ratchet measures duplication with jscpd).
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
  const t = useT()
  return (
    <>
      <PosterImg
        src={poster}
        alt={t('entity.alt.poster', { title })}
        className="transition group-hover:ring-2 group-hover:ring-foreground/30"
      />
      <span className="line-clamp-2 text-xs font-medium group-hover:text-foreground">
        {title}
      </span>
      <span className="line-clamp-1 text-xs text-muted-foreground">
        {[year, rotuloTipo(type, t)].filter(Boolean).join(' · ')}
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

// FilmografiaCard is the card of a work ALREADY imported: it navigates to the INTERNAL detail of the entity
// (`/entities/{id}`). The poster is a local route (via `posterSrc`).
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

// FilmografiaExternaCard is the card of a work NOT imported YET: it navigates to the
// EXTERNAL detail (`/external/$source/$type/$id` — the same route that the search and the similars use)
// and gains the "Not imported" badge. `poster_url` already comes PROXIED from the backend (unlike
// the internal poster, which is a local route — hence it does not go through `posterSrc`).
function FilmografiaExternaCard({
  credito,
}: {
  credito: PersonExternalCredit
}) {
  const t = useT()
  return (
    <Link
      to="/external/$source/$type/$id"
      params={{
        source: credito.source,
        type: credito.type,
        id: credito.source_id,
      }}
      aria-label={t('entity.person.viewExternal', { title: credito.title })}
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
            {t('entity.person.notImportedBadge')}
          </Badge>
        }
      />
    </Link>
  )
}

export function PersonDetailScreen() {
  const t = useT()
  const { personId } = useParams({ from: '/person/$personId' })
  const { data, isPending, isError, error } = useQuery(
    personQueryOptions(personId),
  )

  const voltar = <VoltarBiblioteca />

  if (isPending) {
    return (
      <main className="mx-auto max-w-4xl px-4 py-6">
        {voltar}
        <LoadingState label={t('entity.person.loading')} />
      </main>
    )
  }
  if (isError) {
    return (
      <main className="mx-auto max-w-4xl px-4 py-6">
        {voltar}
        <ErrorState {...propsDeErro(error, t)} />
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
            alt={t('entity.alt.personPhoto', { name: data.name })}
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
              {t('entity.person.inLibrary')}
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
              {t('entity.person.notImported')}
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
            {t('entity.person.empty')}
          </p>
        ) : null}
      </div>
    </main>
  )
}
