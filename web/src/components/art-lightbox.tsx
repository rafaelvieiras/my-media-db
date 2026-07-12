import { useCallback, useEffect, useRef, useState } from 'react'
import {
  ChevronLeft,
  ChevronRight,
  Download,
  Maximize2,
  Minimize2,
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
  // Definir como preferida: no pai, roteia asset local → PUT prefer; inventário →
  // POST select {prefer:true}.
  onPreferir: (item: ArtworkItem) => void
  // Baixar (materializar sem preferir): POST select {prefer:false}. Só faz sentido
  // para arte de inventário (não-local).
  onBaixar: (item: ArtworkItem) => void
  // Remover: só para asset do DONO (source==='user'); DELETE do asset.
  onRemover: (item: ArtworkItem) => void
  // Alguma mutação em voo — desabilita os botões de ação.
  ocupado: boolean
  // Mensagem de erro de uma ação (pt-BR do envelope), exibida no rodapé.
  erro: string | null
}

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
      className="fixed inset-0 z-50 flex flex-col bg-background/95 outline-none backdrop-blur-sm"
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
          {item.is_preferred ? (
            <Badge variant="secondary">preferida</Badge>
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
          {!item.is_preferred ? (
            <Button
              type="button"
              size="sm"
              disabled={acoes.ocupado}
              onClick={() => acoes.onPreferir(item)}
            >
              <Star aria-hidden />
              Definir como preferida
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
              Baixar pro disco
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
