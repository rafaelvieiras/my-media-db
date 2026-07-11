# ADR-0021 — Modelo de séries: hierarquia, ordenações como projeção e vínculo N:M arquivo↔episódio

- **Status:** Aceita
- **Data:** 2026-07-11
- **Agenda:** [TV1–TV5](../00-agenda-de-decisoes.md) · **Spec:** [04-modelo-de-dados](../04-modelo-de-dados.md), [07-enriquecimento-providers](../07-enriquecimento-providers.md)

## Contexto

Séries são o salto de complexidade além de filmes (item único): uma hierarquia
**série → temporada → episódio** e o problema espinhoso das **múltiplas
ordenações de temporada** (aired / DVD / absolute / alternate). Os dois provedores
que o MVP usa expõem isso de formas diferentes — TMDB via *episode groups*, TheTVDB
via *season types* — e um deles (TheTVDB) tem conteúdo que o outro removeu por
política (curtas antigos exibidos na TV). Antes de qualquer código foi feita
**pesquisa empírica** nas duas APIs (Simpsons 71663/456, Looney Tunes 72514,
One Piece 81797) e nas convenções de media servers (Jellyfin/Kodi/Plex/Sonarr).

A tensão central que a pesquisa resolveu: um mesmo episódio aparece com
`(temporada, número)` **diferentes** conforme a ordem, então esse par **não é
chave**; e um arquivo de streaming frequentemente **empacota vários curtas** que na
TV foram episódios separados, então o vínculo arquivo↔episódio não é 1:1.

## Decisão

1. **Episódio é a entidade canônica de grão fino.** Uma `entity type='episode'` por
   episódio atômico do provedor, ancorada no **`episode.id`** (nunca no número). A
   numeração de **todas** as ordens vive no `extension_data` namespace `core.tv@1`
   (`absolute_number` + `numbering: {official:{season,number}, dvd:{…}, …}`),
   espelhando o `episode.seasons[]` do TheTVDB.

2. **Ordenação (season type) é projeção de numeração, não estrutura.** Trocar de
   ordem **renumera/reagrupa os mesmos episódios finos — nunca muda a
   granularidade**. Confirmado empiricamente por conjuntos de ids: em Looney Tunes o
   DVD (413) é subconjunto exato do aired (1195), `dvd-only = 0` (é **cobertura**,
   não fusão); em One Piece a **união das 4 ordens = 1231 = o total do `official`**,
   com **zero ids próprios** nas ordens alternativas. Nenhum provedor tem conceito de
   "episódio combinado" (sem campo `combined`/`parentEpisode`/`subEpisodes`).

3. **Temporada é entity apenas para a ordenação _default_ da série** (configurável —
   pode ser DVD se a biblioteca do dono for DVD). Ela existe para dar casa a
   **artwork e override próprios** e à navegação; liga à série por
   `entity_relation(kind='tv_season', sort_index=número)`. **Ordenações alternativas
   não materializam temporadas** — são numeração derivada do episódio, agrupada na
   leitura pelo server. (A opção uniforme — temporada-entity por ordem, ~103 para
   Looney Tunes — foi descartada por custo e por ordens alternativas raramente terem
   artwork próprio.)

4. **O vínculo arquivo↔episódio é N:M, ancorado no id do episódio.** Nova junção
   `media_file_episode(media_file_id, episode_id, ordinal)`. Um arquivo de streaming
   que empacota 3 curtas → **1 arquivo ligado a 3 episódios** (`ordinal` preserva a
   sequência); um episódio dividido em partes → **1 episódio ligado a N arquivos**
   (o sentido já herdado de filmes, [ADR-0008](0008-identidade-e-matching-kodi-jellyfin.md)).
   O "combinado" é propriedade do **arquivo/release**, não do catálogo. Ancorar no
   **id** (não no número) é normativo: imuniza contra o re-scrape "remexer" vínculos.

5. **Duas fontes externas passam pelo registry de providers (executa R2).** Substitui
   o provider singular e o `prioridadeFontes=["tmdb"]` hardcoded por um registry
   `nome→Provider` e a lista global `[tmdb, tvdb]`
   ([ADR-0002](0002-prioridade-entre-fontes-externas.md)): por campo vence a primeira
   fonte com valor; o TheTVDB fornece as ordenações e artwork exclusivo. Séries
   **exclusivas de um provedor** (Looney Tunes não tem `remoteId` de TMDB nem IMDB)
   usam **identidade primária do TheTVDB** — o schema aceita item sem contraparte.

## Consequências

- **Trocar a ordem re-projeta os números sem tocar nos arquivos** — vantagem sobre
  Kodi/Jellyfin/Plex, que exigem renomear arquivos ao mudar de ordem.
- Episódio e temporada são **entities de 1ª classe** → herdam o resolver, o override
  por-idioma, a busca FTS, a posse por `media_file` e o artwork, **sem tocar o motor
  do resolver** (campos por `extension_data`; a árvore compõe no server, como
  `montarMembros` já faz para coleção). Custo: ~N entities por série (barato).
- **`entity_relation` ganha `sort_index`** (ordenação estável de filhos; hoje só
  ordena por `id`). **Nova coluna gerada `external_id_tvdb`** (dedup por id TheTVDB),
  espelhando `external_id_tmdb/_imdb`. `entity.type` **não** muda (sem CHECK).
- **`extension_data core.tv@1`** cobra o maquinário de validação por JSON Schema
  (prometido em [04](../04-modelo-de-dados.md), ainda inexistente) — ou aceitar dado
  não validado no início, endurecendo depois.
- Ordenações alternativas **sem entity**: navegar "DVD S23" é **agrupamento em
  leitura** (server), não relacional. Aceitável; promover uma temporada alternativa a
  entity (para override/artwork próprios nela) fica como **evolução** se a
  necessidade aparecer.
- Multi-idioma: episódios usam o mesmo mecanismo por-idioma
  ([ADR-0019](0019-resolucao-por-idioma-fallback-proveniencia.md)); TheTVDB usa código
  `por` (3 letras, sem pt-BR/pt-PT) e **exige fallback** (muitos episódios sem
  tradução).
- Decisões relacionadas: [ADR-0001](0001-resolver-particionamento-por-origem.md)
  (motor genérico), [ADR-0002](0002-prioridade-entre-fontes-externas.md) (prioridade
  de fontes), [ADR-0005](0005-composicao-uniforme-extension-data.md) (`extension_data`),
  [ADR-0008](0008-identidade-e-matching-kodi-jellyfin.md) (matching Kodi/Jellyfin),
  [ADR-0011](0011-external-ids-autoritativo-media-external-cache.md) (identidade
  externa).
