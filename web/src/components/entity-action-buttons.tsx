import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { FileText, ImageDown, Pencil, Sparkles } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  ApiError,
  downloadMissingArtwork,
  enrichMissing,
  writeEntityNfo,
  type DownloadMissingArtworkResult,
  type EnrichMissingResult,
} from '@/lib/api'
import { rotuloKind } from '@/lib/artwork'
import {
  precisaAtencaoCatalogacao,
  type CatalogCompleteness,
} from '@/lib/catalog-completeness-badges'

// EntityActionButtons (briefing 0059) substitui o botão solto "Editar" no topo
// do detalhe por um GRUPO de ações rápidas: Editar (sempre) + "Baixar
// informações faltando" (0056 sob demanda, só este item) + "Baixar arte
// faltando" (0048/0049 sob demanda), os dois novos condicionais ao que
// `catalog_completeness` (0058) diz estar faltando. Nenhum modal de "escolher
// candidato" — decisão explícita do briefing (§3/§6): "flagged" só linka para
// a fila de Revisão já existente.

// montarListaBaixados monta a frase PT-BR "Pôster e fanart baixados." a partir
// dos kinds baixados (rótulo via `rotuloKind`, mesma fonte de verdade da
// galeria/lightbox — 0048/0049), com vírgula entre os itens e "e" antes do
// último (concatenação PT-BR simples; não há um helper genérico assim no
// projeto ainda).
function montarListaBaixados(kinds: string[]): string {
  const rotulos = kinds.map((kind) => rotuloKind(kind).toLowerCase())
  const lista =
    rotulos.length <= 1
      ? (rotulos[0] ?? '')
      : `${rotulos.slice(0, -1).join(', ')} e ${rotulos[rotulos.length - 1]}`
  return `${lista.charAt(0).toUpperCase()}${lista.slice(1)} baixados.`
}

// mensagemEnriquecimento resolve o desfecho de POST .../enrich-missing (§4):
// aplicado (com o método) × sinalizado (link para /review) × nada encontrado
// (neutro) — nenhum dos três é erro; erro de rede/500 é tratado à parte.
function mensagemEnriquecimento(resultado: EnrichMissingResult) {
  if (resultado.outcome === 'applied') {
    return (
      <span role="status" className="text-sm text-muted-foreground">
        Aplicado —{' '}
        {resultado.method === 'local'
          ? 'ligado a um item já catalogado'
          : 'enriquecido via provider'}
        .
      </span>
    )
  }
  if (resultado.outcome === 'flagged') {
    return (
      <span role="status" className="text-sm text-muted-foreground">
        Não encontramos uma correspondência confiável — o item foi enviado para
        a fila de <Link to="/dashboard/review">Revisão</Link>.
      </span>
    )
  }
  return (
    <span role="status" className="text-sm text-muted-foreground">
      Nenhuma correspondência encontrada desta vez.
    </span>
  )
}

// mensagemArte resolve o desfecho de POST .../download-missing-artwork (§4):
// baixou pelo menos um kind × nenhuma opção disponível — idempotente (nada
// faltando também cai no "nenhuma opção", no-op silencioso não muda a UI).
function mensagemArte(resultado: DownloadMissingArtworkResult) {
  if (resultado.downloaded.length > 0) {
    return (
      <span role="status" className="text-sm text-muted-foreground">
        {montarListaBaixados(resultado.downloaded)}
      </span>
    )
  }
  if (resultado.unavailable.length > 0) {
    return (
      <span role="status" className="text-sm text-muted-foreground">
        Nenhuma arte disponível nos provedores para o que falta.
      </span>
    )
  }
  return null
}

// TOOLTIP_SEM_PASTA explica por que "Salvar informações (NFO)" fica desabilitado
// quando a entity não tem nenhum media_file presente — mesma condição usada pela
// galeria de artes (0062) para "salvar na pasta"/"definir como principal".
const TOOLTIP_SEM_PASTA =
  'Este item não tem um arquivo na biblioteca — sem pasta de destino para gravar o .nfo.'

export function EntityActionButtons({
  id,
  catalogCompleteness,
  owned = true,
}: {
  id: string
  catalogCompleteness: CatalogCompleteness
  // Posse (`ResolvedEntity.owned`, `EXISTS media_file present`): sem isso não há
  // pasta de destino na biblioteca real para o .nfo (briefing 0062). Default
  // `true` — compat com quem não passa a prop.
  owned?: boolean
}) {
  const queryClient = useQueryClient()

  // Invalidação compartilhada pelos botões novos (§4): a tela (entity),
  // a listagem (entities) e a galeria (artwork) refletem o resultado sem F5.
  const invalidar = () => {
    queryClient.invalidateQueries({ queryKey: ['entity', id] })
    queryClient.invalidateQueries({ queryKey: ['entities'] })
    queryClient.invalidateQueries({ queryKey: ['artwork', id] })
  }

  const enriquecer = useMutation({
    mutationFn: () => enrichMissing(id),
    onSuccess: invalidar,
  })
  const baixarArte = useMutation({
    mutationFn: () => downloadMissingArtwork(id),
    onSuccess: invalidar,
  })
  // Salvar informações (NFO, briefing 0062): idempotente — sempre sobrescreve. Não
  // precisa invalidar `artwork`/`entities` (não mexe em arte nem nos campos que o
  // grid mostra), mas invalida a entity: `nfo_path`/`nfo_written_at` mudam.
  const salvarNfo = useMutation({
    mutationFn: () => writeEntityNfo(id),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ['entity', id] }),
  })

  const mostrarInformacoes = precisaAtencaoCatalogacao(catalogCompleteness)
  const mostrarArte = !!catalogCompleteness?.missing.some((chave) =>
    chave.startsWith('artwork:'),
  )

  const erroEnriquecer =
    enriquecer.error instanceof ApiError
      ? enriquecer.error.message
      : enriquecer.isError
        ? 'Não foi possível buscar informações externas.'
        : null
  const erroArte =
    baixarArte.error instanceof ApiError
      ? baixarArte.error.message
      : baixarArte.isError
        ? 'Não foi possível baixar a arte faltante.'
        : null
  const erroNfo =
    salvarNfo.error instanceof ApiError
      ? salvarNfo.error.message
      : salvarNfo.isError
        ? 'Não foi possível gravar o .nfo.'
        : null

  return (
    <div className="flex flex-col items-end gap-1.5">
      <div className="flex flex-wrap items-center gap-2">
        <Button asChild variant="outline" size="sm">
          <Link to="/entities/$id/edit" params={{ id }}>
            <Pencil aria-hidden />
            Editar
          </Link>
        </Button>
        {mostrarInformacoes ? (
          <Button
            variant="outline"
            size="sm"
            disabled={enriquecer.isPending}
            onClick={() => enriquecer.mutate()}
          >
            <Sparkles aria-hidden />
            {enriquecer.isPending ? 'Buscando…' : 'Baixar informações faltando'}
          </Button>
        ) : null}
        {mostrarArte ? (
          <Button
            variant="outline"
            size="sm"
            disabled={baixarArte.isPending}
            onClick={() => baixarArte.mutate()}
          >
            <ImageDown aria-hidden />
            {baixarArte.isPending ? 'Baixando…' : 'Baixar arte faltando'}
          </Button>
        ) : null}
        <Button
          variant="outline"
          size="sm"
          disabled={salvarNfo.isPending || !owned}
          title={owned ? undefined : TOOLTIP_SEM_PASTA}
          onClick={() => salvarNfo.mutate()}
        >
          <FileText aria-hidden />
          {salvarNfo.isPending ? 'Gravando…' : 'Salvar informações (NFO)'}
        </Button>
      </div>
      {erroEnriquecer ? (
        <span role="alert" className="text-sm font-medium text-destructive">
          {erroEnriquecer}
        </span>
      ) : enriquecer.isSuccess ? (
        mensagemEnriquecimento(enriquecer.data)
      ) : null}
      {erroArte ? (
        <span role="alert" className="text-sm font-medium text-destructive">
          {erroArte}
        </span>
      ) : baixarArte.isSuccess ? (
        mensagemArte(baixarArte.data)
      ) : null}
      {erroNfo ? (
        <span role="alert" className="text-sm font-medium text-destructive">
          {erroNfo}
        </span>
      ) : salvarNfo.isSuccess ? (
        <span role="status" className="text-sm text-muted-foreground">
          .nfo gravado em {salvarNfo.data.nfo_path}
        </span>
      ) : null}
    </div>
  )
}
