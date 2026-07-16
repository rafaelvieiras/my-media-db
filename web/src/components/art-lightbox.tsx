import { useCallback, useEffect, useRef, useState } from 'react'
import {
  ChevronLeft,
  ChevronRight,
  Download,
  Maximize2,
  Minimize2,
  Save,
  Star,
  Trash2,
  X,
} from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { rotuloIdioma } from '@/lib/languages'
import { infoKind, rotuloKind } from '@/lib/artwork'
import type { ArtworkItem } from '@/lib/api'

// ArtLightbox (0049): overlay quase-cheio para ver UMA arte em alta, com navegação
// prev/próxima DENTRO do tipo (setas do teclado + botões), Esc/click-no-backdrop
// para fechar, foco preso (acessibilidade) e um toggle de zoom (ajustar ↔ 100%).
// Mostra idioma/likes/origem e dispara as AÇÕES — a MUTAÇÃO mora no pai (a galeria),
// que injeta os handlers e o estado `ocupado`/`erro`; o lightbox só decide QUAIS
// botões cabem para cada item (preferir/baixar/remover, conforme local × inventário
// × dono).

export interface AcoesLightbox {
  // Definir como PRINCIPAL (briefing 0062, fonte de verdade `is_principal`): no
  // pai, cria o asset primeiro se o item ainda for só candidato do inventário
  // (sem `asset_id`), depois chama PUT prefer — que materializa (se preciso) e
  // renomeia os arquivos físicos. Um único clique encadeia as duas chamadas
  // quando necessário (jornada §2 passo 4).
  onPreferir: (item: ArtworkItem) => void
  // Salvar NA PASTA real da biblioteca (briefing 0062): cria o asset primeiro se
  // preciso, depois materializa (baixa pro cache se preciso + copia pro disco).
  // Distinto de `onBaixar` (que só cacheia internamente, sem tocar a pasta real).
  onMaterializar: (item: ArtworkItem) => void
  // Baixar pro CACHE interno (materializar sem preferir, sem tocar a pasta real):
  // POST select {prefer:false}. Só faz sentido para arte ainda não cacheada.
  onBaixar: (item: ArtworkItem) => void
  // Remover: só para asset do DONO (source==='user'); DELETE do asset.
  onRemover: (item: ArtworkItem) => void
  // Alguma mutação em voo — desabilita os botões de ação.
  ocupado: boolean
  // Posse (briefing 0062, `ResolvedEntity.owned`): sem media_file presente não há
  // pasta de destino na biblioteca real — desabilita "Salvar na pasta"/"Definir
  // como principal" com tooltip explicativo (jornada §2 passo 7). Não afeta
  // "Baixar pro cache"/"Remover" (não tocam a pasta real).
  podeSalvarNaPasta: boolean
  // Mensagem de erro de uma ação (pt-BR do envelope), exibida no rodapé.
  erro: string | null
}

// TOOLTIP_SEM_PASTA explica por que os botões de materializar ficam desabilitados
// quando a entity não tem nenhum media_file presente (só catálogo).
const TOOLTIP_SEM_PASTA =
  'Este item não tem um arquivo na biblioteca — sem pasta de destino para salvar.'

// Seletores dos elementos focáveis para o "foco preso" (trap de Tab) do dialog.
const FOCAVEIS =
  'button:not([disabled]), [href], input, [tabindex]:not([tabindex="-1"])'

export function ArtLightbox({
  kind,
  items,
  index,
  onIndex,
  onClose,
  acoes,
}: {
  kind: string
  items: ArtworkItem[]
  // Índice ATUAL dentro do tipo (controlado pelo pai — a mesma lista das linhas).
  index: number
  onIndex: (i: number) => void
  onClose: () => void
  acoes: AcoesLightbox
}) {
  const containerRef = useRef<HTMLDivElement>(null)
  // Zoom: `false` = ajustar à tela (contain); `true` = 100% (resolução natural,
  // com scroll no frame). Reseta ao trocar de arte.
  const [zoom, setZoom] = useState(false)

  const item = items[index]
  const total = items.length

  const irAnterior = useCallback(() => {
    setZoom(false)
    onIndex((index - 1 + total) % total)
  }, [index, total, onIndex])

  const irProxima = useCallback(() => {
    setZoom(false)
    onIndex((index + 1) % total)
  }, [index, total, onIndex])

  // Teclado: Esc fecha; setas navegam; Tab fica PRESO nos focáveis do dialog.
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
        return
      }
      if (e.key === 'ArrowLeft') {
        e.preventDefault()
        irAnterior()
        return
      }
      if (e.key === 'ArrowRight') {
        e.preventDefault()
        irProxima()
        return
      }
      if (e.key === 'Tab') {
        const focaveis =
          containerRef.current?.querySelectorAll<HTMLElement>(FOCAVEIS)
        if (!focaveis || focaveis.length === 0) {
          return
        }
        const primeiro = focaveis[0]
        const ultimo = focaveis[focaveis.length - 1]
        const ativo = document.activeElement
        if (e.shiftKey && ativo === primeiro) {
          e.preventDefault()
          ultimo.focus()
        } else if (!e.shiftKey && ativo === ultimo) {
          e.preventDefault()
          primeiro.focus()
        }
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [irAnterior, irProxima, onClose])

  // Foco preso: foca o dialog ao abrir e devolve o foco ao elemento anterior ao fechar.
  useEffect(() => {
    const anterior = document.activeElement as HTMLElement | null
    containerRef.current?.focus()
    return () => anterior?.focus?.()
  }, [])

  if (!item) {
    return null
  }

  const info = infoKind(kind)
  const legenda = `${rotuloKind(kind)} ${index + 1} de ${total}`

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={legenda}
      tabIndex={-1}
      ref={containerRef}
      // Backdrop: click FORA da imagem/painel fecha. Os filhos param a propagação.
      onClick={onClose}
      // Backdrop SEMPRE escuro (valoriza a arte), mesmo no tema claro: a classe `dark`
      // força os tokens shadcn (texto/botões) para claro-no-escuro no subtree do lightbox.
      className="dark fixed inset-0 z-50 flex flex-col bg-black/85 text-foreground outline-none backdrop-blur-sm"
    >
      {/* Barra do topo: legenda + zoom + fechar. */}
      <div
        className="flex items-center gap-2 p-3"
        onClick={(e) => e.stopPropagation()}
      >
        <span className="text-sm font-medium text-muted-foreground">
          {legenda}
        </span>
        <div className="ml-auto flex items-center gap-1.5">
          <Button
            type="button"
            variant="outline"
            size="sm"
            aria-label={zoom ? 'Ajustar à tela' : 'Ampliar para 100%'}
            aria-pressed={zoom}
            onClick={() => setZoom((z) => !z)}
          >
            {zoom ? <Minimize2 aria-hidden /> : <Maximize2 aria-hidden />}
            {zoom ? 'Ajustar' : '100%'}
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            aria-label="Fechar"
            onClick={onClose}
          >
            <X aria-hidden />
          </Button>
        </div>
      </div>

      {/* Palco: imagem em alta + setas de navegação. */}
      <div className="relative flex min-h-0 flex-1 items-center justify-center px-2">
        {total > 1 ? (
          <Button
            type="button"
            variant="outline"
            size="icon"
            aria-label="Arte anterior"
            className="absolute left-3 z-10"
            onClick={(e) => {
              e.stopPropagation()
              irAnterior()
            }}
          >
            <ChevronLeft aria-hidden />
          </Button>
        ) : null}

        <div
          className={`flex h-full w-full items-center justify-center ${
            zoom ? 'overflow-auto' : ''
          } ${info.contain ? 'bg-muted/40' : ''}`}
          onClick={(e) => e.stopPropagation()}
        >
          <img
            key={item.url}
            src={item.url}
            alt={`${rotuloKind(kind)} — arte ${index + 1}`}
            className={
              zoom ? 'max-w-none' : 'max-h-full max-w-full object-contain'
            }
          />
        </div>

        {total > 1 ? (
          <Button
            type="button"
            variant="outline"
            size="icon"
            aria-label="Próxima arte"
            className="absolute right-3 z-10"
            onClick={(e) => {
              e.stopPropagation()
              irProxima()
            }}
          >
            <ChevronRight aria-hidden />
          </Button>
        ) : null}
      </div>

      {/* Rodapé: metadados (idioma/likes/origem/preferida) + ações. */}
      <div
        className="flex flex-col gap-2 border-t border-foreground/10 p-3"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex flex-wrap items-center gap-1.5">
          <Badge variant="outline">{rotuloKind(kind)}</Badge>
          {item.is_principal ? (
            <Badge variant="secondary">principal</Badge>
          ) : null}
          {item.is_local_in_library ? (
            <Badge variant="outline">salvo na pasta</Badge>
          ) : null}
          {item.lang ? (
            <Badge variant="secondary">{rotuloIdioma(item.lang)}</Badge>
          ) : (
            <Badge variant="outline">sem texto</Badge>
          )}
          {item.likes ? (
            <span className="inline-flex items-center gap-1 text-sm text-muted-foreground">
              <Star className="size-4" aria-hidden />
              {item.likes}
            </span>
          ) : null}
          <Badge variant="outline">{item.source}</Badge>
          {item.disc_type ? (
            <Badge variant="outline">{item.disc_type}</Badge>
          ) : null}
          {item.season ? (
            <Badge variant="outline">Temporada {item.season}</Badge>
          ) : null}
        </div>

        <div className="flex flex-wrap items-center gap-1.5">
          {!item.is_principal ? (
            <Button
              type="button"
              size="sm"
              disabled={acoes.ocupado || !acoes.podeSalvarNaPasta}
              title={acoes.podeSalvarNaPasta ? undefined : TOOLTIP_SEM_PASTA}
              onClick={() => acoes.onPreferir(item)}
            >
              <Star aria-hidden />
              Definir como principal
            </Button>
          ) : null}
          {!item.is_local_in_library ? (
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={acoes.ocupado || !acoes.podeSalvarNaPasta}
              title={acoes.podeSalvarNaPasta ? undefined : TOOLTIP_SEM_PASTA}
              onClick={() => acoes.onMaterializar(item)}
            >
              <Save aria-hidden />
              Salvar na pasta
            </Button>
          ) : null}
          {!item.is_local ? (
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={acoes.ocupado}
              onClick={() => acoes.onBaixar(item)}
            >
              <Download aria-hidden />
              Baixar pro cache
            </Button>
          ) : null}
          {item.source === 'user' && item.asset_id ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              aria-label="Remover arte"
              disabled={acoes.ocupado}
              onClick={() => acoes.onRemover(item)}
            >
              <Trash2 aria-hidden />
              Remover
            </Button>
          ) : null}
        </div>

        {acoes.erro ? (
          <p role="alert" className="text-sm font-medium text-destructive">
            {acoes.erro}
          </p>
        ) : null}
      </div>
    </div>
  )
}
