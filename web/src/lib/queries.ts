import { queryOptions } from '@tanstack/react-query'
import {
  getEntity,
  getEntityFiles,
  getHealth,
  getScanStatus,
  listEntities,
  listLibrary,
  searchMedia,
  type EntityListParams,
} from '@/lib/api'

// Opções compartilhadas por rota. Centralizar em `queryOptions` mantém a
// queryKey e a queryFn coesas e reutilizáveis (componente, loader do router,
// prefetch, invalidação). As chaves seguem o recurso: `['library']`,
// `['entities', params]`, `['entity', id]`, `['scan', id]`.
export const healthQueryOptions = queryOptions({
  queryKey: ['health'],
  queryFn: getHealth,
})

export const libraryQueryOptions = queryOptions({
  queryKey: ['library'],
  queryFn: listLibrary,
})

export function entitiesQueryOptions(params: EntityListParams) {
  return queryOptions({
    queryKey: ['entities', params],
    queryFn: () => listEntities(params),
  })
}

export function entityQueryOptions(id: string) {
  return queryOptions({
    queryKey: ['entity', id],
    queryFn: () => getEntity(id),
  })
}

export function entityFilesQueryOptions(id: string) {
  return queryOptions({
    queryKey: ['entity', id, 'files'],
    queryFn: () => getEntityFiles(id),
  })
}

// Busca unificada (0015). A chave carrega termo+tipo para cachear por consulta.
// `enabled: q !== ''` impede o disparo enquanto o input está vazio (estado
// inicial/instrução) — o componente já debounce o termo antes de chegar aqui.
export function searchQueryOptions(q: string, type?: string) {
  return queryOptions({
    queryKey: ['search', q, type ?? null],
    queryFn: () => searchMedia(q, type),
    enabled: q !== '',
  })
}

// Status do scan com POLLING: enquanto `running`, o TanStack Query re-busca a
// cada 1,5s; ao concluir, `refetchInterval` devolve `false` e o polling para
// sozinho (decisão de sessão — §6 do 0011: polling, não websocket).
export function scanStatusQueryOptions(id: string) {
  return queryOptions({
    queryKey: ['scan', id],
    queryFn: () => getScanStatus(id),
    refetchInterval: (query) => (query.state.data?.running ? 1500 : false),
  })
}
