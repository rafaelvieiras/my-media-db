import { Link } from '@tanstack/react-router'
import { rotuloTipo } from '@/lib/entity-badges'
import type { AncestorRef } from '@/lib/api'

// Migalhas: the ASCENDING breadcrumb of the series tree. It gives context and
// navigation for the nodes with no place on the home/search: inside a season or an
// episode, it shows the trail up to the series and allows going up the tree.
//
// It receives the ANCESTORS already in ROOT→PARENT order (as the backend serves them in
// `ResolvedEntity.ancestors`: `[série]` for a season; `[série, temporada]` for an
// episode) and the CURRENT item. Each ancestor becomes a <Link> to `/entities/{id}`; the
// current item CLOSES the trail without a link (it is the current state). It follows the spirit of
// `ColecaoBand` (navigation to the parent reusing the theme tokens).
//
// Empty for a top-level work (movie/tv/collection): `ancestors` arrives as `[]` and the
// component renders nothing — the screen keeps only "Voltar à biblioteca".
export function Migalhas({
  ancestors,
  atual,
}: {
  ancestors: AncestorRef[]
  atual: { title: string; type: string }
}) {
  if (ancestors.length === 0) {
    return null
  }
  return (
    <nav aria-label="Trilha da série" className="min-w-0">
      <ol className="flex flex-wrap items-center gap-1.5 text-sm text-muted-foreground">
        {ancestors.map((no) => (
          <li key={no.id} className="flex items-center gap-1.5">
            <Link
              to="/entities/$id"
              params={{ id: no.id }}
              title={rotuloTipo(no.type)}
              className="max-w-[16rem] truncate hover:text-foreground hover:underline"
            >
              {no.title}
            </Link>
            <span aria-hidden className="text-muted-foreground/60">
              ›
            </span>
          </li>
        ))}
        <li
          aria-current="page"
          className="max-w-[16rem] truncate font-medium text-foreground"
          title={rotuloTipo(atual.type)}
        >
          {atual.title}
        </li>
      </ol>
    </nav>
  )
}
