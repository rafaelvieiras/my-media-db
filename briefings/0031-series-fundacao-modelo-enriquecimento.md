# 0031 — Séries: fundação do modelo + enriquecimento (Fase 1 do épico)

> Modelar séries (série→temporada→episódio) e trazê-las **dos provedores** —
> buscar, importar e navegar a árvore — com o segundo provider (TheTVDB) e o
> registry multi-provider. Arquivos locais ficam para a Fase 2.

| | |
|---|---|
| **ID** | 0031 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override (vertical séries) |
| **Status** | ready |
| **Docs relacionadas** | [ADR-0021](../docs/adr/0021-modelo-de-series-hierarquia-ordenacoes-vinculo-arquivo.md), [00-agenda TV1–TV5](../docs/00-agenda-de-decisoes.md), [04-modelo-de-dados](../docs/04-modelo-de-dados.md), [07-enriquecimento-providers](../docs/07-enriquecimento-providers.md) |
| **Depende de** | 0022, 0024 (detalhe externo rico + paridade), 0025 (molde pai-com-filhos), 0026 (interno-first) |

---

## 0. O épico (contexto e faseamento)

Séries são o salto de complexidade além de filmes. O modelo foi **fundado por
pesquisa empírica** dos dois provedores e fechado em **ADR-0021** (agenda TV1–TV5).
Princípios que este briefing implementa:

- **Episódio = grão fino, entity canônica única**, ancorada no `episode.id`.
- **Ordenação (season type) = projeção de numeração** sobre os mesmos episódios —
  nunca muda a granularidade.
- **Temporada = entity só para a ordenação _default_** (enxuto); alternativas são
  numeração derivada.
- **Arquivo↔episódio = N:M ancorado no id** (o "combinado" é do arquivo, não do
  catálogo) — **materializado na Fase 2**.
- **Duas fontes via registry** (`[tmdb, tvdb]`, executa R2).

Faseamento do épico (TV5):
- **Fase 1 — este briefing (enriquecimento):** schema + provider TheTVDB + registry
  + buscar/importar/exibir a árvore **a partir dos provedores**. Validável sem
  nenhum arquivo local, com Simpsons (2 fontes) e Looney Tunes (só TheTVDB).
- **Fase 2 — scanner + arquivos:** naming de TV (`SxxEyy`/absolute/range
  `S01E01-E03`), criação da árvore a partir do disco e a junção
  `media_file_episode` (N:M). Conferir a convenção Kodi/Jellyfin/Plex/Sonarr.
- **Fase 3 — refinos:** seletor de ordenação na UI, artwork/override de temporada,
  cobertura de i18n de episódios.

## 1. Objetivo

Hoje o app só cataloga filmes. Este briefing dá ao dono a capacidade de **buscar
uma série, importá-la e navegar sua árvore** (temporadas e episódios) com metadados
ricos, usando o **TheTVDB** como segundo provedor — a fonte forte de séries e a
única que tem conteúdo como Looney Tunes. Estabelece o modelo de dados de séries
(ADR-0021) e o **registry multi-provider** (executa R2), que o resto do épico e as
próximas verticais vão reusar.

## 2. Jornada do usuário

1. O dono busca "The Simpsons" na busca (o filtro de tipo já tem "Série").
2. Aparecem resultados de **ambas as fontes** (TMDB e TheTVDB); interno-first se já
   estiver na biblioteca (0026).
3. Ele abre o detalhe externo da série (tela rica já existente) e clica **Importar**.
4. O sistema importa a **árvore**: a série, as temporadas da ordenação default e os
   episódios (grão fino), com pôsteres e sinopses no idioma configurado (fallback
   quando faltar).
5. Na tela da série importada, ele vê a **lista de temporadas**; ao abrir uma
   temporada, vê a **lista de episódios** numerados.
6. Caso "Looney Tunes": busca, acha **só no TheTVDB**, importa; a série existe
   mesmo **sem contraparte no TMDB** (identidade primária do TheTVDB).
7. Desvios: série sem tradução no idioma → cai para o fallback/original; provedor
   fora do ar → cache stale (nunca 404, ADR-0015); episódio sem sinopse → vazio, não
   quebra.

## 3. Escopo

**Dentro:**
- Modelo de séries em schema (ADR-0021): `entity.type` `tv`/`season`/`episode`;
  `extension_data core.tv@1`; `entity_relation.sort_index`; coluna gerada
  `external_id_tvdb`.
- **Provider TheTVDB** (`internal/provider/tvdb/`): `Provider` completo (busca +
  detalhe de série) **e** a capacidade opcional `SerieProvider` (temporadas +
  episódios + season types) — login/token, paginação, i18n `por` + fallback.
- **Registry multi-provider**: `nome→Provider`; `prioridadeFontes` configurável
  (default `[tmdb, tvdb]`); dedup por id externo generalizado (`external_id_tvdb`).
- **Import da árvore** (`enrich`): ramo `tipo=="tv"` → cria série + temporadas (ordem
  default) + episódios finos (com `numbering` multi-ordem) + relações + assets.
- **API + Front**: ler/exibir a árvore (série→temporadas→episódios) reusando o molde
  pai-com-filhos (0025); `rotuloTipo` estendido; OpenAPI + `gen:types`.

**Fora (explicitamente):**
- **Scanner de arquivos locais e naming de TV** → Fase 2.
- **Junção `media_file_episode` (N:M) populada** → Fase 2 (a tabela pode nascer aqui
  vazia ou lá; decidir na implementação — sem arquivos, nada a ligar).
- **Seletor de ordenação na UI, artwork/override de temporada** → Fase 3.
- **Árvore de episódios do TMDB por episode groups** (ordens alternativas do TMDB) →
  a base editorial vem do TMDB, mas a **estrutura de ordenações** é do TheTVDB nesta
  fase; ordens alternativas do TMDB ficam para depois.
- **Elenco/equipe** (MD6/0029).

## 4. Contrato técnico

### Dados / modelo
- **`entity.type`**: passar a gravar/aceitar `tv`, `season`, `episode` (sem CHECK; sem
  migração de núcleo).
- **`extension_data` `core.tv@1`**:
  - série: `{ default_season_type: "official", season_types: ["official","dvd","absolute"] }`
  - temporada: `{ ordering: "official", season_number: 1933, year: 1933 }`
  - episódio: `{ absolute_number: 373, numbering: { official:{season:1933,number:16}, dvd:{season:23,number:5}, absolute:{season:1,number:373} } }`
- **Migração** (pt-BR, ver [ADR-0017](../docs/adr/0017-migrations-goose-embedado.md)):
  `entity_relation` ganha `sort_index INTEGER`; nova coluna gerada `external_id_tvdb`
  (espelha `external_id_tmdb`) + índice. `entity_relation.kind` novos valores
  `tv_season` (série→temporada) e `tv_episode` (temporada→episódio).
- Queries sqlc **em ASCII**; ordenar filhos por `sort_index`.

### API (`/api/v1`)
- Detalhe externo de série já existe (`GET /external/{source}/tv/{id}`); estender o
  DTO para expor a árvore de temporadas quando o provedor for `SerieProvider`.
- Import: `POST /import` com `type=tv` importa a árvore.
- Leitura da árvore importada: reusar/estender `GET /entities/{id}/members` para 2
  níveis **ou** rotas explícitas `GET /entities/{id}/seasons` e
  `.../seasons/{n}/episodes`. Decidir na implementação (menor atrito: reusar
  `members` diferenciando por `type`).
- `openapi.yaml` mantido à mão → rodar `gen:types` (a catraca cobre `tsc -b`).

### Backend (Go)
- `internal/provider/tvdb/`: cliente v4 (login `POST /v4/login {apikey}` com
  `MYMEDIADB_TTDB_API_KEY`, token JWT ~1 mês cacheado, re-login sob 401; paginação
  500/pág via `links.next`; traduções `por` com fallback). Implementa `Provider` +
  nova interface **`SerieProvider`** (análoga a `ColecaoProvider`): buscar temporadas
  e episódios por season type, e a lista de season types da série.
- `provider.go`: declarar `SerieProvider`. `server.go`: registry `nome→Provider`
  substituindo `s.searchProvider` singular; ajustar `externaldetail`,
  `entities.go` (`prioridadeFontes`, `camadasDaFonte`) e `enrich.go`
  (`acharPorIDExterno` por fonte).
- `enrich.go`: ramo `tipo=="tv"` em `Importar` → cria a árvore, dedup de episódios
  por `episode.id` do provedor, persiste `numbering` no `extension_data`.
- **Resolver não muda** (campos por `extension_data`; árvore compõe no server, molde
  `montarMembros`).

### Frontend (Vite/React)
- `entity-detail.tsx`: ramificar `children` por `data.type` — `tv` → lista de
  Temporadas; `season` → lista de Episódios — reusando `CollectionMembers`/
  `entity_relation`. `entity-badges.ts`: `rotuloTipo` para `season`/`episode`.
- `RichDetailView`, `PosterCard`, `schema-form`, `search-panel` já são agnósticos —
  sem mudança estrutural. Uniões `movie|tv` estendidas onde necessário (partir do
  `openapi.yaml`).

### Arquivos afetados (previsão)
- **Criar:** `internal/db/migrations/0007_series.sql`; `internal/provider/tvdb/*.go`;
  queries sqlc de árvore; testes.
- **Alterar:** `internal/provider/provider.go`; `internal/server/{server,entities,external_detail,enrich}.go`;
  `internal/db/queries/entity_relation.sql`; `openapi.yaml`; `web/src/routes/entity-detail.tsx`;
  `web/src/lib/entity-badges.ts`; `web/src/lib/api*.ts` (via gen).

## 5. Critérios de aceite

- [ ] Migração aplica: `entity_relation.sort_index` e `external_id_tvdb` existem;
      `go test ./...` verde (`-count=1`).
- [ ] `tvdb` autentica e busca; `SerieProvider` traz temporadas e episódios com a
      `numbering` multi-ordem (teste com fixture; sem chamar a rede na catraca).
- [ ] Registry serve dois provedores; `prioridadeFontes` configurável resolve a base
      editorial por campo; dedup por `external_id_tvdb` não duplica.
- [ ] Importar **Simpsons** cria série + temporadas (ordem default) + episódios com
      pôsteres/sinopses; **Looney Tunes** importa **sem** contraparte TMDB.
- [ ] A tela da série lista temporadas; a da temporada lista episódios numerados;
      `rotuloTipo` mostra "Série"/"Temporada"/"Episódio".
- [ ] Episódio/temporada resolvem pelo **mesmo** `resolver.Resolver()` (override
      por-idioma disponível), sem tocar o motor.
- [ ] Catraca local verde: `node scripts/quality-gate.mjs --local`, `tsc -b`,
      `npm test`, cobertura Go/web sem regressão, gocyclo/jscpd dentro da tolerância.
- [ ] Smoke ao vivo no ambiente de dev (buscar→importar→navegar Simpsons e Looney
      Tunes) sem erro no console.

## 6. Notas de implementação / decisões

- Fundação já preparada: `entity.type` sem CHECK, `entity_relation` pronta, resolver
  genérico, molde coleção. Ver a varredura em ADR-0021.
- **Ancorar o episódio no `episode.id`** do provedor (não no número) é normativo —
  base para o vínculo de arquivos da Fase 2 e imunidade a re-scrape.
- TheTVDB usa código de idioma **`por`** (3 letras); mapear a partir da config de
  idiomas; **fallback obrigatório** (muitos episódios sem tradução).
- `external_detail.artworks` do TheTVDB **mistura** artwork de série e temporada
  (filtrar por `recordType`); tipos exclusivos (`clearart`/`clearlogo`) são bônus.
- Usar **agentes** para pesquisa/implementação/documentação, respeitando a árvore
  compartilhada do `web/` (sequencial), validando entre etapas.

## 7. Fora deste briefing / follow-ups

- **Fase 2 (scanner + arquivos):** naming de TV, junção `media_file_episode` N:M,
  casos de borda (multi-episódio `S01E01-E03`, specials/Season 0, absolute). Conferir
  Kodi/Jellyfin/Plex/Sonarr.
- **Fase 3 (refinos):** seletor de ordenação na UI, artwork/override de temporada,
  ordens alternativas do TMDB (episode groups), cobertura de i18n de episódios.
- **Maquinário de validação de `extension_data` por JSON Schema** (prometido na doc
  04, ainda inexistente) — endurecer `core.tv@1` depois.
- Elenco/equipe de séries via MD6 (briefing 0029).
