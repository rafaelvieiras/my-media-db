import { Link } from '@tanstack/react-router'
import { rotuloTipo } from '@/lib/entity-badges'
import type { AncestorRef } from '@/lib/api'

// Migalhas: o breadcrumb ASCENDENTE da árvore de séries (0032). Dá contexto e
// navegação para os nós sem lugar na home/busca: dentro de uma temporada ou de um
// episódio, mostra a trilha até a série e permite subir a árvore.
//
// Recebe os ANCESTRAIS já na ordem RAIZ→PAI (como o backend serve em
// `ResolvedEntity.ancestors`: `[série]` para temporada; `[série, temporada]` para
// episódio) e o item ATUAL. Cada ancestral vira um <Link> para `/entities/{id}`; o
// item atual FECHA a trilha sem link (é o estado corrente). Segue o espírito do
// `ColecaoBand` (navegação para o pai reusando os tokens do tema).
//
// Vazio para obra de topo (movie/tv/collection): `ancestors` chega `[]` e o
// componente não renderiza nada — a tela mantém só o "Voltar à biblioteca".
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
