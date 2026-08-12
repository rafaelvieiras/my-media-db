import { queryOptions } from '@tanstack/react-query'
import {
  browseLibraryPath,
  getCollectionDetail,
  getEntity,
  getEntityEpisodes,
  getEntityFields,
  getEntityFiles,
  getEntityMembers,
  getArtwork,
  getExternalDetail,
  getCatalogCompletenessSettings,
  getHealth,
  getHistoricoEstado,
  getSystemHealth,
  hasEntityTheme,
  listHistoricoEventos,
  listHistoricoOrfaos,
  getJellyfinWebhookToken,
  getLanguageSettings,
  getRetroactiveEnrichmentSettings,
  getAutoMaterializeArtworkSettings,
  getYtdlpSettings,
  getMatchPreview,
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
  type HistoricoOrfaoListParams,
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

// Telemetria de sistema (0055): chave própria `['health', 'system']` — SOB o
// mesmo prefixo de `healthQueryOptions` (é a mesma tela/domínio), mas com
// `queryFn` independente: uma falha aqui não deve marcar `['health']` como
// erro (são dois `useQuery` distintos na tela, ver health.tsx).
export const systemHealthQueryOptions = queryOptions({
  queryKey: ['health', 'system'],
  queryFn: getSystemHealth,
})

export const libraryQueryOptions = queryOptions({
  queryKey: ['library'],
  queryFn: listLibrary,
})

// Navegador de pastas (briefing 0071): a chave carrega o caminho navegado — cada
// pasta visitada cacheia sob sua própria chave (voltar a uma pasta já vista na
// sessão não refaz o fetch). Usado pelo `PathBrowser`.
export function libraryBrowseQueryOptions(path: string) {
  return queryOptions({
    queryKey: ['library', 'browse', path],
    queryFn: () => browseLibraryPath(path),
    // Path inválido (422) é erro permanente — retentar não muda o resultado
    // sem mudar `path`. Retry aqui só gera 3 chamadas idênticas ao servidor.
    retry: false,
  })
}

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

// Config de throttle do enriquecimento retroativo (0085): fonte única para o
// admin (settings.tsx) e o placeholder do override por biblioteca
// (library-new.tsx/library-edit.tsx). Chave `['settings', 'enrichment-retroativo']`
// — invalidada pelo PUT da tela de configurações.
export const retroactiveEnrichmentSettingsQueryOptions = queryOptions({
  queryKey: ['settings', 'enrichment-retroativo'],
  queryFn: getRetroactiveEnrichmentSettings,
})

// Config do toggle de materialização automática de artes (0089): fonte única
// para o card de artes da tela de configurações. Chave
// `['settings', 'auto-materialize-artwork']` — invalidada pelo PUT do switch.
export const autoMaterializeArtworkSettingsQueryOptions = queryOptions({
  queryKey: ['settings', 'auto-materialize-artwork'],
  queryFn: getAutoMaterializeArtworkSettings,
})

// Status do binário yt-dlp + path customizado (0101, ADR-0025): fonte única
// para o card "yt-dlp" da tela de configurações. Chave `['settings', 'ytdlp']`
// — invalidada pelo PUT do formulário.
export const ytdlpSettingsQueryOptions = queryOptions({
  queryKey: ['settings', 'ytdlp'],
  queryFn: getYtdlpSettings,
})

// Token do webhook de entrada Jellyfin (0093/0097): fonte única para a tela
// `/dashboard/webhook`. Chave `['webhooks', 'jellyfin', 'token']` — invalidada
// depois de gerar/regenerar o token.
export const jellyfinWebhookTokenQueryOptions = queryOptions({
  queryKey: ['webhooks', 'jellyfin', 'token'],
  queryFn: getJellyfinWebhookToken,
})

// Config de completude de CATALOGAÇÃO (0057): fonte única para o formulário de
// admin. Chave `['settings', 'catalog-completeness']` — invalidada pelo PUT da
// tela de configurações. Conceito DIFERENTE de completude de POSSE
// (`entity-badges.ts`) — não compartilha chave/cache com aquele.
export const catalogCompletenessSettingsQueryOptions = queryOptions({
  queryKey: ['settings', 'catalog-completeness'],
  queryFn: getCatalogCompletenessSettings,
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

// Tema musical local (briefing 0082): confere (HEAD, uma vez por carga) se a
// entity tem um theme.{mp3,ogg,flac} na pasta-raiz — sem invalidação própria
// (o arquivo só muda por ação manual do dono no disco, fora da UI).
export function entityThemeQueryOptions(id: string) {
  return queryOptions({
    queryKey: ['entity', id, 'theme'],
    queryFn: () => hasEntityTheme(id),
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

// Histórico de consumo (briefing 0092/0094, ADR-0022). Eventos brutos, mais
// recente primeiro (`kind` opcional filtra; ausente traz todos). Chave própria
// `['historico', id, 'eventos', kind]` — SOB o prefixo `['historico', id]`, para
// uma invalidação por `['historico', id]` (após criar/apagar evento) derrubar
// de uma vez esta chave E `historicoEstadoQueryOptions` abaixo, sem enumerar
// `kind`s nem duplicar a chamada de invalidação.
export function historicoEventosQueryOptions(id: string, kind?: string) {
  return queryOptions({
    queryKey: ['historico', id, 'eventos', kind ?? null],
    queryFn: () => listHistoricoEventos(id, kind),
  })
}

// Estado atual derivado do histórico, por kind (0092) — alimenta o selo
// "Assistido" do hero (kind="assistir"). Chave `['historico', id, 'estado']`,
// mesmo prefixo `['historico', id]` da query de eventos acima (mesma
// invalidação após criar/apagar).
export function historicoEstadoQueryOptions(id: string) {
  return queryOptions({
    queryKey: ['historico', id, 'estado'],
    queryFn: () => getHistoricoEstado(id),
  })
}

// Lista GLOBAL de eventos órfãos (briefing 0099), tela `/dashboard/historico-
// orfaos`. Chave própria `['historico', 'orfaos', params]` — FORA do prefixo
// `['historico', id]` acima (é uma lista global, não de uma entity), então a
// invalidação por-entity não precisa (nem deveria) tocar aqui.
export function historicoOrfaosQueryOptions(params: HistoricoOrfaoListParams) {
  return queryOptions({
    queryKey: ['historico', 'orfaos', params],
    queryFn: () => listHistoricoOrfaos(params),
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

// Prévia de candidato por card da fila de revisão (0060). Chave `['match-preview',
// id]` — um `useQuery` por `ReviewCard` ao montar (não lazy; ver briefing §6, um
// request por card é aceitável para o volume da fila). Sem invalidação própria: o
// card some da fila (e a query junto) assim que `confirm`/`confirm-candidate`
// resolve, via `invalidarRevisao` do ['review'].
export function matchPreviewQueryOptions(id: string) {
  return queryOptions({
    queryKey: ['match-preview', id],
    queryFn: () => getMatchPreview(id),
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
