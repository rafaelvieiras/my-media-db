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

// Rota `/external/$source/$type/$id`: o item externo por inteiro ANTES de importar
// (briefing 0023). Consome o detalhe rico e persistente do 0022 no idioma ativo —
// trocar o idioma (topo) re-busca os campos traduzíveis (o `lang` entra na queryKey).
// Reusa a RichDetailView COMPARTILHADA com o detalhe da entity; o específico daqui é
// o botão Importar e o "Voltar" recursivo (similares navegam a esta mesma rota).
//
// `type==='album'` (briefing 0115 Parte 2) é um shape TOTALMENTE diferente (faixas
// do release, não um item único) — delega a ExternalAlbumView em vez de montar o
// RichDetailView, sem precisar de rota própria (o backend também reusa o mesmo
// segmento "album" com precedência sobre {type}, ver server.go). ExternalDetailScreen
// só decide QUAL sub-tela renderizar; cada uma chama seus próprios hooks (regra dos
// hooks preservada — nenhum hook condicional dentro do mesmo componente).

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

// ExternalItemView é o detalhe rico ANTES de importar de um item de UM tipo
// (movie/tv/collection — nunca album, ver o branch acima): extraído do corpo
// original de ExternalDetailScreen para manter a regra dos hooks sã quando
// `type==='album'` delega a outro componente sem chamar este hook.
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
