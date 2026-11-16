import { useQuery } from '@tanstack/react-query'
import { useParams, useRouter } from '@tanstack/react-router'
import { ArrowLeft } from 'lucide-react'
import { ApiError } from '@/lib/api'
import { externalDetailQueryOptions } from '@/lib/queries'
import { useLanguage } from '@/lib/language-context'
import { ErrorState, LoadingState } from '@/components/query-states'
import { ImportarButton, RichDetailView } from '@/components/rich-detail-view'
import { deExternalDetail } from '@/lib/rich-detail'
import { ExternalAlbumView } from '@/routes/external-album'

// Route `/external/$source/$type/$id`: the external item as a whole BEFORE importing
// It consumes the rich, persistent detail in the active language —
// switching the language (top) re-fetches the translatable fields (the `lang` enters the queryKey).
// It reuses the RichDetailView SHARED with the entity detail; what is specific here is
// the Importar button and the recursive "Voltar" (similars navigate to this same route).
//
// `type==='album'` is a TOTALLY different shape (tracks
// of the release, not a single item) — it delegates to ExternalAlbumView instead of mounting the
// RichDetailView, without needing a route of its own (the backend also reuses the same
// "album" segment with precedence over {type}, see server.go). ExternalDetailScreen
// only decides WHICH sub-screen to render; each one calls its own hooks (the rules of
// hooks preserved — no conditional hook inside the same component).

// propsDeErro chooses the title/message of the error state by HTTP status: 404
// (absent in the source) × 502/503 (source unavailable) × the rest (generic).
function propsDeErro(error: unknown): { title: string; message?: string } {
  const status = error instanceof ApiError ? error.status : 0
  const message = error instanceof Error ? error.message : undefined
  if (status === 404) {
    return { title: 'Item não encontrado na fonte', message }
  }
  if (status === 502 || status === 503) {
    return { title: 'Fonte indisponível no momento', message }
  }
  return { title: 'Não foi possível carregar o detalhe', message }
}

export function ExternalDetailScreen() {
  const { source, type, id } = useParams({
    from: '/external/$source/$type/$id',
  })
  const router = useRouter()

  if (type === 'album') {
    return (
      <ExternalAlbumView
        source={source}
        id={id}
        back={<VoltarButton onClick={() => router.history.back()} />}
      />
    )
  }
  return <ExternalItemView source={source} type={type} id={id} />
}

// ExternalItemView is the rich detail BEFORE importing of an item of ONE type
// (movie/tv/collection — never album, see the branch above): extracted from the original
// body of ExternalDetailScreen to keep the rules of hooks sane when
// `type==='album'` delegates to another component without calling this hook.
function ExternalItemView({
  source,
  type,
  id,
}: {
  source: string
  type: string
  id: string
}) {
  const { lang } = useLanguage()
  const router = useRouter()
  const { data, isPending, isError, error } = useQuery(
    externalDetailQueryOptions(source, type, id, lang),
  )

  return (
    <main className="mx-auto max-w-4xl px-4 py-6">
      {isPending ? (
        <LoadingState label="Carregando detalhe…" />
      ) : isError ? (
        <>
          <VoltarButton onClick={() => router.history.back()} />
          <ErrorState {...propsDeErro(error)} />
        </>
      ) : (
        <RichDetailView
          detail={deExternalDetail(data)}
          source={source}
          back={<VoltarButton onClick={() => router.history.back()} />}
          actions={
            <ImportarButton
              source={source}
              sourceId={data.source_id}
              type={data.type}
              size="lg"
            />
          }
        />
      )}
    </main>
  )
}

// VoltarButton goes back in the history — recursive navigation (similar → similar →
// back) is normal route history.
function VoltarButton({ onClick }: { onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="mb-4 inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
    >
      <ArrowLeft className="size-4" aria-hidden />
      Voltar
    </button>
  )
}
