import { useQuery } from '@tanstack/react-query'
import { Link, useParams, useRouter } from '@tanstack/react-router'
import { ArrowLeft, Library } from 'lucide-react'
import {
  ApiError,
  type ExternalCollectionPart,
  type ExternalResult,
} from '@/lib/api'
import { collectionDetailQueryOptions } from '@/lib/queries'
import { useLanguage } from '@/lib/language-context'
import { internalIdDe } from '@/lib/internal-id'
import { srcDeImagem } from '@/lib/rich-detail'
import { Button } from '@/components/ui/button'
import { ErrorState, LoadingState } from '@/components/query-states'
import { ExternalResultCard } from '@/components/external-result-card'
import { ImportarButton } from '@/components/rich-detail-view'

// Route `/collection/$source/$id`: the external COLLECTION as a page of its own (a
// follow-up of the external collection view). Header with the name + backdrop of the set and a grid of the parts;
// each part is an `ExternalResultCard` (reuse) that navigates to the external detail and brings the
// Importar shortcut. Switching the language (top) re-fetches (the `lang` enters the queryKey).

// propsDeErro chooses title/message by HTTP status: 404 (absent in the source) ×
// 502/503 (source unavailable/without a collection) × the rest (generic).
function propsDeErro(error: unknown): { title: string; message?: string } {
  const status = error instanceof ApiError ? error.status : 0
  const message = error instanceof Error ? error.message : undefined
  if (status === 404) {
    return { title: 'Coleção não encontrada na fonte', message }
  }
  if (status === 502 || status === 503) {
    return { title: 'Fonte indisponível no momento', message }
  }
  return { title: 'Não foi possível carregar a coleção', message }
}

export function CollectionDetailScreen() {
  const { source, id } = useParams({ from: '/collection/$source/$id' })
  const { lang } = useLanguage()
  const router = useRouter()
  const { data, isPending, isError, error } = useQuery(
    collectionDetailQueryOptions(source, id, lang),
  )

  return (
    <main className="mx-auto max-w-4xl px-4 py-6">
      {isPending ? (
        <LoadingState label="Carregando coleção…" />
      ) : isError ? (
        <>
          <VoltarButton onClick={() => router.history.back()} />
          <ErrorState {...propsDeErro(error)} />
        </>
      ) : (
        <div className="flex flex-col gap-6">
          <VoltarButton onClick={() => router.history.back()} />

          {/* Header: backdrop as a BANNER with a gradient into the card; the name and the synopsis
              sit over the solid card (same contrast pattern as the rich hero). */}
          <div className="overflow-hidden rounded-2xl bg-card ring-1 ring-foreground/10">
            {srcDeImagem(data.backdrop_url) ? (
              <div className="relative aspect-[16/6] w-full sm:aspect-[16/5]">
                <img
                  src={srcDeImagem(data.backdrop_url) ?? undefined}
                  alt=""
                  aria-hidden
                  className="h-full w-full object-cover"
                />
                <div className="absolute inset-0 bg-gradient-to-t from-card via-card/40 to-transparent" />
              </div>
            ) : null}
            <div className="flex flex-col gap-2 p-4 sm:p-6">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <h1 className="font-heading text-2xl font-semibold">
                  {data.name}
                </h1>
                {/* Internal-first: if the collection was ALREADY imported, the header takes
                    to the internal entity ("Ver coleção") instead of offering the import again.
                    Otherwise, the button imports the WHOLE collection — it becomes an entity
                    type='collection' with the member movies linked; it navigates to
                    /entities/{novoId}. A label distinct from the "Importar" of the parts. */}
                {internalIdDe(data) != null ? (
                  <Button asChild>
                    <Link
                      to="/entities/$id"
                      params={{ id: internalIdDe(data) as string }}
                    >
                      <Library aria-hidden />
                      Ver coleção
                    </Link>
                  </Button>
                ) : (
                  <ImportarButton
                    source={source}
                    sourceId={id}
                    type="collection"
                    label="Importar coleção"
                  />
                )}
              </div>
              {data.overview ? (
                <p className="text-sm text-muted-foreground">{data.overview}</p>
              ) : null}
            </div>
          </div>

          <section className="flex flex-col gap-3">
            <h2 className="font-heading text-lg font-medium">
              Filmes da coleção
            </h2>
            {data.parts.length > 0 ? (
              <ul className="grid gap-3 sm:grid-cols-2">
                {data.parts.map((parte) => (
                  <ExternalResultCard
                    key={`${parte.type}:${parte.source_id}`}
                    result={comoResultado(source, parte)}
                    internalId={internalIdDe(parte)}
                  />
                ))}
              </ul>
            ) : (
              <p className="text-sm text-muted-foreground">
                Esta coleção não tem itens.
              </p>
            )}
          </section>
        </div>
      )}
    </main>
  )
}

// comoResultado adapts a part of the collection to the ExternalResult shape that the
// ExternalResultCard consumes (same fields; `saved` is always false — the part is an
// external item until it is imported).
function comoResultado(
  source: string,
  parte: ExternalCollectionPart,
): ExternalResult {
  return {
    source,
    source_id: parte.source_id,
    type: parte.type,
    title: parte.title,
    year: parte.year,
    overview: parte.overview,
    poster_url: parte.poster_url,
    saved: false,
  }
}

// VoltarButton goes back in the history (the collection is always reached by navigation
// from a detail page).
function VoltarButton({ onClick }: { onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
    >
      <ArrowLeft className="size-4" aria-hidden />
      Voltar
    </button>
  )
}
