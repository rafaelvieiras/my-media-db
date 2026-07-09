import { useQuery } from '@tanstack/react-query'
import { healthQueryOptions } from '@/lib/queries'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'

// Tela de saúde (rota `/health`): consome `health` via TanStack Query e deriva
// três estados visíveis — carregando / erro / ok. Um erro de rede é ESTADO da
// UI, nunca uma exceção não-capturada: o Card sempre renderiza.
export function HealthScreen() {
  const { data, isPending, isError } = useQuery(healthQueryOptions)

  return (
    <main className="flex items-center justify-center p-6">
      <Card className="w-full max-w-md" size="default">
        <CardHeader>
          <CardTitle className="flex items-center justify-between gap-3">
            Estado da API
            <StatusBadge isPending={isPending} isError={isError} />
          </CardTitle>
          <CardDescription>Saúde do serviço MyMediaDB</CardDescription>
        </CardHeader>
        <CardContent>
          {isPending ? (
            <p className="text-muted-foreground">Conectando…</p>
          ) : isError ? (
            <p className="font-medium text-destructive">API indisponível</p>
          ) : (
            <p className="font-medium text-emerald-600 dark:text-emerald-400">
              API online — v{data.version}
            </p>
          )}
        </CardContent>
      </Card>
    </main>
  )
}

function StatusBadge({
  isPending,
  isError,
}: {
  isPending: boolean
  isError: boolean
}) {
  if (isPending) {
    return <Badge variant="secondary">conectando</Badge>
  }
  if (isError) {
    return <Badge variant="destructive">offline</Badge>
  }
  return (
    <Badge className="border-transparent bg-emerald-600 text-white">
      online
    </Badge>
  )
}
