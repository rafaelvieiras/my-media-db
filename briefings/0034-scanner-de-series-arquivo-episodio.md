# 0034 — Scanner de séries: do disco ao episódio (naming TV + auto-import + N:M)

> Ensinar o scanner a reconhecer arquivos de série, casá-los aos episódios do
> catálogo (auto-importando a árvore do provider quando preciso) e vinculá-los
> pela junção N:M genérica `media_file_entity` — ancorada no id, nunca no número.

| | |
|---|---|
| **ID** | 0034 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override (vertical séries, Fase 2 do épico) |
| **Status** | draft |
| **Docs relacionadas** | [ADR-0021](../docs/adr/0021-modelo-de-series-hierarquia-ordenacoes-vinculo-arquivo.md), [0009](0009-scanner-fswalk-ffprobe-matching.md), [0031](0031-series-fundacao-modelo-enriquecimento.md), [0018](0018-fila-revisao-orfas-correcao-match.md) |
| **Depende de** | 0031, 0009 |

---

## 1. Objetivo

A Fase 1 (0031) trouxe o **catálogo** de séries (série→temporada→episódio,
enriquecido do provider). Mas o scanner (0009) é 100% filme: agrupa por pasta,
crava `type="movie"` e vincula `media_file N:1 entity`. Ele não sabe o que é uma
série, não parseia `SxxEyy`, e não há como um arquivo apontar para um episódio.

Este briefing fecha a lacuna **disco → episódio**: o usuário aponta uma
biblioteca de séries, o scanner varre, identifica cada arquivo como episódio(s),
**auto-importa a árvore da série** do provider quando ela ainda não está no
catálogo, e liga o arquivo aos episódios certos pela junção **N:M genérica
`media_file_entity`** (`kind='tv_episode'`). O vínculo é sempre pelo **id do episódio** (lição do bug
do Sonarr, ADR-0021 §3/§4): trocar a ordenação re-projeta números **sem** tocar
vínculos — a vantagem estrutural do MyMediaDB sobre Kodi/Jellyfin/Plex.

## 2. Jornada do usuário

1. O usuário cadastra um datasource e marca o **tipo da biblioteca**: *Filmes* ou
   *Séries* (decisão do dono: tipo explícito, como Kodi/Jellyfin/Plex).
2. Ele dispara o scan. Para cada pasta de série (ex.: `Os Simpsons/Season 01/`):
   - Se a série **já está** no catálogo, o scanner casa os arquivos aos episódios
     por número e os vincula.
   - Se **não está**, o scanner **busca a série pelo nome no provider**, importa a
     árvore (reusa o enrich da Fase 1) e então casa os arquivos (estilo
     Sonarr/Jellyfin — decisão do dono: auto-import).
3. Um arquivo `Show S01E05.mkv` vira 1 vínculo ao episódio S1×5. Um arquivo com
   **range** `Show S01E01-E03.mkv` (rip que empacota 3 curtas) vira **3 vínculos**
   ao mesmo arquivo, com `ordinal` preservando a ordem.
4. Onde o scanner não consegue decidir — **nome de série ambíguo** (o provider
   retorna 2+ séries), **episódio não encontrado**, **numeração fora de qualquer
   ordenação conhecida** — o item vai para a **fila de revisão/órfãs** (0018), sem
   inventar dado.
5. Ao abrir um **episódio**, o usuário vê os **arquivos daquele episódio** (mediainfo,
   caminho). Ao abrir a **temporada/série**, vê a posse agregada (quantos episódios
   possui). O breadcrumb (0032) dá a navegação ascendente.

## 3. Escopo

**Dentro:**
- **Tipo de biblioteca** no `library_source` (`content_type` ∈ `movie`|`series`,
  default `movie`). API de cadastro + UI mínima para escolher no datasource.
- **Parser de TV** no `internal/naming`: reconhece `S01E05`/`s01e05`, **ranges**
  `S01E01-E03` (Jellyfin/Plex/Sonarr, hífen) e `S01E01E03`/`S01E01E02` (Kodi,
  colado), e **absoluto** (anime, ex.: `Show - 013`, `Show 013`). Retorna **lista**
  de episódios (para ranges) + flag de absoluto. Separação de temporada 0 (specials)
  e de número de temporada que **não é 1..N** (pode ser 0, ano, box-set).
- **Junção N:M genérica** `media_file_entity(media_file_id, entity_id, kind, ordinal)`
  (migration nova) + queries sqlc (ASCII). Ancorada no `entity_id`. O `kind` é o
  discriminador (TV grava `tv_episode`; um plugin de música gravaria `track`) —
  espelha o `entity_relation` genérico, **plugin-extensível sem migration do core**
  (ver §6).
- **Scanner de séries**: quando a biblioteca é `series`, o fluxo de match é o novo
  caminho (não o de filme). Detecção da série (nome da pasta), **auto-import** da
  árvore do provider quando ausente, **match em memória** número→`episode_id`
  (ordenação **default da série, com fallback** para as demais `season_types`;
  absoluto contra `absolute_number` — decisão do dono), criação/atualização dos
  vínculos N:M. Ambiguidade/erro → fila de revisão.
- **Posse por episódio** derivada da junção. Leitura: episódio expõe seus arquivos;
  série/temporada expõem posse agregada.
- **Modelo de posse**: `media_file.entity_id` do arquivo de série aponta para a
  **entity da série** (a "obra"); os vínculos por episódio vivem **só** em
  `media_file_entity` (`kind='tv_episode'`). (Ver §6 — decisão.)

**Fora (explicitamente):**
- **Remapeamento manual** de arquivo↔episódio (o caso do streaming que empacota N
  curtas num arquivo nomeado como **1** episódio, exigindo o dono mapear à mão) —
  a junção N:M já **suporta** (1 arquivo → N episódios), mas a **UI de edição**
  do vínculo é follow-up. A Fase 2 resolve só o que o **nome** expressa (ranges
  explícitos `S01E01-E03`).
- **Seletor de ordenação na UI**, artwork/still de episódio, i18n de episódio —
  Fase 3 do épico (0031 §7).
- **Fila persistente de scan / múltiplos scans concorrentes** — evolução do 0009,
  não deste briefing.
- Renomear arquivos no disco — o MyMediaDB **nunca** escreve no disco de mídia.

## 4. Contrato técnico

### Dados / modelo
- **Migration `0008_library_source_content_type.sql`**: `ALTER TABLE library_source
  ADD COLUMN content_type TEXT NOT NULL DEFAULT 'movie';` (valores `movie`|`series`;
  sem CHECK, coerente com o resto do schema). Down: drop da coluna.
- **Migration `0009_media_file_entity.sql`** (junção GENÉRICA, não específica de TV):
  ```sql
  CREATE TABLE media_file_entity (
      media_file_id INTEGER NOT NULL REFERENCES media_file(id) ON DELETE CASCADE,
      entity_id     TEXT    NOT NULL REFERENCES entity(id)     ON DELETE CASCADE,
      kind          TEXT    NOT NULL,
      ordinal       INTEGER NOT NULL DEFAULT 0,
      PRIMARY KEY (media_file_id, entity_id, kind)
  );
  CREATE INDEX idx_mfe_entity ON media_file_entity(entity_id, kind);
  ```
  `kind` discrimina o tipo de vínculo (`tv_episode` para TV; `track` p/ música no
  futuro). `ordinal` ordena as entidades **dentro de um mesmo arquivo** (ranges).
  Down: drop do índice e da tabela.
- **Queries sqlc** (ASCII, `internal/db/queries/media_file_entity.sql`):
  `CriarVinculoEntidade` (upsert idempotente por PK), `ListarVinculosPorArquivo`,
  `ListarArquivosDaEntidade` (por `entity_id`+`kind`), `RemoverVinculosDoArquivoPorKind`
  (para re-scan limpo sem apagar vínculos de outros kinds), `EntidadeTemPosse` +
  agregado de posse. Regenerar `sqlcgen`.
- **Nenhuma** coluna gerada nova para numeração: o match é **em memória** (ver
  Backend). O `numbering`/`absolute_number` seguem no `extension_data core.tv@1`.

### API
- **Cadastro/edição de datasource** ganha `content_type` (`movie`|`series`). Rever o
  handler de `library` (0008): request aceita o campo; response o expõe. Default
  `movie` quando omitido (compat).
- **Detalhe da entity** (`GET /entities/{id}`): para `episode`, incluir os arquivos
  vinculados (via `media_file_entity`, `kind='tv_episode'`) na área de arquivos/mediainfo — hoje a
  anotação de arquivos olha `media_file.entity_id`, que para episódio não aponta o
  episódio. Para `tv`/`season`, expor posse agregada (nº de episódios possuídos /
  total). Atualizar `openapi.yaml` + `gen:types`.
- Nenhum endpoint novo obrigatório (a UI de remap manual é follow-up).

### Backend (Go)
- **`internal/naming`**: novo parser de TV. Sugestão de API — `func ParseEpisode(name
  string) (EpisodeInfo, bool)` no mesmo pacote (o `Parse` de filme fica intacto;
  o cabeçalho já declara TV como follow-up). `EpisodeInfo{Season int; Episodes
  []int; Absolute []int; IsAbsolute bool}`. Regexes cobrindo os padrões do escopo.
  Ranges expandem para lista (`E01-E03` → `[1,2,3]`; `E01E02` → `[1,2]`). Testes de
  tabela exaustivos por padrão (é a peça mais frágil).
- **`internal/scanner`**:
  - `montarFacts`/`resolverEntity` passam a **ramificar por `content_type`** do
    `library_source` (já disponível no job). Para `series`: não usar o cache
    `folderKey→movie`; extrair episódio(s) do **nome do arquivo** (não da pasta), e a
    série do **nome da pasta da série** (a pasta acima de `Season NN/`, quando houver).
  - **Resolução da série**: procurar no catálogo por título (e id externo embutido,
    se houver). Ausente → **auto-import**: `provider` busca `tv` pelo nome (reusa a
    busca externa), escolhe o melhor match (título exato > maior confiança); **1
    candidato** → importa a árvore (reusa `enrich.importarSerie`); **0 ou 2+** →
    fila de revisão, arquivo fica órfão/pendente, **sem** stub de episódio.
  - **Match número→episódio (em memória)**: carregada a árvore da série uma vez por
    série no ciclo do scan, montar mapa a partir do `extension_data` dos episódios:
    `{(ordering, season, number) → episode_id}` e `{absolute → episode_id}`. Casar o
    `EpisodeInfo` do arquivo: (1) tentar a **ordenação default** da série; (2) fallback
    às demais `season_types`; (3) se `IsAbsolute`, casar por `absolute_number`.
    **Ambíguo** (bate em ordens diferentes com episódios distintos) ou **sem match**
    → revisão. Vínculos criados via `CriarVinculoEntidade` (`kind='tv_episode'`) com `ordinal`.
  - **`media_file`**: para série, `entity_id` = **id da série** (a obra). `type` da
    entity nunca mais é cravado `movie` no caminho de série. Re-scan de um arquivo
    já visto: `RemoverVinculosDoArquivoPorKind('tv_episode')` + recriar (idempotente), sobrevive a
    rename via `content_hash` (reencontrar, 0009).
  - **`match_confidence`**: arquivo de série casado por número sem ambiguidade =
    alta (reusar a escala do `confianca()`; número determinístico ≈ 90). Série
    resolvida por id externo embutido no nome = 100.
- **`internal/enrich`**: expor um ponto reusável para "importar árvore da série a
  partir de um resultado de busca do provider" (o scanner chama; já existe
  `importarSerie` a partir de `SerieArvore` — falta o passo buscar-por-nome→escolher).

### Frontend (Vite/React)
- **Cadastro/edição de datasource**: seletor de tipo (Filmes/Séries). Reusa o form
  dirigido por schema (0030) se o datasource já passar por lá; senão, campo simples.
- **Detalhe de episódio**: seção de **arquivos** (mediainfo, caminho, estado) a partir
  dos vínculos. **Temporada/série**: badge de posse (`possui 12/13`). Reusar os
  componentes de arquivo já existentes do detalhe de filme.

### Arquivos afetados (previsão)
- `internal/db/migrations/0008_library_source_content_type.sql` (novo),
  `internal/db/migrations/0009_media_file_entity.sql` (novo).
- `internal/db/queries/media_file_entity.sql` (novo), `library_source.sql`,
  `entity.sql` (posse) — regen `sqlcgen`.
- `internal/naming/naming.go` (+ `naming_test.go`).
- `internal/scanner/scan.go`, `match.go`, `manager.go` (passar `content_type`).
- `internal/enrich/serie.go`/`enrich.go` (buscar-por-nome→importar).
- `internal/server/library*.go`, `entities.go`, `openapi.yaml`.
- `web/`: form de datasource, detalhe de episódio/temporada, `api-types.ts` (gen).
- `internal/e2e/journeys_test.go` (jornada scan de série com fake provider).

## 5. Critérios de aceite

- [ ] Datasource pode ser marcado `series`; o valor persiste e volta na API/UI.
- [ ] `ParseEpisode` reconhece `S01E05`, `s01e05`, `S01E01-E03`, `S01E01E02`,
      absoluto (`Show - 013`) — coberto por testes de tabela; range vira lista.
- [ ] Scan de uma pasta de série **não importada** auto-importa a árvore do provider
      e vincula os arquivos aos episódios corretos.
- [ ] `Show S01E01-E03.mkv` cria **3** linhas em `media_file_entity` para o **mesmo**
      `media_file` (`kind='tv_episode'`), com `ordinal` 0/1/2.
- [ ] Vínculo é pelo `entity_id`; trocar a ordenação default da série **não** altera
      nenhum vínculo (teste: re-projeção não mexe em `media_file_entity`).
- [ ] Nome de série ambíguo (2+ candidatos) ou episódio inexistente → item na fila de
      revisão/órfãs, **sem** criar episódio/série fantasma.
- [ ] Detalhe de episódio lista seus arquivos; temporada/série mostram posse agregada.
- [ ] Catraca completa verde; smoke ao vivo com fake provider (e, se viável, uma
      pasta real de Simpsons de exemplo) cobrindo caminho feliz + range + ambiguidade.

## 6. Notas de implementação / decisões

- **Decisões do dono (AskUserQuestion, 2026-07-11):** (1) **auto-import** do provider
  no scan; (2) **tipo de biblioteca** explícito (não heurística); (3) match **na
  ordenação default com fallback** às demais + absoluto contra `absolute_number`.
- **Junção arquivo↔entity é GENÉRICA (`media_file_entity` com `kind`), não específica
  de TV.** Espelha o `entity_relation(parent, child, kind, sort_index)`: o core ganha
  UMA tabela de vínculo arquivo↔entity reusável por qualquer tipo/plugin, em vez de
  uma tabela por tipo (`media_file_episode`, `media_file_track`, …) que exigiria
  migration do core a cada plugin. O scanner de TV grava `kind='tv_episode'`; um
  plugin de música gravaria `kind='track'`. Custo idêntico ao da versão específica
  (uma coluna `kind` a mais), alinhado ao **princípio de esquema extensível para
  plugins** (tipos livres em `entity.type`, relações genéricas em `entity_relation`,
  campos em `extension_data` namespaced). Descartado: junção por tipo (não
  plugin-extensível).
- **`media_file.entity_id` aponta para a SÉRIE, não para o episódio.** Um arquivo
  pode mapear N episódios (range/combinado); um `entity_id` escalar não representaria
  isso. Apontar para a série (o ancestral comum) mantém `entity_id NOT NULL` e o
  caminho de filme intacto. **Posse do episódio = existe linha em
  `media_file_entity`**; posse de temporada/série = derivada (agregação). Descartado:
  `entity_id`→episódio (assimétrico p/ ranges) e `entity_id` nullable (mexe no
  contrato de filme). *Esta é a decisão estrutural do briefing; se provar-se
  fundacional, promover a nota no ADR-0021.*
- **Match em memória, não coluna gerada.** O `numbering` é aninhado e **multi-ordem**
  (uma coluna gerada `VIRTUAL` não expressa bem N ordenações). Carregar os episódios
  da série (volume pequeno, self-hosted) e montar o mapa é mais simples e cobre o
  fallback entre ordens sem SQL exótico. Se a escala exigir, indexar depois.
- **Âncora no id (ADR-0021).** O número resolve para `episode_id` **no momento do
  scan** e é isso que persiste. Re-scrape que renumere não pode remexer vínculos.
- **Ranges vs. combinado do streaming.** A junção 1-arquivo→N-episódios cobre ambos;
  a Fase 2 só **detecta** o que o nome diz (`S01E01-E03`). O caso do arquivo nomeado
  como 1 episódio que na verdade contém N (mapeamento manual) usa a **mesma** tabela,
  mas via UI de edição — follow-up.

## 7. Fora deste briefing / follow-ups

- **UI de remapeamento manual** arquivo↔episódio (streaming empacotado, correção de
  match) — reusa `media_file_entity`.
- **Seletor de ordenação** na UI (projeta os números da ordem escolhida a partir do
  `numbering`), artwork/still e i18n de episódio — Fase 3 do 0031.
- **Heurística de detecção** como fallback ao tipo de biblioteca (o dono escolheu
  tipo explícito; heurística fica como conveniência futura).
- **Specials/temporada 0** e numerações atípicas: cobrir no parser/matcher; casos
  exóticos que sobrarem viram itens da fila de revisão.
