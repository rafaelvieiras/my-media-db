import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { ImageOff } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { posterSrc, type EntityCard } from '@/lib/api'
import { cn } from '@/lib/utils'
import { ehDraft, precisaRevisao, rotuloTipo } from '@/lib/entity-badges'

// Card de pôster do grid: arte (com fallback de placeholder), título e badges de
// posse ("na biblioteca") e de revisão (baixa confiança — só sinaliza). Clicar
// abre o detalhe da entity.
export function PosterCard({ card }: { card: EntityCard }) {
  const [falhouArte, setFalhouArte] = useState(false)
  const src = posterSrc(card.poster_url)
  const mostrarPlaceholder = src === null || falhouArte

  return (
    <Link
      to="/entities/$id"
      params={{ id: card.id }}
      className="group flex flex-col gap-2"
      data-slot="poster-card"
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
        <div className="absolute top-1.5 left-1.5 flex flex-wrap gap-1">
          <PosseBadge owned={card.owned} />
          <EstadoMatchBadge card={card} />
        </div>
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
// alerta de revisão de baixa confiança. Nenhum dos dois → sem badge.
function EstadoMatchBadge({ card }: { card: EntityCard }) {
  if (ehDraft(card.status)) {
    return <Badge variant="secondary">não identificado</Badge>
  }
  if (precisaRevisao(card)) {
    return <Badge variant="destructive">revisar</Badge>
  }
  return null
}
