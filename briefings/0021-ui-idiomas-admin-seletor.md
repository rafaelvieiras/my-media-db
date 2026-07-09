# 0021 — UI de idiomas: admin de config + seletor de leitura

> A superfície do multi-idioma para o dono: uma tela de **Configurações** para definir
> principal/fallback/permitidos, e um **seletor de idioma** na leitura que passa o
> `?lang=` — mostrando, via proveniência, quando um campo caiu no fallback.

| | |
|---|---|
| **ID** | 0021 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Docs relacionadas** | [ADR-0018](../docs/adr/0018-modelo-multi-idioma-dimensao-nas-camadas.md), [ADR-0019](../docs/adr/0019-resolucao-por-idioma-fallback-proveniencia.md), [05-api](../docs/05-api.md) |
| **Status** | ready |
| **Depende de** | [0019](0019-fundacao-multi-idioma-settings-resolver.md), [0020](0020-provider-multi-idioma-traducao-tags.md) |

---

## 1. Objetivo

Dar ao gestor o controle dos idiomas e ao leitor a escolha do idioma. Consome os
endpoints do [0019](0019-fundacao-multi-idioma-settings-resolver.md)/[0020](0020-provider-multi-idioma-traducao-tags.md)
(settings + `?lang=` + `resolved_languages`), já vivos no backend. É a primeira vez
que o multi-idioma aparece na tela: o dono configura os idiomas, escolhe em que idioma
lê e **vê quando um campo saiu no fallback** — a deixa para depois traduzir no editor
([0016](0016-editor-override-ui.md)).

## 2. Jornada do usuário

1. O dono abre **Configurações → Idiomas**: vê principal `pt-BR`, fallback `en`,
   permitidos `[pt-BR, en]`. Adiciona `es` aos permitidos e salva. A validação impede
   salvar se principal/fallback saírem dos permitidos.
2. No cabeçalho há um **seletor de idioma** (os permitidos); ele escolhe `en`.
3. Ao abrir um filme importado, título/sinopse/tags aparecem em inglês. Escolhe `pt-BR`
   e a mesma tela recompõe em português (Duna / Ficção científica).
4. Num item que só tem inglês, ao ler em `pt-BR` a sinopse aparece em inglês com um
   **selo discreto** ("em inglês") — sinalizando o fallback. É a base para ele depois
   adicionar a tradução pt-BR manualmente.

## 3. A decisão de design que sustenta tudo

O **idioma de leitura é um estado global leve** (não um search param por rota): um
`LanguageProvider` (React context) guarda o `lang` atual, **semeado** pelo `primary`
das settings e **persistido** em `localStorage`. O seletor no `AppHeader` o altera; as
queries de leitura (`getEntity`, `listEntities`) passam a **incluir o `lang` na
queryKey e no `?lang=`**, então trocar o idioma re-busca e recompõe sem recarregar a
página. A **lista de permitidos** (do `GET /settings/languages`) é a fonte única que
alimenta tanto o seletor quanto o admin.

Shape **plano preservado** (ADR-0019): a UI só troca um parâmetro; nenhum campo vira
objeto por idioma. A proveniência (`resolved_languages`) é lida como mapa lateral só
para o selo de fallback.

## 4. Contrato técnico

### API client (`web/src/lib/api.ts`)
- `getEntity(id, lang?)` e `listEntities(params, lang?)` ganham `lang` opcional →
  acrescentam `?lang=` (via `URLSearchParams`, junto dos params atuais da lista).
  `lang` vazio/omitido = sem o parâmetro (backend cai no principal — compat).
- `getLanguageSettings(): Promise<LanguageSettings>` e
  `putLanguageSettings(body): Promise<LanguageSettings>` (GET/PUT `/settings/languages`,
  via `lerJson`/`erroDaResposta`; o 422 do PUT vira `ApiError` com a mensagem pt-BR).

### Query options (`web/src/lib/queries.ts`)
- `entityQueryOptions(id, lang?)` e `entitiesQueryOptions(params, lang?)` incluem o
  `lang` na **queryKey** (`['entity', id, lang]`, `['entities', params, lang]`) — troca
  de idioma = cache-miss = re-busca.
- `languageSettingsQueryOptions` (`['settings', 'languages']`).

### Estado de idioma (`web/src/lib/language-context.tsx`, novo)
- `LanguageProvider` no `Wrap` do router (ou volta do QueryClientProvider): expõe
  `{ lang, setLang, allowed, primary }`. Semente: `primary` das settings; se houver
  valor salvo em `localStorage` **e** ainda ∈ `allowed`, usa-o; senão `primary`.
  `setLang` persiste em `localStorage`. Enquanto as settings carregam, cai no `primary`
  default (pt-BR) — nunca quebra.
- `useLanguage()` hook. Componentes de leitura (detalhe, grid) consomem o `lang` daqui.

### Componentes / rotas
- **Seletor** (`web/src/components/language-selector.tsx`, novo): `Select` (shadcn) com
  os `allowed`; troca via `setLang`. Mora no `AppHeader` (à direita). Esconde-se se só
  houver um idioma permitido.
- **Configurações** (`web/src/routes/settings.tsx`, nova rota `/settings`): formulário
  — `Select` para principal e fallback, **multi-select** para permitidos (checkbox list
  com os candidatos; ou toggles). Validação client-side: principal e fallback ∈
  permitidos, permitidos não-vazio (espelha o backend; o 422 do servidor é o guarda
  final e sua mensagem é exibida inline). Salva via `putLanguageSettings`; no sucesso,
  **invalida** `['settings','languages']`, `['entity']` e `['entities']` (as leituras
  dependem dos permitidos). Link "Configurações" no `AppHeader`.
- **Detalhe** (`entity-detail.tsx`): passa `lang` ao `entityQueryOptions`. Renderiza as
  **tags** (hoje não mostra) como `Badge`s. **Selo de fallback**: para `description` e
  cada campo traduzível, quando `resolved_languages[campo]` existe e `!== lang` (e não
  é `""`), um `Badge`/texto discreto "em {idioma}" ao lado — rótulo de idioma legível
  (`pt-BR → Português (BR)`, `en → Inglês`; um pequeno mapa, com fallback ao próprio
  código).
- **Grid** (`routes/index.tsx`): passa `lang` ao `entitiesQueryOptions`. (Os cards
  servem título neutro hoje — a lista aceita `?lang=` e valida; manter o passe do lang
  para coerência e futura resolução de card.)

### Rótulos de idioma
- Um helper `web/src/lib/languages.ts` com o mapa código→nome legível (pt-BR, en, es,
  fr… os comuns) e `rotuloIdioma(codigo)` com fallback ao código. Fonte única para
  seletor, admin e selo.

### Arquivos afetados
- Criar: `lib/language-context.tsx`, `lib/languages.ts`, `components/language-selector.tsx`,
  `routes/settings.tsx`, testes (`*.test.tsx`).
- Alterar: `lib/api.ts` (+lang, +settings), `lib/queries.ts` (+lang, +settings),
  `router.tsx` (rota `/settings` + `LanguageProvider` no Wrap), `components/app-header.tsx`
  (link Configurações + seletor), `routes/entity-detail.tsx` (lang + tags + selo),
  `routes/index.tsx` (lang). Sem tocar backend/openapi (tipos já gerados no 0019/0020).

## 5. Critérios de aceite

- [ ] **Configurações → Idiomas** lê e grava principal/fallback/permitidos; valida
  (principal e fallback ∈ permitidos) e exibe o erro do 422 inline; o multi-select de
  permitidos funciona.
- [ ] O **seletor** no cabeçalho lista os permitidos e troca o idioma de leitura;
  persiste entre reloads (localStorage) e cai no principal quando o salvo não é mais
  permitido.
- [ ] Trocar o idioma no detalhe recompõe título/sinopse/tags (re-busca por `?lang=`)
  **sem recarregar a página**.
- [ ] Quando um campo cai no fallback (`resolved_languages[campo] !== lang`), aparece o
  **selo discreto** com o idioma efetivo.
- [ ] `tsc -b` verde; vitest cobrindo o form de settings, o seletor e o selo; ESLint
  limpo; catraca verde (cobertura web não abaixo do baseline). Validação Playwright ao
  vivo (configurar `es`, importar, alternar pt-BR/en, ver o selo de fallback).

## 6. Notas de implementação / decisões

- **Idioma global via context + localStorage** (não search param): o seletor é do
  `AppHeader` (global), e todas as rotas de leitura o consomem — um estado
  compartilhado é mais simples que threa-lo por search param em cada rota. A queryKey
  com `lang` garante cache correto por idioma.
- **Invalidar leituras no PUT**: mudar os permitidos pode tornar o `lang` atual
  inválido; o provider já revalida no `setLang` (só aceita ∈ allowed). Invalidar
  `entity`/`entities` evita servir cache de um idioma recém-desabilitado.
- **Selo de fallback é leitura pura de `resolved_languages`** — nenhuma lógica de
  idioma na UI além de comparar com o `lang` pedido. Coerente com o shape plano.
- **Reuso shadcn/ui**: `select`, `badge`, `button`, `input` já existem; adicionar
  `label`/checkbox se o multi-select pedir (via CLI shadcn, sem CDN).

## 7. Fora deste briefing / follow-ups

- Editor de override **por idioma** (adicionar/editar a tradução pt-BR faltante) —
  [0016](0016-editor-override-ui.md); edição de tags por idioma (junto do 0016);
  resolução de card por idioma no grid; FTS por idioma.
