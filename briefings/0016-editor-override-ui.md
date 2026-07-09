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
| **Status** | draft |
| **Depende de** | [0013](0013-resolver-tres-camadas.md) |

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

## 4. Contrato técnico

### API (`/api/v1`)
- **PUT/PATCH `/entities/{id}/override`** body = patch esparso espelhando a entity;
  `200` com a entity **resolvida** (para a UI refletir na hora). Envelope de erro padrão.
- Semântica de chave: presente (mesmo `null`) = override; ausente = herda; endpoint
  de remoção para limpar.

### Dados / modelo
- Upsert em `media_override(entity_id, override_json, updated_at)` ([0012](0012-schema-fase3-camadas-externo-override.md)).
  Validação por schema na **app** (a coluna é `TEXT`).

### Frontend (Vite/React)
- Modo de edição no `EntityDetail`; formulário derivado dos campos do núcleo; editor
  de listas com add/remove; distinção visual "limpar" vs "vazio"; estados de erro
  (validação) inline.

### Arquivos afetados
- Criar: handler de override, validação de patch, componentes de edição, testes.
- Alterar: `openapi.yaml` (+ regen tipos), `EntityDetail` do [0011](0011-frontend-grid-lista-detalhe.md), cliente de API.

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
