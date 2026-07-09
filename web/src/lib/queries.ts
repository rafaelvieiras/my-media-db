import { queryOptions } from '@tanstack/react-query'
import { getHealth } from '@/lib/api'

// Opções compartilhadas para a query de saúde. Centralizar em `queryOptions`
// mantém a queryKey e a queryFn coesas e reutilizáveis (componente, loader do
// router, prefetch, etc.).
export const healthQueryOptions = queryOptions({
  queryKey: ['health'],
  queryFn: getHealth,
})
