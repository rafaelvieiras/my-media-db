import { queryOptions } from '@tanstack/react-query'
import {
  getEntity,
  getEntityFiles,
  getExternalDetail,
  getHealth,
  getLanguageSettings,
  getOrphans,
  getOverrideBlob,
  getReviewQueue,
  getScanStatus,
  listAssets,
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

// `lang` entra na queryKey: trocar o idioma de leitura é cache-miss = re-busca
// por `?lang=`, sem recarregar a página. `lang` omitido usa `null` na chave
// (chave estável; equivale a "sem idioma" = principal do backend).
export function entitiesQueryOptions(params: EntityListParams, lang?: string) {
  return queryOptions({
    queryKey: ['entities', params, lang ?? null],
    queryFn: () => listEntities(params, lang),
  })
}

export function entityQueryOptions(id: string, lang?: string) {
  return queryOptions({
    queryKey: ['entity', id, lang ?? null],
    queryFn: () => getEntity(id, lang),
  })
}

// Config de idioma (0019): fonte única para o seletor e o admin. Chave
// `['settings', 'languages']` — invalidada pelo PUT da tela de configurações.
export const languageSettingsQueryOptions = queryOptions({
  queryKey: ['settings', 'languages'],
  queryFn: getLanguageSettings,
})

export function entityFilesQueryOptions(id: string) {
  return queryOptions({
    queryKey: ['entity', id, 'files'],
    queryFn: () => getEntityFiles(id),
  })
}

// Blob CRU do override (0016). A chave `['entity', id, 'override']` fica SOB o
// prefixo `['entity', id]` — assim uma invalidação por `['entity', id]` após a
// edição derruba de uma vez a entity (todas as variações de `lang`), os arquivos
// e este blob, sem enumerar idiomas. Não carrega `lang`: o blob é o mesmo para
// todos (a UI lê a fatia do idioma corrente dele).
export function overrideBlobQueryOptions(id: string) {
  return queryOptions({
    queryKey: ['entity', id, 'override'],
    queryFn: () => getOverrideBlob(id),
  })
}

// Galeria de arte (0017). Chave própria `['assets', id]` (não sob `['entity',
// id]`): mudar a arte invalida ESTA chave E `['entity', id]` (o pôster do
// detalhe/grid reflete a nova precedência) E `['entities']` (o grid) —
// invalidações independentes, sem derrubar uma pela outra.
export function assetsQueryOptions(id: string) {
  return queryOptions({
    queryKey: ['assets', id],
    queryFn: () => listAssets(id),
  })
}

// Busca unificada (0015). A chave carrega termo+tipo+idioma para cachear por
// consulta: a mesma busca em idiomas diferentes tem resultados externos distintos.
// `enabled: q !== ''` impede o disparo enquanto o input está vazio (estado
// inicial/instrução) — o componente já debounce o termo antes de chegar aqui.
export function searchQueryOptions(q: string, type?: string, lang?: string) {
  return queryOptions({
    queryKey: ['search', q, type ?? null, lang ?? null],
    queryFn: () => searchMedia(q, type, lang),
    enabled: q !== '',
  })
}

// Detalhe externo rico (0022/0023). A chave carrega `(source, type, id, lang)`:
// o idioma entra na chave porque a MESMA tela em idiomas diferentes é conteúdo
// diferente (mesma lição da busca) — trocar o idioma é cache-miss = re-busca por
// `?lang=`. Cada item externo visitado (inclusive similares/coleção navegados)
// cacheia sob sua própria chave. `lang ?? null` mantém a chave estável.
export function externalDetailQueryOptions(
  source: string,
  type: string,
  id: string,
  lang?: string,
) {
  return queryOptions({
    queryKey: ['external', source, type, id, lang ?? null],
    queryFn: () => getExternalDetail(source, type, id, lang),
  })
}

// Fila de revisão (0018). Chave própria `['review']` — invalidada após confirmar
// ou corrigir um match (junto de `['entities']` e `['entity', id]`, que também
// mudam de estado). Query enxuta (array de cards), sem params.
export const reviewQueueQueryOptions = queryOptions({
  queryKey: ['review'],
  queryFn: getReviewQueue,
})

// Fila de órfãs (0018). Chave `['orphans']` — invalidada após purgar (individual
// ou lote), junto de `['entities']` (o card some do grid também).
export const orphansQueryOptions = queryOptions({
  queryKey: ['orphans'],
  queryFn: getOrphans,
})

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
