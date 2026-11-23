import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Copy, Eye, EyeOff } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { CorpoComCarga } from '@/components/query-states'
import { jellyfinWebhookTokenQueryOptions } from '@/lib/queries'
import { useT } from '@/lib/i18n'
import { regenerateJellyfinWebhookToken, ApiError } from '@/lib/api'
import { ScreenHeader } from '@/components/screen-header'

// Route `/dashboard/webhook`: it closes the hole left by the
// consumption history work — neither delivery built the UI for configuring the token of the
// Jellyfin inbound webhook (each piece of work pushed the responsibility
// to the other). It generates/copies the token and the ready-made URL to paste into the
// Jellyfin Webhooks plugin. The "Outbound webhooks" section is a deliberate
// placeholder: ADR-0022 decided that outbound is a future
// provider capability (`TrackerProvider`), not a configurable feature today.
export function WebhookScreen() {
  const t = useT()
  const { data, isPending, isError, error } = useQuery(
    jellyfinWebhookTokenQueryOptions,
  )

  return (
    <main className="mx-auto max-w-2xl px-4 py-6">
      <section className="mb-10">
        <ScreenHeader
          title={t('settings.webhook.title')}
          subtitle={t('settings.webhook.subtitle')}
        />

        <CorpoComCarga
          dados={data}
          carregando={isPending}
          falhou={isError}
          erro={error}
          textos={{
            loading: t('settings.webhook.loading'),
            loadError: t('settings.webhook.loadFailed'),
          }}
        >
          {(dados) => <JellyfinWebhookTokenSection tokenAtual={dados.token} />}
        </CorpoComCarga>
      </section>

      <section>
        <div className="mb-4 flex flex-col gap-1">
          <h2 className="font-heading text-lg font-semibold">
            {t('settings.webhook.outboundTitle')}
          </h2>
          <p className="text-sm text-muted-foreground">
            {t('settings.webhook.outboundSubtitle')}
          </p>
        </div>
        <p className="rounded-lg border border-dashed border-border px-4 py-6 text-center text-sm text-muted-foreground">
          {t('settings.webhook.outboundEmpty')}
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
  const t = useT()
  const queryClient = useQueryClient()
  const [mostrarToken, setMostrarToken] = useState(false)
  // Replacing a token invalidates the configured URL in Jellyfin: the confirmation
  // is an AlertDialog (never `window.confirm`, which the browser can suppress).
  const [confirmarRegenerar, setConfirmarRegenerar] = useState(false)

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
    if (tokenAtual) {
      setConfirmarRegenerar(true)
      return
    }
    mutation.mutate()
  }

  if (!tokenAtual) {
    return (
      <div className="flex flex-col gap-3">
        <p className="text-sm text-muted-foreground">
          {t('settings.webhook.nothingYet')}
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
            {mutation.isPending
              ? t('settings.webhook.generating')
              : t('settings.webhook.generate')}
          </Button>
        </div>
      </div>
    )
  }

  const url = `${window.location.origin}/api/v1/webhooks/jellyfin?token=${tokenAtual}`

  return (
    <div className="flex flex-col gap-4">
      <label className="flex flex-col gap-1.5">
        <span className="text-sm font-medium">
          {t('settings.webhook.tokenLabel')}
        </span>
        <div className="flex items-center gap-2">
          <Input
            type={mostrarToken ? 'text' : 'password'}
            value={tokenAtual}
            readOnly
            aria-label={t('settings.webhook.tokenAria')}
          />
          <Button
            type="button"
            variant="outline"
            size="icon"
            onClick={() => setMostrarToken((v) => !v)}
            aria-label={
              mostrarToken
                ? t('settings.webhook.hideToken')
                : t('settings.webhook.showToken')
            }
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
        <CopyButton
          label={t('settings.webhook.copyToken')}
          valor={tokenAtual}
        />
        <CopyButton label={t('settings.webhook.copyUrl')} valor={url} />
      </div>

      <p className="text-xs text-muted-foreground">
        {t('settings.webhook.pasteHint')}
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
          {mutation.isPending
            ? t('settings.webhook.generating')
            : t('settings.webhook.regenerate')}
        </Button>
      </div>

      <AlertDialog
        open={confirmarRegenerar}
        onOpenChange={setConfirmarRegenerar}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('settings.webhook.confirmRegenerateTitle')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t('settings.webhook.confirmRegenerate')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('common.action.cancel')}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                setConfirmarRegenerar(false)
                mutation.mutate()
              }}
            >
              {t('settings.webhook.confirmRegenerateAction')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

// Generic copy button (token OR URL) — a "Copied!" feedback for 2s.
// First use of `navigator.clipboard` in the project.
function CopyButton({ label, valor }: { label: string; valor: string }) {
  const t = useT()
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
    <>
      <Button type="button" variant="outline" size="sm" onClick={copiar}>
        {copiado ? (
          <>
            <Check className="size-4" aria-hidden />
            {t('settings.webhook.copied')}
          </>
        ) : (
          <>
            <Copy className="size-4" aria-hidden />
            {label}
          </>
        )}
      </Button>
      {/* The feedback is announced, not only painted: the visible label swaps, so this
          live region is what the screen reader hears. It lives NEXT TO the button, not
          inside it — a `role="status"` subtree inside a button is skipped by the
          accessible name computation (the button would lose the name the E2E/AT need). */}
      <span role="status" className="sr-only">
        {copiado ? t('settings.webhook.copied') : ''}
      </span>
    </>
  )
}
