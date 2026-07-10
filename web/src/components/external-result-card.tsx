import { Link } from '@tanstack/react-router'
import { type ExternalResult } from '@/lib/api'
import { ImportarButton, PosterImg } from '@/components/rich-detail-view'

// Card de um resultado EXTERNO ("não salvo"): pôster, título, ano, sinopse truncada
// e o botão Importar. O pôster+texto LINKAM para a tela rica de detalhe do externo
// (0023), que abre o item por inteiro ANTES de importar; o botão Importar segue como
// atalho (fora da âncora — botão não aninha em link). Pôster e Importar são os
// COMPARTILHADOS da RichDetailView, então o card não duplica esse markup (jscpd).
export function ExternalResultCard({ result }: { result: ExternalResult }) {
  return (
    <li className="flex flex-col gap-2 rounded-xl bg-card p-3 ring-1 ring-foreground/10">
      <Link
        to="/external/$source/$type/$id"
        params={{
          source: result.source,
          type: result.type,
          id: result.source_id,
        }}
        className="group flex gap-3"
      >
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
      </Link>
      <ImportarButton
        source={result.source}
        sourceId={result.source_id}
        type={result.type}
        size="sm"
      />
    </li>
  )
}
