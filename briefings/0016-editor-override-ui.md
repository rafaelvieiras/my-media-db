# 0016 — Editor de override (`/api/v1/entities/{id}/override`) + UI (por idioma)

> A autoridade do dono na prática: editar título, sinopse e tags como **patch
> esparso** sobre a base — agora **por idioma** ([0019](0019-fundacao-multi-idioma-settings-resolver.md)),
> com a distinção entre **"limpar override"** e **"definir vazio"** (R6, por idioma) e
> **merge por operações** nas listas (R3). Campos neutros seguem sem idioma.

| | |
|---|---|
| **ID** | 0016 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Docs relacionadas** | [04-modelo-de-dados](../docs/04-modelo-de-dados.md), [ADR-0018](../docs/adr/0018-modelo-multi-idioma-dimensao-nas-camadas.md), [ADR-0019](../docs/adr/0019-resolucao-por-idioma-fallback-proveniencia.md), [ADR-0004](../docs/adr/0004-override-como-blob-json-esparso.md), [ADR-0003](../docs/adr/0003-override-de-listas-por-operacoes.md), [ADR-0006](../docs/adr/0006-presenca-da-chave-sinaliza-override.md), [ADR-0007](../docs/adr/0007-fatos-tecnicos-sao-overridaveis.md) |
| **Status** | done |
| **Depende de** | [0013](0013-resolver-tres-camadas.md), [0015](0015-enriquecimento-import-proxy-imagem.md), [0019](0019-fundacao-multi-idioma-settings-resolver.md), [0021](0021-ui-idiomas-admin-seletor.md) |

---

## 1. Objetivo

O modelo inteiro existe para isto: **dar autoridade final ao dono** ([04](../docs/04-modelo-de-dados.md)).
O resolver já compõe as camadas **por idioma** ([0019](0019-fundacao-multi-idioma-settings-resolver.md))
e a fonte já enche os idiomas ([0020](0020-provider-multi-idioma-traducao-tags.md)); falta
a superfície para o dono **escrever** o override — o patch esparso que vence tudo, no
idioma que ele escolher. É o que transforma "o TMDB só trouxe inglês" em "eu, dono,
escrevo o título em pt-BR" — exatamente o caso que o selo de fallback do [0021](0021-ui-idiomas-admin-seletor.md)
sinaliza.

## 2. Jornada do usuário

1. No detalhe de um item lido em **pt-BR**, a sinopse aparece em inglês com o selo
   "em Inglês" (caiu no fallback — [0021](0021-ui-idiomas-admin-seletor.md)).
2. O dono clica **Editar**, digita a sinopse em português e salva. O selo some: agora
   há override **em pt-BR** que vence a base.
3. Muda o título; troca o seletor para **en** e vê que o override pt-BR não afetou o
   inglês (cada idioma é independente).
4. Nas **tags**, adiciona uma e remove outra — a base externa daquele idioma segue viva
   por baixo; um re-scrape futuro que traga tag nova a inclui.
5. Para um campo, escolhe **"limpar override"** (volta a herdar a base/fallback) ou
   **"definir vazio"** (apaga o valor que a fonte insiste em trazer **naquele idioma**,
   interrompendo o fallback) — são ações distintas.
6. Edita **título original** e **data** (campos **neutros**): valem para todos os
   idiomas, sem seletor.

## 3. A decisão de design que sustenta tudo

O `override_json` gravado **É** o patch que o resolver ([0019](0019-fundacao-multi-idioma-settings-resolver.md))
já sabe interpretar. Para campos **traduzíveis** (`title`, `description`, `tags`) o
resolver lê `override[campo]` como um **mapa por idioma** (`{"pt-BR":…, "en":…}`); para
campos **neutros** (`original_title`, `release_date`, `extension_data`) lê o valor
escalar/objeto direto. **O endpoint não inventa semântica** — só mescla o patch no blob
e devolve a entity resolvida. Zero query sqlc nova (reusa `UpsertMediaOverride`/
`ObterMediaOverride`/`ApagarMediaOverride` do [0012](0012-schema-fase3-camadas-externo-override.md)).

**O `?lang=` é o pincel**: a UI edita no idioma de leitura corrente ([0021](0021-ui-idiomas-admin-seletor.md)).
Um `PATCH …?lang=pt-BR` com `{"title":"Duna"}` grava `override.title["pt-BR"]="Duna"`,
**fundindo** com os outros idiomas já presentes (merge de dois níveis). Campos neutros
no mesmo corpo ignoram o `lang`. Assim a UI nunca monta o mapa por idioma na mão — o
handler o faz, guiado pela **etiqueta `traduzivel` do resolver** (fonte única, R1).

**Forma canônica do blob** (o que o resolver lê):
```json
{
  "title":       { "pt-BR": "Duna", "en": null },
  "description": { "pt-BR": "Em um futuro distante…" },
  "tags":        { "pt-BR": { "add": ["Clássico"], "remove": ["Guerra"] } },
  "original_title": "Dune",
  "release_date": null
}
```
- `title.en = null` → **definir vazio** em inglês (apaga a base e **interrompe** o
  fallback naquele idioma — R6 por idioma).
- ausência da chave `title.es` → **herda** (segue a cadeia de fallback).
- `original_title`/`release_date` escalares → neutros, um valor para todos os idiomas.

## 4. Contrato técnico

### Resolver — expor a etiqueta (R1, sem duplicar conhecimento)
- Adicionar em `internal/resolver` um helper EXPORTADO que o server consome para saber
  quais campos são editáveis e quais são traduzíveis — **sem** o server redigitar a
  lista (que viraria uma segunda fonte de verdade). Ex.: `resolver.CamposEditaveis()
  map[string]CampoInfo` (ou `EhTraduzivel(campo string) (traduzivel, existe bool)` +
  `Kind(campo)`), derivado do `registroOrigem`. Só leitura; não muda a composição.

### API (`/api/v1`) — três verbos, um por intenção; `?lang=` escopa os traduzíveis
- **PATCH `/entities/{id}/override?lang=<code>`** — **merge esparso**. `lang` default =
  principal das settings; se presente, deve ∈ permitidos (senão `400 invalid_query`).
  As chaves do corpo são fundidas no `override_json`:
  - **campo traduzível** → funde em `override[campo][lang]` (deep-merge de 2 níveis:
    preserva os outros idiomas do mesmo campo).
    - **definir vazio** (idioma): `{"title": null}` → `override.title[lang] = null`.
    - **definir valor**: `{"title":"Y"}` → `override.title[lang]="Y"`;
      `{"tags":{"add":["x"],"remove":["y"]}}` → `override.tags[lang]={add,remove}`.
  - **campo neutro** → funde escalar em `override[campo]` (o `lang` é ignorado p/ ele).
  - `200` com a entity **resolvida** no `lang` (a UI reflete na hora). `404` entity
    inexistente; `422` patch malformado (chave não-editorial ou forma inválida).
  - **Migração de legado:** se `override[campo]` de um traduzível estiver num escalar
    legado (0015/pré-0016, sem chave de idioma), tratá-lo como valor do **principal** ao
    fundir (envolve em `{principal: <legado>}` antes de aplicar o novo idioma).
- **DELETE `/entities/{id}/override/{campo}?lang=<code>`** — **limpar override**:
  - traduzível **com** `lang` → remove só `override[campo][lang]`; se o mapa do campo
    ficar vazio, remove a chave `campo`.
  - traduzível **sem** `lang`, ou campo **neutro** → remove a chave `campo` inteira.
  - `200` resolvida; remover chave ausente é idempotente (`200`); `422` se `{campo}` não
    é editorial. Se o blob inteiro ficar `{}`, a linha de override é removida.
- **DELETE `/entities/{id}/override`** — **reset**: apaga o override inteiro. `200`.
- **GET `/entities/{id}/override`** — devolve o **blob cru** guardado (`{}` se nenhum)
  para a UI distinguir "overridado" de "herdado" **por campo e por idioma**. `200`
  (entity existente); `404` se não existe.

### Dados / modelo
- Upsert em `media_override(entity_id, override_json, updated_at)` ([0012](0012-schema-fase3-camadas-externo-override.md)).
  Coluna `TEXT`; **validação é da app**.
- **Merge no Go (dois níveis):** decodifica o blob atual e o patch como
  `map[string]json.RawMessage`. Para cada chave do patch: se **traduzível**, garante que
  `blob[campo]` é objeto (migra legado → `{principal:…}`), decodifica-o em
  `map[string]json.RawMessage`, seta `mapa[lang] = valorDoPatch` (inclusive `null`),
  re-serializa; se **neutro**, `blob[campo] = valorDoPatch` (escalar, como no 0013).
  Preserva o `RawMessage` cru — não reinterpreta o valor (o resolver interpreta na
  leitura).

### Validação do patch (app)
- **Chaves permitidas** = os campos editoriais do registro (via o helper do resolver):
  `title`, `original_title`, `description`, `release_date`, `tags`, `extension_data`.
  Chave fora disso → `422` (não gravar `status`/`id`/`external_ids`/derivados).
- **Forma por kind:** escalar (traduzível ou neutro) aceita `string` ou `null`; `tags`
  aceita array de strings, objeto `{add?:[],remove?:[]}` ou `null`; `extension_data`
  aceita objeto ou `null`. Forma inválida → `422`, envelope padrão.
- **`lang`** (query) ∈ permitidos das settings; ausente = principal.

### Frontend (Vite/React) — edita no idioma corrente
- **Modo de edição no `EntityDetail`** ([0021](0021-ui-idiomas-admin-seletor.md)): botão
  **Editar** → formulário. Os campos **traduzíveis** (title/description/tags) editam o
  **idioma de leitura atual** (`useLanguage().lang`); um rótulo deixa claro "editando em
  {idioma}". Os campos **neutros** (original_title/release_date) editam uma vez, sem
  idioma. Editor de `tags` com chips add/remove.
- **Overridado é visível:** consulta `GET .../override`; um campo com override no idioma
  atual ganha um selo ("editado"); o selo "em {idioma}" de fallback do [0021](0021-ui-idiomas-admin-seletor.md)
  convive (herdado × editado são estados distintos). Cada campo traduzível tem ação
  **Limpar** (→ `DELETE .../{campo}?lang=atual`, volta a herdar naquele idioma) —
  distinta de esvaziar o input (que, ao salvar, manda `null` = **definir vazio**).
- **Salvar** → `PATCH ?lang=atual` com só os campos alterados; sucesso invalida
  `['entity', id]` (todas as variações de idioma) e `['entities']`, sai do modo de
  edição; erro de validação inline.
- **Trocar o idioma** no seletor recarrega o editor no novo idioma (mesmo componente).

### Arquivos afetados
- Criar (backend): `internal/server/override.go` (handlers PATCH/DELETE/GET + merge de 2
  níveis + validação), testes. Helper exportado em `internal/resolver` (+ teste). Sem
  pkg nova nem query sqlc nova.
- Criar (frontend): componentes de edição (form + editor de listas) em `web/src/`.
- Alterar: `openapi.yaml` (+ regen tipos; `tsc -b` verde), `internal/server/server.go`
  (rotas), `web/src/routes/entity-detail.tsx`, `web/src/lib/api.ts` + `queries.ts`.

## 5. Critérios de aceite

- [x] Editar o título **em pt-BR** grava `override.title["pt-BR"]` e o resolver passa a
  servi-lo por cima da base **só em pt-BR**; ler em `en` não é afetado.
- [x] **Limpar override** (`DELETE …/{campo}?lang=`) remove a chave do idioma (volta a
  herdar/fazer fallback); **definir vazio** (`null` no idioma) apaga a base e interrompe
  o fallback — distintos e testados (R6 por idioma).
- [x] `tags` editam por `{add, remove}` **dentro do idioma**, com a base externa daquele
  idioma viva por baixo (R3 por idioma).
- [x] Campo **neutro** (`original_title`/`release_date`) grava escalar único (sem
  idioma) e vale em qualquer `?lang=`.
- [x] Patch malformado / chave não-editorial / `lang` não permitido são rejeitados
  (`422`/`400`, envelope de erro). Merge preserva os outros idiomas do mesmo campo.
- [x] A UI edita no idioma corrente, mostra o que está overridado e reflete o valor
  resolvido na hora. Catraca verde (Go + type-check web). Validação Playwright ao vivo
  (adicionar a sinopse pt-BR faltante e ver o selo de fallback sumir).

**Validação ao vivo (backend por curl + UI por Playwright, item Dune):** override
`title` pt-BR não afetou o `en` (título "Duna — Edição do Dono" em pt-BR × "Dune" em
en); add de tags `es` com a base `es` viva por baixo (R3); `description es=null` apagou
e não caiu no fallback (R6); "Limpar" voltou a herdar; blob cru em 2 níveis. Na UI: o
botão **Editar** abre o editor "Editando em Português (BR)"; salvar o título fez surgir
o selo **"editado"** e trocar para `en` mostrou "Dune" sem selo — isolamento por idioma
provado end-to-end.

## 6. Notas de implementação / decisões

- **"Limpar ≠ vazio", agora por idioma** (R6): sem os dois, o dono não consegue apagar
  um valor teimoso da fonte **num idioma** nem desfazer a edição daquele idioma — a UI
  **tem** de separar as ações, e o `lang` as escopa.
- **Merge de 2 níveis é o cerne**: fundir `{"title":{"en":"X"}}` sobre
  `{"title":{"pt-BR":"Y"}}` dá `{"title":{"pt-BR":"Y","en":"X"}}` — nunca substitui o
  objeto inteiro do campo. Falhar nisso apagaria traduções de outros idiomas.
- **R1 preservado**: o server não redigita a lista de campos/traduzíveis — pergunta ao
  resolver (helper exportado). Uma fonte de verdade.
- **Patch esparso** mantém o não-destrutivo: só o idioma/campo que o dono tocou é dele;
  o resto evolui com re-scrape.
- Reusar shadcn/ui (form/input/badge do [0021](0021-ui-idiomas-admin-seletor.md)/[0011](0011-frontend-grid-lista-detalhe.md)).

## 7. Fora deste briefing / follow-ups

- Override de créditos (MD6); UI dedicada a corrigir mediainfo (R7); arte custom
  ([0017](0017-arte-custom-upload-ui.md)); expor proveniência por campo como recurso
  próprio (possível Fase 4).
