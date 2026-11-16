import { Link } from '@tanstack/react-router'
import { type ExternalResult } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ImportarButton, PosterImg } from '@/components/rich-detail-view'

// Visual core (poster + title + year + truncated synopsis) of an EXTERNAL result,
// shared by `ExternalResultCard` (import) and `CorrectMatchResultCard`
// (correct) — only the footer ACTION changes between the two; the poster/text and the link
// destination are identical (avoids markup duplication measured by jscpd).
//
// `internalId` CENTRALIZES on the internal one: when the external item already is an
// entity of the library, the link goes to `/entities/{id}` (INTERNAL, not /external).
// Without it, the link goes to the rich external detail screen.
export function ExternalResultContent({
  result,
  internalId = null,
}: {
  result: ExternalResult
  internalId?: string | null
}) {
  const conteudo = (
    <>
      <PosterImg
        src={result.poster_url}
        alt={`Pôster de ${result.title}`}
        className="w-20 shrink-0"
      />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <div className="flex items-baseline gap-2">
          <span className="line-clamp-1 font-medium group-hover:underline">
            {result.title}
          </span>
          {result.year ? (
            <span className="shrink-0 text-xs text-muted-foreground">
              {result.year}
            </span>
          ) : null}
        </div>
        {result.overview ? (
          <p className="line-clamp-3 text-xs text-muted-foreground">
            {result.overview}
          </p>
        ) : null}
      </div>
    </>
  )

  return internalId != null ? (
    <Link
      to="/entities/$id"
      params={{ id: internalId }}
      className="group flex gap-3"
    >
      {conteudo}
    </Link>
  ) : (
    <Link
      to="/external/$source/$type/$id"
      params={{
        source: result.source,
        type: result.type,
        id: result.source_id,
      }}
      className="group flex gap-3"
    >
      {conteudo}
    </Link>
  )
}

// Card of an EXTERNAL result ("not saved") in the IMPORT search: it reuses the visual
// core (`ExternalResultContent`) and adds the Importar button as a shortcut, outside
// the anchor (a button does not nest in a link). When `internalId` exists, the item already is
// an entity of the library — it shows the "Na biblioteca" badge and does NOT offer Importar (it avoids
// reimporting what is already cataloged).
export function ExternalResultCard({
  result,
  internalId = null,
}: {
  result: ExternalResult
  internalId?: string | null
}) {
  return (
    <li className="flex flex-col gap-2 rounded-xl bg-card p-3 ring-1 ring-foreground/10">
      <ExternalResultContent result={result} internalId={internalId} />
      {internalId != null ? (
        <Badge variant="secondary" className="w-fit">
          Na biblioteca
        </Badge>
      ) : (
        <ImportarButton
          source={result.source}
          sourceId={result.source_id}
          type={result.type}
          size="sm"
        />
      )}
    </li>
  )
}

// Card of an EXTERNAL result in the CORRECT search (Review queue): same
// visual core as `ExternalResultCard`, but the action is "Usar este" (it calls
// `correctMatch` via `onEscolher`), not Importar. Unlike the import flow,
// `internalId` does NOT hide the action — it is precisely the duplicate merge case:
// the "Na biblioteca" badge appears ALONGSIDE the button, never in its place.
export function CorrectMatchResultCard({
  result,
  internalId = null,
  pendente = false,
  onEscolher,
}: {
  result: ExternalResult
  internalId?: string | null
  pendente?: boolean
  onEscolher: (r: ExternalResult) => void
}) {
  return (
    <li className="flex flex-col gap-2 rounded-xl bg-card p-3 ring-1 ring-foreground/10">
      <ExternalResultContent result={result} internalId={internalId} />
      <div className="flex flex-wrap items-center gap-2">
        {internalId != null ? (
          <Badge variant="secondary" className="w-fit">
            Na biblioteca
          </Badge>
        ) : null}
        <Button
          size="sm"
          disabled={pendente}
          onClick={() => onEscolher(result)}
        >
          Usar este
        </Button>
      </div>
    </li>
  )
}
