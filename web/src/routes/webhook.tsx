import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Copy, Eye, EyeOff } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { ErrorState, LoadingState } from '@/components/query-states'
import { jellyfinWebhookTokenQueryOptions } from '@/lib/queries'
import { regenerateJellyfinWebhookToken, ApiError } from '@/lib/api'

// Rota `/dashboard/webhook` (briefing 0097): fecha o buraco deixado pelo
// 0093/0094 — nenhum dos dois construiu a UI de configuração do token do
// webhook de entrada do Jellyfin (cada briefing empurrou a responsabilidade
// pro outro). Gera/copia o token e a URL pronta pra colar no plugin de
// Webhooks do Jellyfin. A seção "Webhooks de saída" é um placeholder
// consciente: H2/ADR-0022 decidiu que saída é uma capacidade de provider
// futura (`TrackerProvider`), não uma feature configurável hoje.
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

// Seção do token propriamente dita: estado vazio (nenhum token gerado) OU
// token vigente com toggle mostrar/ocultar + copiar. `regenerar` some por
// trás de uma confirmação quando já existe um token (substituir invalida o
// atual, exigindo reconfigurar a URL no Jellyfin).
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

// Botão de copiar genérico (token OU URL) — feedback "Copiado!" por 2s.
// Primeiro uso de `navigator.clipboard` no projeto.
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
