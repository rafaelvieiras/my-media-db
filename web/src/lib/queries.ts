import { queryOptions } from '@tanstack/react-query'
import {
  getCollectionDetail,
  getEntity,
  getEntityEpisodes,
  getEntityFields,
  getEntityFiles,
  getEntityMembers,
  getArtwork,
  getExternalDetail,
  getHealth,
  getLanguageSettings,
  listJobExecutions,
  listJobs,
  getOrphans,
  getOverrideBlob,
  getPerson,
  getReviewQueue,
  getScanStatus,
  listAssets,
  listEntities,
  listLibrary,
  searchMedia,
  type EntityListParams,
  type JobExecutionsParams,
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

// Membros de uma coleção (0025). `lang` entra na chave: trocar o idioma re-busca os
// títulos dos membros. Chave sob `['entity', id, 'members', ...]` para a invalidação
// por entity alcançá-la.
export function entityMembersQueryOptions(id: string, lang?: string) {
  return queryOptions({
    queryKey: ['entity', id, 'members', lang ?? null],
    queryFn: () => getEntityMembers(id, lang),
  })
}

// Episódios da série agrupados por ordem de exibição (0040). A chave carrega
// `(id, ordering, lang)` sob o prefixo `['entity', id]`: trocar a ordem (seletor) é
// cache-miss = re-busca; o idioma re-resolve os títulos. Só é consultada quando a
// ordem escolhida difere do default (a view default usa os cards de temporada).
export function entityEpisodesQueryOptions(
  id: string,
  ordering: string,
  lang?: string,
) {
  return queryOptions({
    queryKey: ['entity', id, 'episodes', ordering, lang ?? null],
    queryFn: () => getEntityEpisodes(id, ordering, lang),
  })
}

// Descritores do formulário editorial (0030). Metadado IMUTÁVEL entre deploys
// (deriva de um registro estático do backend), então `staleTime: Infinity`
// evita re-buscas — as três rotas de CRUD compartilham este cache. Chave própria
// `['schema', 'entity-fields']`, fora do prefixo por-entity (não depende de id).
export const entityFieldsQueryOptions = queryOptions({
  queryKey: ['schema', 'entity-fields'],
  queryFn: getEntityFields,
  staleTime: Infinity,
})

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

// Galeria agrupada por kind + selos de disponibilidade (0048/0049). Chave própria
// `['artwork', id]` — consumida DUAS vezes (a faixa de selos no card do topo e a
// galeria em linhas), que o TanStack Query deduplica numa única ida. Mudar a arte
// (preferir/baixar/remover) invalida ESTA chave (a galeria/selos refletem), E
// `['entity', id]` + `['entities']` (o pôster do detalhe e do grid seguem a nova
// precedência), como a galeria de assets do 0017.
export function artworkQueryOptions(id: string) {
  return queryOptions({
    queryKey: ['artwork', id],
    queryFn: () => getArtwork(id),
  })
}

// Busca unificada (0015). A chave carrega termo+tipo+idioma para cachear por
// consulta: a mesma busca em idiomas diferentes tem resultados externos distintos.
// `enabled: q !== ''` impede o disparo enquanto o input está vazio (estado
// inicial/instrução) — o componente já debounce o termo antes de chegar aqui.
export function searchQueryOptions(
  q: string,
  type?: string,
  lang?: string,
  include?: string[],
) {
  // `include` entra ORDENADO na chave para o cache ser estável (mesma seleção de
  // sub-nós = mesma chave, independente da ordem em que os toggles foram ligados).
  const inc = include && include.length > 0 ? [...include].sort() : null
  return queryOptions({
    queryKey: ['search', q, type ?? null, lang ?? null, inc],
    queryFn: () => searchMedia(q, type, lang, include),
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

// Coleção externa como página (0024). A chave carrega `(source, id, lang)` — o
// idioma entra na chave (mesma lição do detalhe externo): a mesma coleção em idiomas
// diferentes é conteúdo diferente. `lang ?? null` mantém a chave estável.
export function collectionDetailQueryOptions(
  source: string,
  id: string,
  lang?: string,
) {
  return queryOptions({
    queryKey: ['collection', source, id, lang ?? null],
    queryFn: () => getCollectionDetail(source, id, lang),
  })
}

// Pessoa deduplicada + filmografia (0029, fase D). Chave própria `['person', id]` —
// read-only nesta fase, sem invalidação (a pessoa não é editável pela UI).
export function personQueryOptions(id: string) {
  return queryOptions({
    queryKey: ['person', id],
    queryFn: () => getPerson(id),
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

// Jobs (0052, fundação). Chave própria `['jobs']` — invalidada após PATCH
// (habilitar/agendar) e após disparar `run`. POLLING leve enquanto ALGUM job da
// lista está `running` (o card em "rodando…" sai sozinho sem o dono recarregar);
// para quando nenhum estiver, evitando poll para sempre (mesmo espírito do scan).
export const jobsQueryOptions = queryOptions({
  queryKey: ['jobs'],
  queryFn: listJobs,
  refetchInterval: (query) =>
    query.state.data?.some((job) => job.running) ? 4000 : false,
})

// Histórico paginado de execuções de UM job (0052). Chave sob `['job', id,
// 'executions', ...]` — invalidada após disparar `run` daquele job (uma nova
// linha `running` aparece no topo). `limit`/`before` entram na chave para cachear
// por página.
export function jobExecutionsQueryOptions(
  id: string,
  params: JobExecutionsParams = {},
) {
  return queryOptions({
    queryKey: [
      'job',
      id,
      'executions',
      params.limit ?? null,
      params.before ?? null,
    ],
    queryFn: () => listJobExecutions(id, params),
  })
}
