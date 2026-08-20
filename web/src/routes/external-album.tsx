import type { ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Library, Music } from 'lucide-react'
import { ApiError, type ExternalAlbumTrack } from '@/lib/api'
import { externalAlbumQueryOptions } from '@/lib/queries'
import { useLanguage } from '@/lib/language-context'
import { Button } from '@/components/ui/button'
import { ErrorState, LoadingState } from '@/components/query-states'
import { ImportarButton } from '@/components/rich-detail-view'

// ExternalAlbumView é a tela de "faixas faltando" de um álbum (briefing 0115
// Parte 2, ADR-0029): faixas OFICIAIS do release, com "Importar" por faixa
// faltando e "na biblioteca" pras já catalogadas — mesmo padrão de
// CollectionDetailScreen (coleção→filmes), adaptado a álbum→faixas. Renderizada
// pela MESMA rota `/external/$source/$type/$id` que o detalhe rico usa
// (ExternalDetailScreen delega aqui quando `type==='album'`), então não precisa
// de rota própria nem toca router.tsx.

function propsDeErro(error: unknown): { title: string; message?: string } {
  const status = error instanceof ApiError ? error.status : 0
  const message = error instanceof Error ? error.message : undefined
  if (status === 404) {
    return { title: 'Álbum não encontrado na fonte', message }
  }
  if (status === 502 || status === 503) {
    return { title: 'Fonte indisponível no momento', message }
  }
  return { title: 'Não foi possível carregar o álbum', message }
}

export function ExternalAlbumView({
  source,
  id,
  back,
}: {
  source: string
  id: string
  back: ReactNode
}) {
  const { lang } = useLanguage()
  const { data, isPending, isError, error } = useQuery(
    externalAlbumQueryOptions(source, id, lang),
  )

  if (isPending) {
    return (
      <main className="mx-auto max-w-4xl px-4 py-6">
        <LoadingState label="Carregando álbum…" />
      </main>
    )
  }
  if (isError) {
    return (
      <main className="mx-auto max-w-4xl px-4 py-6">
        {back}
        <ErrorState {...propsDeErro(error)} />
      </main>
    )
  }

  return (
    <main className="mx-auto max-w-4xl px-4 py-6">
      <div className="flex flex-col gap-6">
        {back}

        <div className="flex flex-col gap-2 rounded-2xl bg-card p-4 ring-1 ring-foreground/10 sm:p-6">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div>
              <h1 className="font-heading text-2xl font-semibold">
                {data.name}
              </h1>
              {data.year || data.genre ? (
                <p className="text-sm text-muted-foreground">
                  {[data.year, data.genre].filter(Boolean).join(' · ')}
                </p>
              ) : null}
            </div>
            {data.internal_id ? (
              <Button asChild variant="secondary">
                <Link to="/entities/$id" params={{ id: data.internal_id }}>
                  <Library aria-hidden />
                  Ver álbum
                </Link>
              </Button>
            ) : null}
          </div>
        </div>

        <section className="flex flex-col gap-3">
          <h2 className="font-heading text-lg font-medium">Faixas do álbum</h2>
          {data.tracks.length > 0 ? (
            <ol className="flex flex-col divide-y divide-foreground/10 rounded-xl ring-1 ring-foreground/10">
              {data.tracks.map((track) => (
                <li key={track.source_id}>
                  <FaixaLinha
                    source={source}
                    track={track}
                    albumInternalId={data.internal_id ?? null}
                  />
                </li>
              ))}
            </ol>
          ) : (
            <p className="text-sm text-muted-foreground">
              Este álbum não tem faixas na fonte.
            </p>
          )}
        </section>
      </div>
    </main>
  )
}

// FaixaLinha é UMA faixa do release: já catalogada linka pro detalhe interno;
// faltando ganha o botão Importar (find-or-create ligado ao álbum pai via
// `parentId`, briefing 0115 Parte 2).
function FaixaLinha({
  source,
  track,
  albumInternalId,
}: {
  source: string
  track: ExternalAlbumTrack
  albumInternalId: string | null
}) {
  return (
    <div className="flex items-center gap-3 px-3 py-2.5">
      <span className="w-6 shrink-0 text-right text-sm tabular-nums text-muted-foreground">
        {track.track_number}
      </span>
      <Music className="size-4 shrink-0 text-muted-foreground" aria-hidden />
      {track.internal_id ? (
        <Link
          to="/entities/$id"
          params={{ id: track.internal_id }}
          className="line-clamp-1 flex-1 text-sm font-medium hover:underline"
        >
          {track.title}
        </Link>
      ) : (
        <span className="line-clamp-1 flex-1 text-sm font-medium">
          {track.title}
        </span>
      )}
      {track.internal_id ? (
        <span className="text-xs text-muted-foreground">na biblioteca</span>
      ) : albumInternalId ? (
        <ImportarButton
          source={source}
          sourceId={track.source_id}
          type="faixa"
          parentId={albumInternalId}
          size="sm"
          label="Importar"
        />
      ) : null}
    </div>
  )
}
