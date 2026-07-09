import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import { ArrowLeft } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { entityQueryOptions } from '@/lib/queries'
import { ehDraft, rotuloTipo } from '@/lib/entity-badges'
import { MediaInfoTable } from '@/components/media-info-table'
import { ErrorState, LoadingState } from '@/components/query-states'

// Rota `/entities/$id`: cabeçalho (título/tipo/status) + lista de arquivos com
// mediainfo legível. Itens não-identificados aparecem como `draft`.
export function EntityDetailScreen() {
  const { id } = useParams({ from: '/entities/$id' })
  const { data, isPending, isError, error } = useQuery(entityQueryOptions(id))

  return (
    <main className="mx-auto max-w-4xl px-4 py-6">
      <Link
        to="/"
        className="mb-4 inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft className="size-4" aria-hidden />
        Voltar à biblioteca
      </Link>

      {isPending ? (
        <LoadingState label="Carregando item…" />
      ) : isError ? (
        <ErrorState
          title="Não foi possível carregar o item"
          message={error instanceof Error ? error.message : undefined}
        />
      ) : (
        <>
          <header className="mb-6 flex flex-col gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="font-heading text-2xl font-semibold">
                {data.title}
              </h1>
              <Badge variant="outline">{rotuloTipo(data.type)}</Badge>
              {ehDraft(data.status) ? (
                <Badge variant="secondary">não identificado</Badge>
              ) : (
                <Badge variant="outline">{data.status}</Badge>
              )}
            </div>
            {data.description ? (
              <p className="text-sm text-muted-foreground">
                {data.description}
              </p>
            ) : null}
          </header>

          <section className="flex flex-col gap-3">
            <h2 className="font-heading text-lg font-medium">Arquivos</h2>
            <MediaInfoTable entityId={id} />
          </section>
        </>
      )}
    </main>
  )
}
