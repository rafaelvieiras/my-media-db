# 0007 — Schema do núcleo (mínimo da Fase 2)

> A primeira migration real: `entity`, `media_file`, `library_source`, `asset`,
> `entity_relation` e a busca FTS5 — mais as queries sqlc de CRUD. Fecha o "briefing
> de schema" que a doc 04 vinha adiando.

| | |
|---|---|
| **ID** | 0007 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 2 — UI de biblioteca |
| **Docs relacionadas** | [04-modelo-de-dados](../docs/04-modelo-de-dados.md), [ADR-0009](../docs/adr/0009-posse-e-status-eixos-ortogonais.md), [ADR-0011](../docs/adr/0011-external-ids-autoritativo-media-external-cache.md), [ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md) |
| **Status** | ready |
| **Depende de** | [0006](0006-fundacao-persistencia-sqlite.md) |

---

## 1. Objetivo

A doc 04 traz o modelo como **esboço** ("nomes/colunas a fechar no briefing de
schema"). Este é esse briefing: fecha e materializa o **schema mínimo da Fase 2**
como migration goose + queries sqlc, respeitando as decisões já travadas — posse
derivada e `status` como intenção ([ADR-0009](../docs/adr/0009-posse-e-status-eixos-ortogonais.md)),
`external_ids` autoritativo ([ADR-0011](../docs/adr/0011-external-ids-autoritativo-media-external-cache.md)),
ciclo de vida `missing`/offline com `last_seen` ([ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md)),
e busca sem GIN (FTS5 + colunas geradas). Não cobre `media_external`/`media_override`
(entram na Fase 3, com o enriquecimento e o override).

## 2. Jornada do usuário

Jornada do **dev** (schema é interno):

1. Roda as migrations ([0006](0006-fundacao-persistencia-sqlite.md)); as tabelas do
   núcleo passam a existir com FKs, índices e a tabela FTS.
2. Usa as queries sqlc geradas para inserir/ler uma `entity` com um `media_file`,
   um `asset` e checar a **posse derivada** e a busca textual — tudo por teste.
3. Um `PRAGMA foreign_key_check` e uma busca FTS retornam sem erro.

## 3. Escopo

**Dentro (migration + queries sqlc):**
- **`entity`**: `id` (text/uuid, PK), `type`, `title`, `original_title`, `description`,
  `release_date`, `status` (default `cataloged`, CHECK no conjunto), `tags_json`,
  `external_ids_json`, `extension_data_json`, `created_at`, `updated_at`.
- **`media_file`**: `id` (PK), `entity_id` (FK), `path` (UNIQUE), `content_hash`
  (indexado), `size`, `mtime`, `edition`, `part_no`, `match_confidence` (real),
  `mediainfo_json`, **`state`** (`present`|`missing`, CHECK), **`last_seen`**,
  `created_at`, `updated_at`. 1:N com `entity`.
- **`library_source`**: `id` (PK), `path` (UNIQUE), `type`, `last_scan`, **`state`**
  (`online`|`offline`, CHECK), `created_at`, `updated_at`.
- **`asset`**: `id` (PK), `entity_id` (FK), `kind` (poster|fanart|…), `origin`
  (local|external|custom), `path_or_uri`, `meta_json`, `created_at`.
- **`entity_relation`**: `id` (PK), `parent_id` (FK), `child_id` (FK), `kind`
  (série↔temporada↔episódio, coleção↔filme). Criada agora; populada só pelos tipos
  que o scanner tratar.
- **Colunas geradas + índices** (busca sem GIN, doc 04):
  - `external_ids` quentes → coluna gerada `GENERATED ALWAYS AS
    (json_extract(external_ids_json,'$.tmdb'))` (idem imdb) + índice, para lookup por id.
  - **FTS5** `entity_fts` sobre `title`/`original_title`/`tags`, sincronizada por
    **triggers** `AFTER INSERT/UPDATE/DELETE` em `entity`.
- **Queries sqlc** de CRUD mínimo por tabela + a consulta de **posse derivada**
  (EXISTS `media_file` com `state='present'`) usada pela listagem.

**Fora (explicitamente):**
- `media_external`, `media_override`, `person`, `credit`, `company` — Fase 3/4.
- Qualquer endpoint (é [0008](0008-datasources-cadastro-api-library.md)+) e o scanner ([0009](0009-scanner-fswalk-ffprobe-matching.md)).
- Materializar posse como coluna: **não** (uma coluna gerada não enxerga outra
  tabela); posse é computada na query. Ver notas.

## 4. Contrato técnico

### Dados / modelo
- DDL conforme o Escopo. `PRAGMA foreign_keys=ON` já vem do [0006](0006-fundacao-persistencia-sqlite.md).
- `*_json` são `TEXT` validados como JSON pela app antes de gravar (JSON1 disponível).
- Resolver divergências do esboço da doc 04: **incluir `last_seen` e `state`** em
  `media_file` (exigidos por [ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md));
  **incluir `state` em `library_source`** (sinal offline). Atualizar a doc 04 e o
  diagrama ER para refletir (ver §6).

### Backend (Go)
- Migration `0002_schema_nucleo.sql` (Up/Down) em `internal/db/migrations/`.
- `internal/db/queries/*.sql` para o sqlc; regenerar o pacote.
- Sem handler novo; só o pacote de dados.

### Arquivos afetados
- Criar: `internal/db/migrations/0002_schema_nucleo.sql`, `internal/db/queries/*.sql`.
- Alterar: pacote sqlc gerado; `docs/04-modelo-de-dados.md` (fechar esboço +
  ER); testes.

## 5. Critérios de aceite

- [ ] Migration cria todas as tabelas do Escopo; `Down` as remove; `foreign_key_check` limpo.
- [ ] `status` default `cataloged` e CHECK rejeita valor fora de `{cataloged,wanted,unreleased,draft}`.
- [ ] `media_file.state` CHECK em `{present,missing}`; `path` UNIQUE; `content_hash` indexado.
- [ ] Coluna gerada de `external_ids` permite lookup por `tmdb` via índice (verificável por `EXPLAIN QUERY PLAN`).
- [ ] Inserir/atualizar/apagar `entity` mantém `entity_fts` em sincronia (busca retorna o esperado).
- [ ] Query de **posse** retorna verdadeiro só quando há `media_file` `present`.
- [ ] `sqlc generate` limpo, pacote compila, testes de CRUD passam; catraca verde.

## 6. Notas de implementação / decisões

- **Posse não é coluna gerada.** Colunas geradas do SQLite só referenciam a própria
  linha; posse depende de `media_file`. Fica **computada na query** (EXISTS). Se
  virar gargalo, materializa-se por trigger — evolução, não agora ([ADR-0009](../docs/adr/0009-posse-e-status-eixos-ortogonais.md)).
- **FTS5 por triggers:** mantém `entity_fts` em sincronia sem a app lembrar de
  atualizar; `content=''`/`content_rowid` conforme o padrão FTS5 externo.
- **`state` em vez de flag booleana** para `media_file`/`library_source`: nomeado e
  extensível, casa com o vocabulário do ciclo de vida ([ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md)).
- **Fechar a doc 04:** este briefing promove o esboço a schema real; atualizar a
  seção de tabelas e o diagrama ER (incluir `state`/`last_seen`), registrando que o
  restante (`media_external` etc.) continua esboço até a Fase 3.
- **Código sqlc gerado:** decidir versionar-no-repo vs gerar-no-build; recomendação
  — **versionar** (revisável no PR, não exige sqlc no CI de build). Fechar aqui.

## 7. Fora deste briefing / follow-ups

- Tabelas do resolver externo/override (`media_external`, `media_override`) e de
  pessoas (`person`, `credit`, `company`) na Fase 3/4.
- Índices adicionais guiados por consultas reais (perfilar depois).
- Materialização de posse por trigger, se o EXISTS custar.
