# 0025 — Coleção como entity interna: importável da TMDB + criável/editável manual

> A coleção deixa de ser só uma visão externa (0024) e vira uma ENTITY de primeira
> classe (`type='collection'`), com membros ligados por `entity_relation`. Pode nascer
> de um import da TMDB OU ser criada do zero pelo dono (sem source), misturando membros
> externos (importados) e locais. Herda resolução por camadas, override por idioma e
> arte própria — como qualquer item. Decisão do dono: "as duas".

| | |
|---|---|
| **ID** | 0025 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — extensão pós-revisão |
| **Docs relacionadas** | [03-modelo-dados](../docs/03-modelo-dados.md), [04-resolver](../docs/04-resolver.md), [05-api](../docs/05-api.md) |
| **Status** | fase A feita (importável); fase B pendente |
| **Depende de** | [0024](0024-paridade-detalhe-entity-rico.md) |

---

## 1. Objetivo

Hoje (0024) a coleção é **fetch-through externo**: `/collection/$source/$id` puxa a TMDB ao
vivo, sem persistência nem entity; se a fonte cai, a página cai. O dono quer a coleção
**internalizada** nas DUAS formas: (a) **importável** da TMDB (vira entity com os filmes
membros ligados) e (b) **criável/editável manual** (o dono monta uma coleção do zero,
sem source, agrupando itens da biblioteca). A externa (0024) permanece para navegar
ANTES de importar.

## 2. Modelo (SEM migração — o schema já suporta)

- **Coleção = `entity` com `type='collection'`** (`type` é TEXT livre, sem CHECK).
  - **Importada**: `external_ids={tmdb:<colId>}`; nome/overview/artes semeados da TMDB.
  - **Local**: sem `external_ids`; nome dado pelo dono.
- **Membros = `entity_relation(parent_id=coleção, child_id=filme, kind='collection')`**
  (CRUD sqlc já existe: `CriarEntityRelation`/`ListarRelacoesFilhas`/`ApagarEntityRelation`;
  o comentário da tabela já prevê "colecao<->filme"). `UNIQUE(parent,child,kind)` dedup
  a ligação. CASCADE: purgar a coleção solta as relações (não apaga os filmes); purgar
  um filme o remove das coleções.
- **Ordem dos membros**: por ANO (read-time) na 1ª fase — reordenação manual exigiria uma
  coluna `ord` (migração) e fica como follow-up. Para a ordenação funcionar em itens
  só-importados (sem arquivo), o enrich passou a DENORMALIZAR `entity.release_date` da
  fonte no `persistir` (como já faz com `original_language`); não muda a resolução
  (`release_date` tem base=externo), só popula a coluna base para leitura barata.

## 3. Escopo — Fase A (importável da TMDB)

**Backend**
- `POST /import` aceita `type='collection'`: find-or-create da entity coleção (dedup por
  `external_ids.tmdb`), busca as parts (reusa `provider.BuscarColecao`, 0024), importa
  cada part (find-or-create do filme, o `Importar` atual) e liga via `entity_relation`
  (`kind='collection'`). Semeia nome/overview + cacheia poster/backdrop da coleção
  (reusa `cachearArtes`). **Importa TODAS as parts** (é o ponto da coleção; coleções são
  pequenas — knob para franquias grandes fica como follow-up).
- Resolver/leitura: `GET /entities/{id}` de uma coleção inclui os **membros** (cards
  internos, reusa `entityCardDTO`) — via campo novo `members` no `ResolvedEntity` quando
  `type='collection'`, OU `GET /entities/{id}/members`. (Decisão de forma na implementação.)
- openapi + `gen:types`.

**Frontend**
- Página interna da coleção: `/entities/{id}` com `type='collection'` renderiza a
  `RichDetailView` com os **membros** no lugar de similares (grid de cards → `/entities/{childId}`).
- Botão **"Importar coleção"** na página externa `/collection/$source/$id` → `POST /import`
  `type=collection`, navega para `/entities/{novoId}`.

## 4. Escopo — Fase B (criação manual + edição de membros)

**Backend**
- `POST /collections` (ou `POST /entities` com `type=collection`): cria coleção LOCAL
  (nome do dono, sem source, `status` apropriado).
- `POST /entities/{id}/members` `{child_id}` liga; `DELETE /entities/{id}/members/{childId}`
  desliga (valida que o parent é coleção e o child existe).
- Override de nome + arte já funcionam (é entity) — edição "de graça".

**Frontend**
- Criar coleção manual (form simples).
- Adicionar/remover itens: um PICKER da biblioteca (busca local) na página da coleção.
- Membros misturam origem externa (importados) e local — indistintos no grid (são todos
  entities).

## 5. Decisões / follow-ups

- **Importa todas as parts** na Fase A (recomendado). Knob de "só ligar as que já existem
  + importar sob demanda" = follow-up.
- **Ordem por ano**; reordenação manual (coluna `ord` + migração) = follow-up.
- **Coleção multi-idioma/cache** (herda o débito do 0024): nome/overview da coleção
  seguem o padrão de resolução por idioma quando importados.
- **Séries↔temporadas↔episódios** usam a MESMA `entity_relation` (outro `kind`) — fora
  deste briefing, mas o mecanismo é o mesmo.
