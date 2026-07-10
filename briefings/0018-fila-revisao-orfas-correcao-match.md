# 0018 — Fila de revisão + órfãs + correção de match

> Fecha o loop de curadoria que a Fase 2 só **sinalizou**: o badge de baixa confiança
> e os `draft` não-identificados ganham **ação** — confirmar, corrigir (re-identificar)
> ou purgar — e as **órfãs** (arquivo sumiu, zero investimento do dono) entram numa
> fila de limpeza que nunca apaga sozinha.

| | |
|---|---|
| **ID** | 0018 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Docs relacionadas** | [06-biblioteca-e-scan](../docs/06-biblioteca-e-scan.md), [04-modelo-de-dados](../docs/04-modelo-de-dados.md), [ADR-0008](../docs/adr/0008-identidade-e-matching-kodi-jellyfin.md), [ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md) |
| **Status** | done |
| **Depende de** | [0013](0013-resolver-tres-camadas.md), [0015](0015-enriquecimento-import-proxy-imagem.md), [0016](0016-editor-override-ui.md) |

---

## 1. Objetivo

O scanner ([0009](0009-scanner-fswalk-ffprobe-matching.md)) **nunca trava a
biblioteca**: casa o melhor palpite e **sinaliza** o incerto ([06](../docs/06-biblioteca-e-scan.md)).
A Fase 2 mostrou o badge, mas não deu o que fazer com ele. Este briefing entrega a
**curadoria**: confirmar/corrigir matches de baixa confiança, tratar drafts
não-identificados e gerenciar **órfãs** — sempre não-destrutivo, com **purga** como
única ação destrutiva e sempre explícita.

## 2. Jornada do usuário

1. O dono abre **Revisão**: itens casados com baixa confiança (*"mapeado com 60% —
   confirmar ou corrigir?"*) e `draft` não-identificados.
2. Num item, **confirma** (aceita o palpite, some o alerta), **corrige** (informa o id
   correto da fonte → re-enriquece e passa a valer) ou deixa para depois.
3. Abre **Órfãs**: entidades auto-criadas cujo arquivo sumiu e sem investimento do
   dono. Pode **purgar** em lote (explícito) ou ignorar; nada some sozinho.

## 3. A decisão de design que sustenta tudo

**Sem tabela nova — as filas são QUERY, a correção reusa o que já existe, a purga é a
única porta destrutiva** ([ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md)):

- **Revisão** = `entity.status='draft'` (não-identificado) **OU** existe `media_file`
  `present` com `match_confidence > 0 AND < LIMIAR` (baixa confiança). O `LIMIAR` é o
  mesmo `LIMIAR_BAIXA_CONFIANCA=70` da UI ([0011](0011-frontend-grid-lista-detalhe.md)/
  `entity-badges.ts`), agora **fonte única no backend** (a query o recebe/embute).
- **Órfã** = predicado: a entity **tem** `media_file`, **todos** `missing` (nenhum
  `present`), `status='cataloged'` (auto-criada pelo scanner, não `wanted`/`unreleased`
  = intenção do dono, nem `draft` = revisão), **e sem investimento do dono** (sem linha
  em `media_override`, sem `asset` `origin='user'`). Uma entity SÓ importada (sem
  `media_file`) **não** é órfã.
- **Confirmar** = o dono diz "o palpite está certo": sobe o `match_confidence` dos
  `media_file` da entity para `100` (autoridade do dono, some o alerta). Reusa uma query
  curta.
- **Corrigir** = "o palpite está errado, o certo é este": grava `external_ids[source]`
  com o id correto (**identidade vence**, MD4) e **re-enriquece** pelo serviço do
  [0015](0015-enriquecimento-import-proxy-imagem.md) (`enrich.Enriquecer`, multi-idioma
  do [0020](0020-provider-multi-idioma-traducao-tags.md)); sobe a confiança. Não toca
  override/arte do dono (não-destrutivo).
- **Purga** = `ApagarEntity` (a linha da entity); as FKs `ON DELETE CASCADE`
  ([0012](0012-schema-fase3-camadas-externo-override.md)/[0007](0007-schema-nucleo-minimo.md))
  levam `media_file`/`media_external`/`media_override`/`asset` junto. Única ação
  destrutiva, sempre explícita, nunca do sistema.

## 4. Contrato técnico

### API (`/api/v1`)
- **GET `/review`** — fila de revisão: cards (mesmo shape `EntityCard` do [0011](0011-frontend-grid-lista-detalhe.md),
  com `match_confidence`) dos itens `draft` **ou** baixa confiança. `200` (`[]` vazio).
  Ordenar por confiança asc (pior primeiro), drafts ao fim ou por título.
- **GET `/orphans`** — fila de órfãs (cards das entidades pelo predicado acima). `200`.
- **POST `/entities/{id}/confirm`** — confirma o match: sobe `match_confidence` dos
  `media_file` da entity para `100`. `200` com a entity **resolvida**; `404` inexistente.
- **POST `/entities/{id}/correct`** — corrige: corpo `{source, source_id}` (default
  `source="tmdb"`). Grava `external_ids[source]=source_id`, **re-enriquece** e sobe a
  confiança. `200` com a entity resolvida; `422` corpo inválido; `404` inexistente;
  `502` se a fonte falhar (como o import). Requer o serviço de enrich ligado (`503`
  senão, como [0015](0015-enriquecimento-import-proxy-imagem.md)).
- **DELETE `/entities/{id}`** — **purga** individual (`ApagarEntity` + CASCADE; o binário
  de assets em disco é limpo best-effort). `200`/`204`; `404` inexistente.
- **POST `/entities/purge`** — **purga em lote**: corpo `{ids: [...]}`; purga cada uma
  (idempotente por id ausente). `200` com o resumo (quantas removidas). Envelope de erro
  padrão em tudo.

### Dados / modelo
- **Sem tabela/migração nova.** Filas são queries sqlc novas (ASCII, colunas explícitas):
  `ListarRevisao` (com o limiar como parâmetro), `ListarOrfas` (o predicado com
  `NOT EXISTS media_override`/`asset origin='user'` e agregação de `media_file.state`).
  Correção reusa `AtualizarExternalIds` + o serviço `enrich`. Confirmar usa uma query
  nova `AtualizarConfidencePorEntity(entity_id, valor)`. Purga usa `ApagarEntity`.
- **"Investimento do dono"** como predicado explícito e testável: `NOT EXISTS` de
  `media_override(entity_id)` **e** `NOT EXISTS` de `asset(entity_id, origin='user')`.
  (Coleção manual ainda não existe no schema — quando existir, entra no predicado.)

### Backend (Go)
- `internal/server/review.go` (ou similar): handlers das filas + confirmar/corrigir/
  purgar. Reusa `resolverEntity` para devolver o item re-resolvido; o pool de escrita
  para confirm/correct/purge; o `exigirEntity` do [0016](0016-editor-override-ui.md).
  Correção chama `s.enrich.Enriquecer(ctx, id, source, sourceID, tipo, langs)` (langs das
  settings, como enrich/import). Purga em lote itera pelos ids.
- `LIMIAR_BAIXA_CONFIANCA` no backend como const única (espelha o front; documentar que
  os dois devem casar — ou expor via um endpoint de config no futuro).

### Frontend (Vite/React)
- **Rota `/review`** e **rota `/orphans`** (links no `AppHeader`). Cada card com ações:
  - Revisão: **Confirmar** (POST confirm), **Corrigir** (abre busca/entrada do id
    correto — pode reusar o `SearchPanel`/fluxo de import do [0015](0015-enriquecimento-import-proxy-imagem.md)
    para escolher o título certo → POST correct), e link ao detalhe.
  - Órfãs: **Purgar** (com **confirmação explícita** — modal/confirm), seleção em lote.
- O **badge de revisão** do [0011](0011-frontend-grid-lista-detalhe.md)/`entity-badges.ts`
  vira **link** para `/review`. Invalidar `['entities']`/`['entity', id]`/`['review']`/
  `['orphans']` após cada ação.

### Arquivos afetados
- Criar (backend): queries de fila + `AtualizarConfidencePorEntity` (+ regen sqlc),
  `internal/server/review.go` + testes, rotas em `server.go`.
- Criar (frontend): rotas `/review` e `/orphans` + componentes, `api.ts`/`queries.ts`.
- Alterar: `openapi.yaml` (+ regen tipos), `AppHeader`, o badge do [0011](0011-frontend-grid-lista-detalhe.md).

## 5. Critérios de aceite

- [x] A fila de revisão lista baixa confiança (`0 < conf < 70`, arquivo `present`) +
  drafts; **confirmar** sobe a confiança (some o alerta); **corrigir** grava
  `external_ids` + re-enriquece e re-resolve.
- [x] A fila de órfãs lista só entidades auto-criadas (`cataloged`), com arquivo(s) e
  **todos** `missing`, e **sem investimento do dono**; um item com override/asset custom
  **nunca** aparece como órfã (testado).
- [x] **Purga** (individual e lote) é sempre explícita e remove os derivados
  (`media_file`/external/override/assets via CASCADE); nada é apagado como efeito
  colateral de outra ação.
- [x] A correção do dono é autoritativa (identidade vence, MD4) e reflete no resolver;
  não toca override/arte do dono. Catraca verde (Go + type-check web).

**Validação ao vivo (backend por curl/SQL + UI por Playwright, cenários semeados):**
Revisão listou "baixa confiança (40%)" (à frente) e "não identificado" (draft, ao
fim); **confirmar** subiu a confiança para 100 e tirou o item da fila; **corrigir**
buscou "Dune" no TMDB inline, escolher "Dune (2021)" gravou `external_ids.tmdb=438631`,
populou `media_external` e re-resolveu. Órfãs: **purga individual** — o 1º clique só
revelou a confirmação (`alertdialog`, item intacto no banco), o 2º removeu a entity +
`media_file` via CASCADE; **purga em lote** — "selecionar todas" → "Purgar
selecionadas" abriu "Tem certeza? Isso remove 2 itens…", confirmar apagou as duas.

**Bug pego ao vivo e corrigido (draft preso na fila):** `confirmar`/`corrigir` subiam a
confiança mas **não** promoviam o `status`, então um `draft` identificado voltava para
`draft` na fila para sempre (a query casa `status='draft'`). Correção: query
`PromoverDeDraft` (guardada por `status='draft'`, idempotente, no-op em `wanted`/
`unreleased`/`cataloged`) e o passo compartilhado `finalizarRevisao` — os dois ramos do
predicado (confiança **e** status) fecham juntos. Coberto por `TestConfirmarPromoveDraft`
e `TestCorrigirPromoveDraft`, e reconfirmado ao vivo (draft → `cataloged`, saiu da fila).

## 6. Notas de implementação / decisões

- **Não-destrutivo é a regra** ([ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md)):
  a única porta destrutiva é a **purga**, e ela é sempre do dono, nunca do sistema.
- **Órfã ≠ missing**: `missing` é fato de arquivo; órfã é o predicado "auto-criada +
  sumida + sem investimento". Só a segunda entra na fila de limpeza.
- **Correção = re-enrich por identidade**: reusa o serviço do [0015](0015-enriquecimento-import-proxy-imagem.md)
  (coerência com a fase; multi-idioma de graça) em vez de um caminho de escrita novo.
- **Confirmar sobe a confiança para 100**: pragmático e autoritativo (o dono é a fonte
  final), sem coluna de "revisado" nova. Documentar a semântica.
- **Limiar espelhado front/back**: o `70` vive nos dois; anotar o acoplamento (candidato
  a virar config/endpoint depois, fora deste briefing).

## 7. Fora deste briefing / follow-ups

- Correção de créditos (MD6); merge/split de entidades além do corrigir por identidade;
  tuning do limiar de confiança ([06](../docs/06-biblioteca-e-scan.md), decisão de tuning,
  não de modelo); "coleção manual" no predicado de investimento quando o schema a tiver.
