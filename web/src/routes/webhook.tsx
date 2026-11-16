import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Copy, Eye, EyeOff } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { ErrorState, LoadingState } from '@/components/query-states'
import { jellyfinWebhookTokenQueryOptions } from '@/lib/queries'
import { regenerateJellyfinWebhookToken, ApiError } from '@/lib/api'

// Route `/dashboard/webhook`: it closes the hole left by the
// consumption history work — neither delivery built the UI for configuring the token of the
// Jellyfin inbound webhook (each piece of work pushed the responsibility
// to the other). It generates/copies the token and the ready-made URL to paste into the
// Jellyfin Webhooks plugin. The "Webhooks de saída" section is a deliberate
// placeholder: ADR-0022 decided that outbound is a future
// provider capability (`TrackerProvider`), not a configurable feature today.
export function WebhookScreen() {
  const { data, isPending, isError, error } = useQuery(
    jellyfinWebhookTokenQueryOptions,
  )

  return (
    <main className="mx-auto max-w-2xl px-4 py-6">
      <section className="mb-10">
        <div className="mb-4 flex flex-col gap-1">
          <h1 className="font-heading text-xl font-semibold">Webhook</h1>
          <p className="text-sm text-muted-foreground">
            Webhook de entrada — Jellyfin: token usado pelo plugin de Webhooks
            do Jellyfin para reportar reprodução ao histórico de consumo.
          </p>
        </div>

        {isPending ? (
          <LoadingState label="Carregando token…" />
        ) : isError ? (
          <ErrorState
            title="Não foi possível carregar o token do webhook"
            message={error instanceof Error ? error.message : undefined}
          />
        ) : (
          <JellyfinWebhookTokenSection tokenAtual={data.token} />
        )}
      </section>

      <section>
        <div className="mb-4 flex flex-col gap-1">
          <h2 className="font-heading text-lg font-semibold">
            Webhooks de saída
          </h2>
          <p className="text-sm text-muted-foreground">
            Ainda não existem eventos de saída configuráveis — o MyMediaDB só
            recebe eventos (Jellyfin) hoje. Notificar serviços externos (ex.:
            sincronizar com um tracker) fica para uma entrega futura.
          </p>
        </div>
        <p className="rounded-lg border border-dashed border-border px-4 py-6 text-center text-sm text-muted-foreground">
          Nenhum webhook de saída disponível ainda.
        </p>
      </section>
    </main>
  )
}

// The token section itself: empty state (no token generated) OR
// current token with a show/hide toggle + copy. `regenerar` hides behind
// a confirmation when a token already exists (replacing it invalidates the
// current one, requiring the URL to be reconfigured in Jellyfin).
function JellyfinWebhookTokenSection({
  tokenAtual,
}: {
  tokenAtual: string | null
}) {
  const queryClient = useQueryClient()
  const [mostrarToken, setMostrarToken] = useState(false)

  const mutation = useMutation({
    mutationFn: regenerateJellyfinWebhookToken,
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ['webhooks', 'jellyfin', 'token'],
      })
      setMostrarToken(true)
    },
  })

  const mensagemErro =
    mutation.isError && mutation.error instanceof ApiError
      ? mutation.error.message
      : null

  function gerarOuRegenerar() {
    if (
      tokenAtual &&
      !window.confirm(
        'Gerar um novo token invalida o atual — o Jellyfin vai parar de autenticar até você reconfigurar a URL. Continuar?',
      )
    ) {
      return
    }
    mutation.mutate()
  }

  if (!tokenAtual) {
    return (
      <div className="flex flex-col gap-3">
        <p className="text-sm text-muted-foreground">
          Nenhum token gerado ainda.
        </p>
        {mensagemErro ? (
          <p role="alert" className="text-sm text-destructive">
            {mensagemErro}
          </p>
        ) : null}
        <div>
          <Button
            type="button"
            onClick={gerarOuRegenerar}
            disabled={mutation.isPending}
          >
            {mutation.isPending ? 'Gerando…' : 'Gerar token'}
          </Button>
        </div>
      </div>
    )
  }

  const url = `${window.location.origin}/api/v1/webhooks/jellyfin?token=${tokenAtual}`

  return (
    <div className="flex flex-col gap-4">
      <label className="flex flex-col gap-1.5">
        <span className="text-sm font-medium">Token</span>
        <div className="flex items-center gap-2">
          <Input
            type={mostrarToken ? 'text' : 'password'}
            value={tokenAtual}
            readOnly
            aria-label="Token do webhook"
          />
          <Button
            type="button"
            variant="outline"
            size="icon"
            onClick={() => setMostrarToken((v) => !v)}
            aria-label={mostrarToken ? 'Ocultar token' : 'Mostrar token'}
          >
            {mostrarToken ? (
              <EyeOff className="size-4" aria-hidden />
            ) : (
              <Eye className="size-4" aria-hidden />
            )}
          </Button>
        </div>
      </label>

      <div className="flex flex-wrap gap-2">
        <CopyButton label="Copiar token" valor={tokenAtual} />
        <CopyButton label="Copiar URL do webhook" valor={url} />
      </div>

      <p className="text-xs text-muted-foreground">
        Cole a URL acima no plugin de Webhooks do Jellyfin (Painel → Plugins →
        Webhooks → Adicionar → tipo "Genérico"), marcando os eventos de
        reprodução (play/pause/stop).
      </p>

      {mensagemErro ? (
        <p role="alert" className="text-sm text-destructive">
          {mensagemErro}
        </p>
      ) : null}

      <div>
        <Button
          type="button"
          variant="outline"
          onClick={gerarOuRegenerar}
          disabled={mutation.isPending}
        >
          {mutation.isPending ? 'Gerando…' : 'Gerar novo token'}
        </Button>
      </div>
    </div>
  )
}

// Generic copy button (token OR URL) — a "Copiado!" feedback for 2s.
// First use of `navigator.clipboard` in the project.
function CopyButton({ label, valor }: { label: string; valor: string }) {
  const [copiado, setCopiado] = useState(false)

  useEffect(() => {
    if (!copiado) return
    const id = setTimeout(() => setCopiado(false), 2000)
    return () => clearTimeout(id)
  }, [copiado])

  async function copiar() {
    await navigator.clipboard.writeText(valor)
    setCopiado(true)
  }

  return (
    <Button type="button" variant="outline" size="sm" onClick={copiar}>
      {copiado ? (
        <>
          <Check className="size-4" aria-hidden />
          Copiado!
        </>
      ) : (
        <>
          <Copy className="size-4" aria-hidden />
          {label}
        </>
      )}
    </Button>
  )
}
