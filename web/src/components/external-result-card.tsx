import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Download, ImageOff, LoaderCircle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  importExternal,
  ApiError,
  type ExternalResult,
  type ImportRequest,
} from '@/lib/api'

// Card de um resultado EXTERNO ("não salvo"): pôster (URL direta da fonte, com
// placeholder quando ausente/quebrada), título, ano, sinopse truncada e o botão
// Importar. Cada card é dono da própria mutation, então só o item importado
// desabilita/gira — os demais seguem clicáveis.
export function ExternalResultCard({ result }: { result: ExternalResult }) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [falhouArte, setFalhouArte] = useState(false)
  const mostrarPlaceholder = !result.poster_url || falhouArte

  const importMutation = useMutation({
    mutationFn: () =>
      importExternal({
        source: result.source,
        source_id: result.source_id,
        // O tipo do resultado externo é `string` (contrato); o import exige o
        // enum canônico. A busca só devolve `movie`/`tv`, então a coerção é segura.
        type: result.type as ImportRequest['type'],
      }),
    onSuccess: (entity) => {
      queryClient.invalidateQueries({ queryKey: ['entities'] })
      navigate({ to: '/entities/$id', params: { id: entity.id } })
    },
  })

  const mensagemErro =
    importMutation.error instanceof ApiError
      ? importMutation.error.message
      : null

  return (
    <li className="flex gap-3 rounded-xl bg-card p-3 ring-1 ring-foreground/10">
      <div className="relative aspect-[2/3] w-20 shrink-0 overflow-hidden rounded-lg bg-muted">
        {mostrarPlaceholder ? (
          <div
            data-testid="external-poster-placeholder"
            className="flex h-full w-full items-center justify-center text-muted-foreground"
          >
            <ImageOff className="size-6" aria-hidden />
          </div>
        ) : (
          <img
            src={result.poster_url ?? undefined}
            alt={`Pôster de ${result.title}`}
            loading="lazy"
            onError={() => setFalhouArte(true)}
            className="h-full w-full object-cover"
          />
        )}
      </div>
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <div className="flex items-baseline gap-2">
          <span className="line-clamp-1 font-medium">{result.title}</span>
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
        <div className="mt-auto flex items-center gap-2 pt-1">
          <Button
            size="sm"
            disabled={importMutation.isPending}
            onClick={() => importMutation.mutate()}
          >
            {importMutation.isPending ? (
              <LoaderCircle className="animate-spin" aria-hidden />
            ) : (
              <Download aria-hidden />
            )}
            {importMutation.isPending ? 'Importando…' : 'Importar'}
          </Button>
          {mensagemErro ? (
            <span role="alert" className="text-xs font-medium text-destructive">
              {mensagemErro}
            </span>
          ) : null}
        </div>
      </div>
    </li>
  )
}
