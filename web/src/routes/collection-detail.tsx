import { useQuery } from '@tanstack/react-query'
import { useParams, useRouter } from '@tanstack/react-router'
import { ArrowLeft } from 'lucide-react'
import {
  ApiError,
  type ExternalCollectionPart,
  type ExternalResult,
} from '@/lib/api'
import { collectionDetailQueryOptions } from '@/lib/queries'
import { useLanguage } from '@/lib/language-context'
import { srcDeImagem } from '@/lib/rich-detail'
import { ErrorState, LoadingState } from '@/components/query-states'
import { ExternalResultCard } from '@/components/external-result-card'

// Rota `/collection/$source/$id`: a COLEÇÃO externa como página própria (0024,
// follow-up do 0023). Header com nome + backdrop do conjunto e um grid das partes;
// cada parte é um `ExternalResultCard` (reuso) que navega ao detalhe externo e traz o
// atalho Importar. Trocar o idioma (topo) re-busca (o `lang` entra na queryKey).

// propsDeErro escolhe título/mensagem pelo status HTTP: 404 (inexistente na fonte) ×
// 502/503 (fonte indisponível/sem coleção) × demais (genérico).
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

          {/* Header: backdrop como BANNER com degradê para o card; o nome e a sinopse
              ficam sobre o card sólido (mesmo padrão de contraste do hero rico). */}
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
              <h1 className="font-heading text-2xl font-semibold">
                {data.name}
              </h1>
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

// comoResultado adapta uma parte da coleção ao shape ExternalResult que o
// ExternalResultCard consome (mesmos campos; `saved` é sempre false — a parte é um
// item externo até ser importado).
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

// VoltarButton retorna no histórico (a coleção é sempre alcançada por navegação a
// partir de um detalhe).
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
