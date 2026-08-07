import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import {
  ChevronDown,
  CloudDownload,
  FileText,
  ImageDown,
  Images,
  Pencil,
  RefreshCw,
  Settings,
  Sparkles,
  Trash2,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {
  ApiError,
  downloadMissingArtwork,
  enrichEntity,
  enrichMissing,
  materializeMissingMedia,
  purgeEntity,
  refreshEntityFiles,
  writeEntityNfo,
  type DownloadMissingArtworkResult,
  type EnrichMissingResult,
  type MaterializeMissingMediaResult,
  type RefreshFilesResult,
} from '@/lib/api'
import { rotuloKind } from '@/lib/artwork'
import {
  precisaAtencaoCatalogacao,
  type CatalogCompleteness,
} from '@/lib/catalog-completeness-badges'

// EntityManageMenu (briefing 0086, ex-`EntityActionButtons` do 0059) compacta as
// ações do item num ÚNICO dropdown "Gerenciar" — Editar (sempre) + "Baixar
// informações faltando"/"Baixar arte faltando" (0056/0048-0049, condicionais a
// `catalog_completeness`) + "Salvar informações (NFO)" (0062) + "Verificar
// Atualizações de Arquivos"/"Baixar Metadados"/"Baixar Artes Faltantes" (0088,
// SEMPRE visíveis, ações forçadas que ignoram cache/incrementalidade) + "Excluir
// coleção" (0026, só `isCollection`, migrado de `entity-detail.tsx::
// ExcluirColecao`). A LÓGICA de cada mutation é a MESMA de antes — só a
// apresentação virou itens de menu em vez de botões soltos acima do hero.
// Nenhum modal de "escolher candidato": "flagged" só linka para a fila de
// Revisão já existente (mesma decisão do 0059).

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

// mensagemVerificarArquivos resolve o desfecho de POST .../refresh-files (0088):
// formato solto e informativo (o próprio contrato não é rígido) — arquivos
// reconferidos, assets locais atualizados e campos locais de reprodução
// preenchidos a partir do .nfo, quando houver algum.
function mensagemVerificarArquivos(resultado: RefreshFilesResult) {
  const partes = [`${resultado.files_rechecked} arquivo(s) reconferido(s)`]
  if (resultado.assets_found > 0) {
    partes.push(`${resultado.assets_found} asset(s) local(is) atualizado(s)`)
  }
  if (resultado.playback_fields_filled.length > 0) {
    partes.push(
      `campos de reprodução preenchidos: ${resultado.playback_fields_filled.join(', ')}`,
    )
  }
  return (
    <span role="status" className="text-sm text-muted-foreground">
      {partes.join(' — ')}.
    </span>
  )
}

// mensagemArtesFaltantes resolve o desfecho de POST .../materialize-missing-media
// (0088): reusa `montarListaBaixados` (mesma frase PT-BR da galeria/lightbox) para
// os kinds baixados, mais o .nfo quando gravado nesta chamada.
function mensagemArtesFaltantes(resultado: MaterializeMissingMediaResult) {
  if (resultado.artes_baixadas.length > 0) {
    return (
      <span role="status" className="text-sm text-muted-foreground">
        {montarListaBaixados(resultado.artes_baixadas)}
        {resultado.nfo_gravado ? ' O .nfo também foi gravado.' : ''}
      </span>
    )
  }
  if (resultado.nfo_gravado) {
    return (
      <span role="status" className="text-sm text-muted-foreground">
        Nenhuma arte nova baixada — o .nfo foi gravado.
      </span>
    )
  }
  return (
    <span role="status" className="text-sm text-muted-foreground">
      Nenhuma arte nova para materializar.
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

export function EntityManageMenu({
  id,
  catalogCompleteness,
  owned = true,
  isCollection = false,
}: {
  id: string
  catalogCompleteness: CatalogCompleteness
  // Posse (`ResolvedEntity.owned`, `EXISTS media_file present`): sem isso não há
  // pasta de destino na biblioteca real para o .nfo (briefing 0062). Default
  // `true` — compat com quem não passa a prop.
  owned?: boolean
  // Só coleção (briefing 0086) ganha o item destrutivo "Excluir coleção" — filme/
  // série/temporada/episódio não têm essa ação (purga é só de coleção hoje).
  isCollection?: boolean
}) {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  // Estado do diálogo de confirmação de "Excluir coleção": um item de dropdown
  // fecha o menu ao ser selecionado (Radix, correto para acessibilidade) — a
  // confirmação de 2 cliques que existia INLINE dentro da mesma área não cabe
  // mais lá; abre este modal separado em vez de mutar direto.
  const [alertaAberto, setAlertaAberto] = useState(false)

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
  // Excluir coleção (migrado de `entity-detail.tsx::ExcluirColecao`, briefing
  // 0026): DELETE /entities/{id} — purga com CASCADE das relações, os filmes
  // membros NÃO são apagados. No sucesso, invalida a lista e volta para a
  // biblioteca (mesmo comportamento de sempre).
  const excluir = useMutation({
    mutationFn: () => purgeEntity(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['entities'] })
      navigate({ to: '/' })
    },
  })

  // As 3 ações abaixo (briefing 0088) são SEMPRE VISÍVEIS — diferente de
  // `enriquecer`/`baixarArte` acima, não dependem de `catalog_completeness`. Cada
  // uma reusa `invalidar()`: como `entityFilesQueryOptions` (queries.ts) usa a
  // chave `['entity', id, 'files']`, ela já cai sob o prefixo `['entity', id]`
  // invalidado abaixo — sem precisar de uma chave extra.
  //
  // "Verificar Atualizações de Arquivos": re-hash forçado + asset local novo +
  // campos locais de reprodução a partir do .nfo, restrito a esta entity.
  const verificarArquivos = useMutation({
    mutationFn: () => refreshEntityFiles(id),
    onSuccess: invalidar,
  })
  // "Baixar Metadados": mesmo endpoint de sempre (`enrich`), mas com
  // `force: true` pra pular o cache de 24h do detalhe externo.
  const baixarMetadados = useMutation({
    mutationFn: () => enrichEntity(id, { force: true }),
    onSuccess: invalidar,
  })
  // "Baixar Artes Faltantes": nova consulta ao(s) ArteProvider + materialização
  // do que faltar na pasta real — mesmo efeito do job `midias_faltantes`, sob
  // demanda e só para esta entity.
  const baixarArtesFaltantes = useMutation({
    mutationFn: () => materializeMissingMedia(id),
    onSuccess: invalidar,
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
  // Mesma lógica de erro do `ExcluirColecao` original (sem fallback genérico
  // para erro não-ApiError) — migrada tal qual.
  const erroExcluir =
    excluir.error instanceof ApiError ? excluir.error.message : null
  const erroVerificarArquivos =
    verificarArquivos.error instanceof ApiError
      ? verificarArquivos.error.message
      : verificarArquivos.isError
        ? 'Não foi possível reconferir os arquivos.'
        : null
  const erroBaixarMetadados =
    baixarMetadados.error instanceof ApiError
      ? baixarMetadados.error.message
      : baixarMetadados.isError
        ? 'Não foi possível baixar os metadados.'
        : null
  const erroBaixarArtesFaltantes =
    baixarArtesFaltantes.error instanceof ApiError
      ? baixarArtesFaltantes.error.message
      : baixarArtesFaltantes.isError
        ? 'Não foi possível baixar as artes faltantes.'
        : null

  return (
    <div className="flex flex-col items-end gap-1.5">
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="secondary" size="sm">
            <Settings aria-hidden />
            Gerenciar
            <ChevronDown aria-hidden />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem asChild>
            <Link to="/entities/$id/edit" params={{ id }}>
              <Pencil aria-hidden />
              Editar
            </Link>
          </DropdownMenuItem>
          {mostrarInformacoes ? (
            <DropdownMenuItem
              disabled={enriquecer.isPending}
              onSelect={() => enriquecer.mutate()}
            >
              <Sparkles aria-hidden />
              {enriquecer.isPending
                ? 'Buscando…'
                : 'Baixar informações faltando'}
            </DropdownMenuItem>
          ) : null}
          {mostrarArte ? (
            <DropdownMenuItem
              disabled={baixarArte.isPending}
              onSelect={() => baixarArte.mutate()}
            >
              <ImageDown aria-hidden />
              {baixarArte.isPending ? 'Baixando…' : 'Baixar arte faltando'}
            </DropdownMenuItem>
          ) : null}
          <DropdownMenuItem
            disabled={salvarNfo.isPending || !owned}
            title={owned ? undefined : TOOLTIP_SEM_PASTA}
            onSelect={() => salvarNfo.mutate()}
          >
            <FileText aria-hidden />
            {salvarNfo.isPending ? 'Gravando…' : 'Salvar informações (NFO)'}
          </DropdownMenuItem>
          {/* 3 ações SEMPRE VISÍVEIS (briefing 0088) — não dependem de
              `catalog_completeness`, diferente dos 2 itens condicionais acima. */}
          <DropdownMenuSeparator />
          <DropdownMenuItem
            disabled={verificarArquivos.isPending}
            onSelect={() => verificarArquivos.mutate()}
          >
            <RefreshCw aria-hidden />
            {verificarArquivos.isPending
              ? 'Verificando…'
              : 'Verificar Atualizações de Arquivos'}
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={baixarMetadados.isPending}
            onSelect={() => baixarMetadados.mutate()}
          >
            <CloudDownload aria-hidden />
            {baixarMetadados.isPending ? 'Baixando…' : 'Baixar Metadados'}
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={baixarArtesFaltantes.isPending}
            onSelect={() => baixarArtesFaltantes.mutate()}
          >
            <Images aria-hidden />
            {baixarArtesFaltantes.isPending
              ? 'Baixando…'
              : 'Baixar Artes Faltantes'}
          </DropdownMenuItem>
          {isCollection ? (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                variant="destructive"
                onSelect={() => setAlertaAberto(true)}
              >
                <Trash2 aria-hidden />
                Excluir coleção
              </DropdownMenuItem>
            </>
          ) : null}
        </DropdownMenuContent>
      </DropdownMenu>

      {/* O rótulo "Buscando…"/"Baixando…"/"Gravando…" mora no PRÓPRIO item do
          menu (linha do trigger acima) — mas o item some do DOM assim que o
          dropdown fecha (Radix fecha ao selecionar, comportamento padrão
          mantido de propósito, §"Convenções"). Sem um eco aqui embaixo, o
          clique pareceria não ter feito nada até a mutation resolver — por
          isso o PENDING também aparece como status inline, junto do
          resultado/erro (mesmo texto do item, só que sempre visível). */}
      {enriquecer.isPending ? (
        <span role="status" className="text-sm text-muted-foreground">
          Buscando…
        </span>
      ) : erroEnriquecer ? (
        <span role="alert" className="text-sm font-medium text-destructive">
          {erroEnriquecer}
        </span>
      ) : enriquecer.isSuccess ? (
        mensagemEnriquecimento(enriquecer.data)
      ) : null}
      {baixarArte.isPending ? (
        <span role="status" className="text-sm text-muted-foreground">
          Baixando…
        </span>
      ) : erroArte ? (
        <span role="alert" className="text-sm font-medium text-destructive">
          {erroArte}
        </span>
      ) : baixarArte.isSuccess ? (
        mensagemArte(baixarArte.data)
      ) : null}
      {salvarNfo.isPending ? (
        <span role="status" className="text-sm text-muted-foreground">
          Gravando…
        </span>
      ) : erroNfo ? (
        <span role="alert" className="text-sm font-medium text-destructive">
          {erroNfo}
        </span>
      ) : salvarNfo.isSuccess ? (
        <span role="status" className="text-sm text-muted-foreground">
          .nfo gravado em {salvarNfo.data.nfo_path}
        </span>
      ) : null}
      {verificarArquivos.isPending ? (
        <span role="status" className="text-sm text-muted-foreground">
          Verificando…
        </span>
      ) : erroVerificarArquivos ? (
        <span role="alert" className="text-sm font-medium text-destructive">
          {erroVerificarArquivos}
        </span>
      ) : verificarArquivos.isSuccess ? (
        mensagemVerificarArquivos(verificarArquivos.data)
      ) : null}
      {baixarMetadados.isPending ? (
        <span role="status" className="text-sm text-muted-foreground">
          Baixando…
        </span>
      ) : erroBaixarMetadados ? (
        <span role="alert" className="text-sm font-medium text-destructive">
          {erroBaixarMetadados}
        </span>
      ) : baixarMetadados.isSuccess ? (
        <span role="status" className="text-sm text-muted-foreground">
          Metadados atualizados.
        </span>
      ) : null}
      {baixarArtesFaltantes.isPending ? (
        <span role="status" className="text-sm text-muted-foreground">
          Baixando…
        </span>
      ) : erroBaixarArtesFaltantes ? (
        <span role="alert" className="text-sm font-medium text-destructive">
          {erroBaixarArtesFaltantes}
        </span>
      ) : baixarArtesFaltantes.isSuccess ? (
        mensagemArtesFaltantes(baixarArtesFaltantes.data)
      ) : null}

      {isCollection ? (
        <AlertDialog open={alertaAberto} onOpenChange={setAlertaAberto}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Excluir coleção?</AlertDialogTitle>
              <AlertDialogDescription>
                Os filmes MEMBROS não são apagados — só a coleção em si (e suas
                relações com os membros) é removida.
              </AlertDialogDescription>
            </AlertDialogHeader>
            {erroExcluir ? (
              <span
                role="alert"
                className="text-sm font-medium text-destructive"
              >
                {erroExcluir}
              </span>
            ) : null}
            <AlertDialogFooter>
              <AlertDialogCancel disabled={excluir.isPending}>
                Cancelar
              </AlertDialogCancel>
              <AlertDialogAction
                disabled={excluir.isPending}
                onClick={(e) => {
                  // preventDefault: o fechamento automático do AlertDialogAction
                  // fecharia o modal ANTES da mutation resolver — controlamos o
                  // `open` manualmente (fica aberto em erro, mostrando a
                  // mensagem; em sucesso o `navigate` desmonta a tela inteira).
                  e.preventDefault()
                  excluir.mutate()
                }}
              >
                {excluir.isPending ? 'Excluindo…' : 'Excluir coleção'}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      ) : null}
    </div>
  )
}
