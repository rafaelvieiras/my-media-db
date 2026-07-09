# 0016 — Editor de override (`/api/v1/entities/{id}/override`) + UI

> A autoridade do dono na prática: editar título, sinopse, tags e gêneros como
> **patch esparso** sobre a base — com a distinção entre **"limpar override"** e
> **"definir vazio"** (R6) e **merge por operações** nas listas (R3).

| | |
|---|---|
| **ID** | 0016 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Docs relacionadas** | [04-modelo-de-dados](../docs/04-modelo-de-dados.md), [ADR-0004](../docs/adr/0004-override-como-blob-json-esparso.md), [ADR-0003](../docs/adr/0003-override-de-listas-por-operacoes.md), [ADR-0006](../docs/adr/0006-presenca-da-chave-sinaliza-override.md), [ADR-0007](../docs/adr/0007-fatos-tecnicos-sao-overridaveis.md) |
| **Status** | ready |
| **Depende de** | [0013](0013-resolver-tres-camadas.md), [0015](0015-enriquecimento-import-proxy-imagem.md) |

---

## 1. Objetivo

O modelo inteiro existe para isto: **dar autoridade final ao dono** ([04](../docs/04-modelo-de-dados.md)).
Com o resolver compondo as camadas ([0013](0013-resolver-tres-camadas.md)), falta a
superfície para o dono **escrever** o override — o patch esparso que vence tudo. É o
que transforma "o TMDB disse X" em "eu, dono, digo Y".

## 2. Jornada do usuário

1. No detalhe de um item, o dono clica **Editar**.
2. Muda o título; o campo mostra que agora há override (vence a base externa).
3. Numa lista (gêneros), **adiciona** um e **remove** outro — a base externa segue
   viva por baixo; um re-scrape futuro que traga gênero novo o inclui.
4. Para um campo, escolhe **"limpar override"** (volta a herdar) ou **"definir vazio"**
   (apaga o valor que a fonte insiste em trazer) — são ações distintas.

## 3. Escopo

**Dentro:**
- **`PUT/PATCH /api/v1/entities/{id}/override`** — grava o patch esparso em
  `media_override` ([0012](0012-schema-fase3-camadas-externo-override.md)/[ADR-0004](../docs/adr/0004-override-como-blob-json-esparso.md)).
- **`DELETE .../override/{campo}`** (ou omissão da chave) — **limpar override** de um
  campo (remove a chave → volta a herdar); distinto de gravar `null` (**definir
  vazio**, R6/[ADR-0006](../docs/adr/0006-presenca-da-chave-sinaliza-override.md)).
- **Merge de listas** (`tags`/`genres`): API aceita `{add, remove}` (R3/[ADR-0003](../docs/adr/0003-override-de-listas-por-operacoes.md)).
- **Validação** do patch contra o schema (núcleo; base do R1) antes de gravar.
- **UI de edição** no detalhe: campos simples + editor de listas; indica o que está
  overridado; ações "limpar" vs "vazio".
- Testes: patch grava/compõe; limpar ≠ vazio; merge de listas; validação rejeita
  patch malformado.

**Fora (explicitamente):**
- Override de **relações** (elenco/equipe, MD6) — briefing próprio.
- Override de **fatos técnicos** (R7) é permitido pelo mesmo mecanismo, mas a UI
  dedicada a mediainfo fica como follow-up (uso de exceção).
- **Proveniência por campo** exposta como recurso próprio (R6: UI consulta o override
  separadamente; sem endpoint de proveniência agora).

> **A chave do design:** o `override_json` gravado **É** o patch que o resolver
> ([0013](0013-resolver-tres-camadas.md)) já sabe interpretar — escalar (`null`
> apaga), lista (`{add,remove}` / array = substitui / `null` = vazia),
> `extension_data` (namespace/chave, `null` apaga). O endpoint **não traduz** nada:
> mescla o patch no blob e devolve a entity resolvida. Zero query sqlc nova (reusa
> `UpsertMediaOverride`/`ObterMediaOverride`/`ApagarMediaOverride` do [0012](0012-schema-fase3-camadas-externo-override.md)).

### API (`/api/v1`) — três verbos, um por intenção
- **PATCH `/entities/{id}/override`** — **merge esparso**: as chaves do corpo são
  fundidas no `override_json` guardado (só o que veio muda; o resto do override
  permanece). Corpo = patch espelhando os campos editoriais. `200` com a entity
  **resolvida** (a UI reflete na hora). `404` se a entity não existe; `422` patch
  malformado.
  - **Definir vazio** (apagar o valor que a fonte insiste em trazer): mande a chave
    com `null` — vira `null` no blob (R6/[ADR-0006](../docs/adr/0006-presenca-da-chave-sinaliza-override.md)), o resolver apaga a base.
  - **Definir valor**: `{"title":"Y"}` (escalar) ou `{"tags":{"add":["x"],"remove":["y"]}}` (lista).
- **DELETE `/entities/{id}/override/{campo}`** — **limpar override** de um campo:
  remove a chave do blob → o campo volta a **herdar** a base. Distinto de `null`.
  `200` resolvida; remover chave ausente é idempotente (`200`); `422` se `{campo}`
  não é editorial.
- **DELETE `/entities/{id}/override`** — **reset**: apaga o override inteiro (remove
  a linha). `200` resolvida.
- Se, após um DELETE de campo, o blob ficar `{}`, a linha é removida (sem override).

### Dados / modelo
- Upsert em `media_override(entity_id, override_json, updated_at)` ([0012](0012-schema-fase3-camadas-externo-override.md)).
  A coluna é `TEXT`; a **validação é da app**.
- **Merge no Go:** decodifica o blob atual e o patch como `map[string]json.RawMessage`,
  aplica as chaves do patch (sobrescreve/insere), re-serializa. Preserva o `RawMessage`
  — não reinterpreta o valor (o resolver é quem interpreta na leitura).

### Validação do patch (app)
- **Chaves permitidas** = os campos editoriais do registro do resolver:
  `title`, `original_title`, `description`, `release_date`, `tags`, `extension_data`.
  Chave fora disso → `422` (não gravar `status`/`id`/`external_ids`/derivados).
- **Forma por kind:** escalares aceitam `string` ou `null`; `tags` aceita array de
  strings, objeto `{add?:[],remove?:[]}` ou `null`; `extension_data` aceita objeto
  ou `null`. Forma inválida → `422`, envelope padrão.

### Frontend (Vite/React)
- **Modo de edição no `EntityDetail`** ([0011](0011-frontend-grid-lista-detalhe.md)):
  botão **Editar** → formulário dos campos editoriais (inputs para title/original_title/
  description/release_date; editor de `tags` com chips add/remove).
- **Overridado é visível:** um campo com override ativo ganha um selo ("editado");
  cada campo tem ação **Limpar** (→ `DELETE .../{campo}`, volta a herdar) — distinta de
  esvaziar o input (que, ao salvar, manda `null` = **definir vazio**).
- **Salvar** → `PATCH` com só os campos alterados; sucesso invalida `['entity', id]`
  (e `['entities']` para o grid) e sai do modo de edição; erro de validação inline.
- A UI sabe o que está overridado consultando `GET .../override` (o blob cru), abaixo.
- **GET `/entities/{id}/override`** — devolve o blob cru guardado (`{}` se nenhum) para
  a UI distinguir "overridado" de "herdado" por campo. `200` sempre (entity existente),
  `404` se a entity não existe.

### Arquivos afetados
- Criar (backend): `internal/server/override.go` (handlers PATCH/DELETE/GET + merge +
  validação), testes. Sem pkg nova nem query sqlc nova.
- Criar (frontend): componentes de edição (form + editor de listas) em `web/src/`.
- Alterar: `openapi.yaml` (+ regen tipos), `internal/server/server.go` (rotas),
  `web/src/routes/entity-detail.tsx`, `web/src/lib/api.ts` + `queries.ts`.

## 5. Critérios de aceite

- [ ] Editar título grava override e o resolver passa a servi-lo por cima da base.
- [ ] **Limpar override** remove a chave (volta a herdar); **definir vazio** grava `null` (apaga a base) — comportamentos distintos e testados (R6).
- [ ] `tags`/`genres` editam por `{add, remove}` com a base externa viva por baixo (R3).
- [ ] Patch malformado é rejeitado pela validação de schema; a resposta usa o envelope de erro.
- [ ] A UI mostra o que está overridado e reflete o valor resolvido na hora. Catraca verde.

## 6. Notas de implementação / decisões

- **"Limpar ≠ vazio" é o detalhe que sustenta a autoridade** (R6): sem ele, o dono
  não consegue apagar um valor teimoso da fonte nem desfazer uma edição — a UI **tem**
  de separar as duas ações.
- **Patch esparso** (não substituição total) mantém o não-destrutivo: só o que o dono
  tocou é dele; o resto evolui com re-scrape.
- Reusar shadcn/ui (form/input/badge do [0011](0011-frontend-grid-lista-detalhe.md)).

## 7. Fora deste briefing / follow-ups

- Override de créditos (MD6); UI dedicada a corrigir mediainfo (R7); expor
  proveniência por campo (possível Fase 4).
