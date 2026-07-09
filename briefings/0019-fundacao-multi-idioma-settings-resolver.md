# 0019 — Fundação multi-idioma: settings + resolução por idioma

> A dimensão de **idioma** no modelo: config no banco (principal/fallback/permitidos),
> a coluna âncora `original_language`, e o **resolver resolvendo por idioma** com
> cadeia de fallback e proveniência — mantendo o shape plano da API.

| | |
|---|---|
| **ID** | 0019 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Docs relacionadas** | [ADR-0018](../docs/adr/0018-modelo-multi-idioma-dimensao-nas-camadas.md), [ADR-0019](../docs/adr/0019-resolucao-por-idioma-fallback-proveniencia.md), [04-modelo-de-dados](../docs/04-modelo-de-dados.md), [05-api](../docs/05-api.md) |
| **Status** | ready |
| **Depende de** | [0013](0013-resolver-tres-camadas.md) |

---

## 1. Objetivo

O acervo é multi-idioma ([ADR-0018](../docs/adr/0018-modelo-multi-idioma-dimensao-nas-camadas.md)):
o gestor define **idioma principal**, **fallback** e **permitidos**; cada valor
editorial carrega a flag do idioma; a leitura serve o idioma pedido com **fallback**.
Este briefing entrega a **fundação backend** — config (settings), schema, e o
**resolver resolvendo por idioma** — sem ainda mudar o provider (que passa a puxar
traduções no [0020](0020-provider-multi-idioma-traducao-tags.md)) nem a UI ([0021](0021-ui-idiomas-admin-seletor.md)).
É a base que faz o override por idioma ([0016](0016-editor-override-ui.md)) e a
tradução de tags ([0020](0020-provider-multi-idioma-traducao-tags.md)) aterrissarem.

## 2. Jornada do usuário

Indireta mas fundacional: o gestor define em **Configurações** que o idioma
principal é `pt-BR` com fallback `en` (e permite `es`); a partir daí toda leitura
`/entities/{id}?lang=pt-BR` compõe no idioma pedido e, quando falta, cai no fallback
— e a resposta diz **em que idioma** cada campo saiu.

## 3. Escopo

**Dentro:**
- **Settings no banco** ([ADR-0018](../docs/adr/0018-modelo-multi-idioma-dimensao-nas-camadas.md)):
  tabela `app_settings(key, value_json, updated_at)`; chave `languages` =
  `{primary, fallback, allowed[]}`. Endpoints **GET/PUT `/api/v1/settings/languages`**
  (validação: principal e fallback ⊆ permitidos; códigos não-vazios).
- **Migração de schema** (0005): `app_settings` + coluna **`entity.original_language`**
  (âncora do fallback). Sobe a versão do schema.
- **Resolver por idioma** ([ADR-0019](../docs/adr/0019-resolucao-por-idioma-fallback-proveniencia.md)):
  `Camadas` ganha o **contexto de idioma** (idioma pedido + cadeia derivada das
  settings); campos **traduzíveis** (`title`, `description`, `tags`) compõem por
  idioma com precedência `override ⟵ externo ⟵ arquivo` **dentro** do idioma, e a
  cadeia `pedido → fallback → original_language → arquivo` escolhe o idioma efetivo;
  `null` no override do idioma pedido **apaga e interrompe** o fallback (R6 por
  idioma). Campos **neutros** seguem como hoje. Assinatura `Resolver(Camadas) Entidade`
  **preservada**.
- **Proveniência de idioma** no shape resolvido: `resolved_languages` (mapa
  `campo → idioma efetivo`) + eco do `lang` pedido. Shape **plano** mantido.
- **API:** `GET /entities/{id}?lang=` e `GET /entities?lang=` (default = principal das
  settings); `lang` fora dos permitidos → `400 invalid_query`.
- Testes: cadeia de fallback (pedido→fallback→original→arquivo); `null` por idioma
  apaga e não cai no fallback; proveniência correta; regressão (com só arquivo e sem
  settings de idioma, comportamento do 0013 preservado — default `pt-BR`/sem cadeia).

**Fora (explicitamente):**
- **Provider puxando traduções** (`/translations`, gêneros localizados) — [0020](0020-provider-multi-idioma-traducao-tags.md).
  Aqui o externo continua com o que já grava (tratado como um único idioma = o
  principal) até o 0020 preencher os idiomas.
- **UI** de admin de idiomas e seletor de idioma — [0021](0021-ui-idiomas-admin-seletor.md).
- **Override por idioma** (editor) — [0016](0016-editor-override-ui.md) revisado.
- **FTS/busca por idioma** — follow-up.

## 4. Contrato técnico

### Config de idioma (canônico)
- `languages = { "primary": "pt-BR", "fallback": "en", "allowed": ["pt-BR","en","es"] }`.
- Um **helper de settings** (carrega `languages`, com default seguro `primary=pt-BR`,
  `fallback=en`, `allowed=[pt-BR,en]` quando a linha não existe) injetado onde o
  resolver e os handlers precisam. Cache leve em memória com invalidação no PUT.
- **Cadeia de idiomas** para um pedido `lang`: `[lang, fallback, original_language?]`
  dedup, terminando na base neutra (arquivo). `lang` default = `primary`.

### Backend (Go)
- **`internal/db` migração 0005**: `app_settings` (`key` PK, `value_json` TEXT,
  `updated_at`), `ALTER TABLE entity ADD COLUMN original_language TEXT`. Queries sqlc
  (ASCII, colunas explícitas): `ObterAppSetting`, `UpsertAppSetting`. Regenerar sqlc.
- **`internal/resolver`**: `Camadas` ganha `LangPedido string` e `Cadeia []string`
  (ou um `CtxIdioma`). O `registroOrigem` ganha a marca **`traduzivel bool`** por
  campo. A composição de campo traduzível vira **bidimensional** (idioma × camada):
  para cada idioma da cadeia, tenta `override[campo][idioma]` (presença, `null`-aware)
  ⟶ `externo[campo][idioma]` ⟶ (se for o último, base arquivo); a primeira etapa com
  presença define o valor e a **proveniência**. A `CamadaExterna.Campos` passa a
  aceitar, para campo traduzível, `map[idioma]valor` (o 0020 preenche; até lá, o
  handler injeta o valor atual sob o idioma principal).
- **`Entidade`** ganha `ResolvedLanguages map[string]string` (`json:"resolved_languages"`)
  e o handler ecoa o `lang` efetivo. `original_language` entra no shape (neutro).
- **`internal/server`**: `resolverEntity`/list leem as settings, derivam a cadeia,
  passam o contexto ao resolver; parse do `?lang=` com validação contra os permitidos.
  Handlers de settings (`/settings/languages`).
- **openapi.yaml** (+ regen tipos): `?lang=`, `resolved_languages`, `original_language`,
  schema `LanguageSettings`, rotas de settings. Após `gen:types`, rodar `tsc -b` (a
  catraca bloqueia erro de type-check — ver a métrica `web_typecheck_errors`).

### Frontend (Vite/React)
- Nada além dos tipos gerados; a UI vive no [0021](0021-ui-idiomas-admin-seletor.md).

### Arquivos afetados
- Criar: migração 0005 + queries `app_settings`, handler de settings, testes.
- Alterar: `internal/resolver/*` (+ testes), `internal/server/entities.go`
  (contexto de idioma + `?lang=`), `server.go` (rotas settings), `main.go` (se
  precisar do default), `openapi.yaml` (+ tipos).

## 5. Critérios de aceite

- [ ] `GET/PUT /settings/languages` grava e valida (principal/fallback ⊆ permitidos); default seguro quando ausente.
- [ ] `GET /entities/{id}?lang=pt-BR` resolve no idioma pedido; faltando, cai em `fallback`, depois `original_language`, depois arquivo — na ordem.
- [ ] `override[campo]["pt-BR"] = null` **apaga** o campo em pt-BR e **não** cai no fallback (R6 por idioma); ausência da chave segue a cadeia.
- [ ] A resposta traz `resolved_languages` com o idioma efetivo por campo e o shape segue **plano**.
- [ ] `lang` fora dos permitidos → `400`. Sem settings/sem camadas, comportamento do 0013 preservado. Catraca verde (inclui type-check web).

## 6. Notas de implementação / decisões

- **Shape plano preservado é a chave** ([ADR-0019](../docs/adr/0019-resolucao-por-idioma-fallback-proveniencia.md)):
  a UI só passa `lang`; nada de campo virar objeto por idioma. A proveniência é um
  mapa lateral, não muda os campos.
- **Externo tratado como principal até o 0020**: sem o provider multi-idioma, o
  `media_external` atual representa um idioma só; o handler o injeta sob o idioma
  principal para a composição já funcionar de ponta a ponta. Quando o 0020 gravar
  `/translations`, o mesmo caminho passa a ter todos os idiomas — sem mudar o resolver.
- **`traduzivel` é etiqueta no registro** (não `switch`): coerente com R1 — o motor
  compõe por idioma só onde a etiqueta manda; campo neutro nem consulta a cadeia.
- **Settings genérico** (`app_settings` key/value) em vez de tabela dedicada: comporta
  futuras configs (tema, política de scan) sem nova migração.

## 7. Fora deste briefing / follow-ups

- Provider multi-idioma + tradução de tags ([0020](0020-provider-multi-idioma-traducao-tags.md));
  UI de idiomas ([0021](0021-ui-idiomas-admin-seletor.md)); override por idioma
  ([0016](0016-editor-override-ui.md)); FTS/busca por idioma; `Accept-Language` como
  fonte alternativa do `lang`.
