# 0012 — Schema da Fase 3: materializar as camadas `externo` e `override`

> A fundação da Fase 3: criar as tabelas `media_external` (cache de payload por
> fonte) e `media_override` (patch esparso do dono). Só schema + acesso a dados —
> nenhum comportamento novo ainda; o resolver de 3 camadas ([0013](0013-resolver-tres-camadas.md))
> é quem passa a usá-las.

| | |
|---|---|
| **ID** | 0012 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Docs relacionadas** | [04-modelo-de-dados](../docs/04-modelo-de-dados.md), [ADR-0004](../docs/adr/0004-override-como-blob-json-esparso.md), [ADR-0011](../docs/adr/0011-external-ids-autoritativo-media-external-cache.md), [ADR-0017](../docs/adr/0017-migrations-goose-embedado.md) |
| **Status** | done |
| **Depende de** | [0007](0007-schema-nucleo-minimo.md) |

---

## 1. Objetivo

A Fase 2 fechou o núcleo (`entity`, `media_file`, `library_source`, `asset`,
`entity_relation`, `entity_fts`) e o resolver de **uma** camada (arquivo). A Fase 3
precisa das **outras duas camadas** que o modelo prevê ([04](../docs/04-modelo-de-dados.md)):
o **externo** (payload puxado de fontes, base editorial) e o **override** (patch
esparso do dono, autoridade final). Este briefing só as **materializa** — tabelas
e acesso a dados — para que os briefings seguintes (resolver, enriquecimento,
editor de override) tenham onde ler e gravar. É deliberadamente sem
comportamento: um schema errado aqui contamina toda a fase.

## 2. Jornada do usuário

Nenhuma mudança visível ao dono nesta entrega — é fundação. A jornada que ela
**habilita** (enriquecer um item e sobrepor um título) aparece nos briefings
[0015](0015-enriquecimento-import-proxy-imagem.md)/[0016](0016-editor-override-ui.md).
O critério honesto aqui é técnico: as tabelas existem, migram e desmigram limpo,
e as queries geradas compilam.

## 3. Escopo

**Dentro:**
- **Migration `0004`** (goose embedado, [ADR-0017](../docs/adr/0017-migrations-goose-embedado.md))
  criando **`media_external`** e **`media_override`**, reversível (Down).
- **`media_external`** — cache de payload por fonte efetivamente buscada
  (`id, entity_id, source, source_id, payload_json, fetched_at`), FK
  `ON DELETE CASCADE` para `entity`, **`UNIQUE(entity_id, source)`** (uma linha de
  cache por fonte por entidade) e índice para lookup por `(source, source_id)`.
- **`media_override`** — patch esparso, **uma linha por entidade**
  (`entity_id PK, override_json, updated_at`), FK `ON DELETE CASCADE`.
- **Queries sqlc** de acesso (upsert/get/delete de cada), em **ASCII** com colunas
  explícitas (regra do projeto — sqlc v1.31 corrompe SQL de query com acento).
- Testes de migração (up/down deixa o schema na versão esperada) e de round-trip
  das queries (grava e lê `payload_json`/`override_json` intactos).

**Fora (explicitamente):**
- **`person`/`credit`/`company`** e o override de elenco/equipe (MD6) — **adiados**
  para um briefing próprio no fim da Fase 3 (ver §7); esta entrega não os cria.
- A **composição** das camadas (o resolver lê `media_external`/`media_override`) —
  é o [0013](0013-resolver-tres-camadas.md).
- Qualquer **provider**, chamada externa ou preenchimento real das tabelas — 0014+.
- Validação do `override_json` contra JSON Schema (R1) — entra com o editor
  ([0016](0016-editor-override-ui.md)); aqui a coluna é só `TEXT`.

## 4. Contrato técnico

### Dados / modelo
Materializa o esboço já previsto na [doc 04](../docs/04-modelo-de-dados.md)
("Esboço de tabelas ainda não materializadas"):

```
media_external  (id, entity_id -> entity (ON DELETE CASCADE), source, source_id,
                 payload_json TEXT, fetched_at,
                 UNIQUE(entity_id, source),
                 INDEX (source, source_id))
media_override  (entity_id PK -> entity (ON DELETE CASCADE),
                 override_json TEXT, updated_at)
```

- **`payload_json`** guarda o payload **cru** da fonte ([ADR-0011](../docs/adr/0011-external-ids-autoritativo-media-external-cache.md)):
  serve para re-compor sem re-puxar. `source` é o slug da fonte (`"tmdb"`).
- **`source_id` é subordinado** ao `entity.external_ids` (invariante MD4:
  `media_external.source_id == external_ids[source]`) — a **app** mantém isso ao
  gravar; o schema só guarda o valor. Não editar `media_external` à mão.
- **`override_json`** espelha o formato da entity, só com campos mudados
  ([ADR-0004](../docs/adr/0004-override-como-blob-json-esparso.md)); **presença da
  chave = override ativo** (R6) — regra de composição do 0013, não do schema.
- FKs `ON DELETE CASCADE`: só a **purga** explícita da entity remove as camadas
  (coerente com o núcleo da Fase 2 e o ciclo de vida não-destrutivo).

### Backend (Go)
- `internal/db/migrations/0004_*.sql` (Up/Down goose). `SchemaVersion` final = **4**.
- `internal/db/queries/media_external.sql` + `media_override.sql` → sqlc gera em
  `sqlcgen/`. **Escritas pelo pool de escrita** ([ADR-0010](../docs/adr/0010-concorrencia-sqlite-wal-escritor-unico.md)); leituras pelo de leitura.
- Upsert de `media_external` por `(entity_id, source)`; upsert de `media_override`
  por `entity_id`.

### Arquivos afetados
- Criar: migration `0004_*.sql`, `queries/media_external.sql`,
  `queries/media_override.sql`, testes (`internal/db/*_test.go`).
- Alterar: nada de comportamento; regenerar `sqlcgen/` (arquivos versionados).

## 5. Critérios de aceite

- [ ] Migration `0004` cria `media_external` e `media_override`; `SchemaVersion` sobe para **4**; o **Down** as remove e volta para 3 (teste up/down).
- [ ] `media_external` tem `UNIQUE(entity_id, source)`, índice por `(source, source_id)` e FK `ON DELETE CASCADE`; purgar a entity remove as linhas (teste).
- [ ] `media_override` tem `entity_id` como PK (1:1) e FK `ON DELETE CASCADE`.
- [ ] Queries sqlc (upsert/get/delete) compilam, estão em ASCII com colunas explícitas, e um round-trip preserva `payload_json`/`override_json` byte a byte.
- [ ] Leituras usam o pool de leitura; escritas, o de escrita. Catraca (cobertura/lint/vuln) verde.

## 6. Notas de implementação / decisões

- **Só schema, de propósito.** Separar "criar as tabelas" de "compor as camadas"
  ([0013](0013-resolver-tres-camadas.md)) mantém cada briefing testável e evita um
  PR gigante misturando fundação com o coração do resolver.
- **`UNIQUE(entity_id, source)`** materializa "uma linha de cache por fonte por
  entidade" — o upsert por essa chave é como o re-enriquecimento atualiza sem
  duplicar (ADR-0011).
- **Invariante MD4 é responsabilidade da app**, não do schema: SQLite não expressa
  bem `source_id == json_extract(external_ids, '$.'||source)` como constraint
  portável; o 0015 (que grava) e o 0018 (que corrige identidade) o garantem.
- **`override_json` sem validação aqui**: a validação por JSON Schema (R1/R5) vive
  onde o override é **escrito** ([0016](0016-editor-override-ui.md)); esta camada é
  só persistência.

## 7. Fora deste briefing / follow-ups

- **Elenco/equipe (MD6):** tabelas `person`/`credit`/`company` e o override de
  créditos por operações — briefing próprio no fim da Fase 3 (decidido adiar; o
  MVP de override cobre campos simples e listas).
- **`company`↔`entity`** (produtora/estúdio) — junto do MD6.
- Colunas geradas/índices extras sobre `payload_json` se algum lookup justificar.
