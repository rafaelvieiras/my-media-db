import { Link } from '@tanstack/react-router'
import { type ExternalResult } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ImportarButton, PosterImg } from '@/components/rich-detail-view'

// Miolo visual (pôster + título + ano + sinopse truncada) de um resultado EXTERNO,
// compartilhado por `ExternalResultCard` (importar) e `CorrectMatchResultCard`
// (corrigir) — só a AÇÃO do rodapé muda entre os dois; o pôster/texto e o destino
// do link são idênticos (evita duplicação de markup medida pelo jscpd).
//
// `internalId` (briefing 0026) CENTRALIZA no interno: quando o item externo já é uma
// entity da biblioteca, o link vai para `/entities/{id}` (INTERNO, não /external).
// Sem ele, o link segue para a tela rica de detalhe do externo (0023).
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

// Card de um resultado EXTERNO ("não salvo") na busca de IMPORTAR: reusa o miolo
// visual (`ExternalResultContent`) e acrescenta o botão Importar como atalho, fora
// da âncora (botão não aninha em link). Quando `internalId` existe, o item já é
// entity da biblioteca — mostra o selo "Na biblioteca" e NÃO oferece Importar (evita
// reimportar o que já está catalogado).
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

// Card de um resultado EXTERNO na busca de CORRIGIR (fila de Revisão, 0074): mesmo
// miolo visual de `ExternalResultCard`, mas a ação é "Usar este" (chama
// `correctMatch` via `onEscolher`), não Importar. Diferente do fluxo de importar,
// `internalId` NÃO esconde a ação — é justamente o caso de fundir duplicata (0051):
// o selo "Na biblioteca" aparece JUNTO do botão, nunca no lugar dele.
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
