import { useQuery } from '@tanstack/react-query'
import { useParams, useRouter } from '@tanstack/react-router'
import { ArrowLeft } from 'lucide-react'
import { ApiError } from '@/lib/api'
import { externalDetailQueryOptions } from '@/lib/queries'
import { useLanguage } from '@/lib/language-context'
import { ErrorState, LoadingState } from '@/components/query-states'
import { ImportarButton, RichDetailView } from '@/components/rich-detail-view'
import { deExternalDetail } from '@/lib/rich-detail'

// Rota `/external/$source/$type/$id`: o item externo por inteiro ANTES de importar
// (briefing 0023). Consome o detalhe rico e persistente do 0022 no idioma ativo —
// trocar o idioma (topo) re-busca os campos traduzíveis (o `lang` entra na queryKey).
// Reusa a RichDetailView COMPARTILHADA com o detalhe da entity; o específico daqui é
// o botão Importar e o "Voltar" recursivo (similares navegam a esta mesma rota).

// propsDeErro escolhe título/mensagem do estado de erro pelo status HTTP: 404
// (inexistente na fonte) × 502/503 (fonte indisponível) × demais (genérico).
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

// VoltarButton retorna no histórico — navegação recursiva (similar → similar →
// voltar) é histórico normal de rota.
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
