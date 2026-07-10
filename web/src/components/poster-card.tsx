import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { ImageOff } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { posterSrc, type EntityCard } from '@/lib/api'
import { cn } from '@/lib/utils'
import { ehDraft, precisaRevisao, rotuloTipo } from '@/lib/entity-badges'

// Card de pôster do grid: arte (com fallback de placeholder), título e badges de
// posse ("na biblioteca") e de revisão (baixa confiança / não identificado).
// Clicar na arte/título abre o detalhe da entity; clicar no badge de revisão
// leva à fila de curadoria (`/review`). Os badges ficam num overlay IRMÃO do
// Link do card (não aninhado) para não gerar âncora dentro de âncora — o overlay
// é `pointer-events-none` e só o badge-link reativa o clique.
export function PosterCard({ card }: { card: EntityCard }) {
  const [falhouArte, setFalhouArte] = useState(false)
  const src = posterSrc(card.poster_url)
  const mostrarPlaceholder = src === null || falhouArte

  return (
    <div className="group relative flex flex-col gap-2" data-slot="poster-card">
      <Link
        to="/entities/$id"
        params={{ id: card.id }}
        className="flex flex-col gap-2"
      >
        <div className="relative aspect-[2/3] overflow-hidden rounded-xl bg-muted ring-1 ring-foreground/10">
          {mostrarPlaceholder ? (
            <div
              data-testid="poster-placeholder"
              className="flex h-full w-full flex-col items-center justify-center gap-1 text-muted-foreground"
            >
              <ImageOff className="size-8" aria-hidden />
              <span className="px-2 text-center text-xs">Sem arte</span>
            </div>
          ) : (
            <img
              src={src}
              alt={`Pôster de ${card.title}`}
              loading="lazy"
              onError={() => setFalhouArte(true)}
              className="h-full w-full object-cover transition-transform group-hover:scale-[1.02]"
            />
          )}
        </div>
        <div className="flex flex-col gap-0.5">
          <span className="line-clamp-2 text-sm font-medium leading-snug">
            {card.title}
          </span>
          <span className="text-xs text-muted-foreground">
            {rotuloTipo(card.type)}
          </span>
        </div>
      </Link>
      <div className="pointer-events-none absolute top-1.5 left-1.5 flex flex-wrap gap-1">
        <PosseBadge owned={card.owned} />
        <EstadoMatchBadge card={card} />
      </div>
    </div>
  )
}

function PosseBadge({ owned }: { owned: boolean }) {
  if (!owned) {
    return null
  }
  return (
    <Badge className={cn('border-transparent bg-emerald-600 text-white')}>
      na biblioteca
    </Badge>
  )
}

// Badge de estado do match: draft (não identificado) tem precedência sobre o
// alerta de revisão de baixa confiança. Nenhum dos dois → sem badge. Ambos são
// LINK para a fila de revisão (`/review`) — o sinal da Fase 2 agora leva à ação
// da Fase 3. `pointer-events-auto` reativa o clique dentro do overlay inerte.
function EstadoMatchBadge({ card }: { card: EntityCard }) {
  if (ehDraft(card.status)) {
    return (
      <Badge asChild variant="secondary" className="pointer-events-auto">
        <Link to="/review">não identificado</Link>
      </Badge>
    )
  }
  if (precisaRevisao(card)) {
    return (
      <Badge asChild variant="destructive" className="pointer-events-auto">
        <Link to="/review">revisar</Link>
      </Badge>
    )
  }
  return null
}
