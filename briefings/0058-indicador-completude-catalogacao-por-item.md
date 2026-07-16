# 0058 — Indicador de completude de catalogação por item (listagem + detalhe)

> Selo visual PASSIVO no card e uma seção no detalhe mostrando, item a item,
> o que falta preencher conforme a régua configurada no
> [0057](0057-config-completude-metadados-desejaveis.md).

| | |
|---|---|
| **ID** | 0058 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Docs relacionadas** | [04-modelo-de-dados](../docs/04-modelo-de-dados.md), [05-api](../docs/05-api.md), [08-artwork-e-assets](../docs/08-artwork-e-assets.md) |
| **Status** | done |
| **Depende de** | [0057](0057-config-completude-metadados-desejaveis.md) (config de completude de catalogação — este briefing só CONSOME) |

---

## 1. Objetivo

O [0057](0057-config-completude-metadados-desejaveis.md) entrega a régua (o
que o dono considera "bem catalogado" por tipo de mídia), mas uma régua sem
leitor não muda nada na experiência: o dono ainda teria que abrir item por
item para saber se falta sinopse, elenco ou um pôster. Este briefing fecha
esse ciclo — **avalia cada item contra a régua** e torna o resultado visível
em dois lugares onde o dono já olha hoje: o card da listagem/grid (sinal
rápido, "algo falta aqui") e a tela de detalhe (o que exatamente falta, para
decidir se vale a pena ir atrás).

É um indicativo **passivo**: sinaliza, não age. Baixar o que falta é escopo
do [0059](#7-fora-deste-briefing--follow-ups) (grupo de botões de ação), que
vai **depender** deste selo para saber o que oferecer.

**Distinção crítica, já fixada pelo 0057 (§6) e repetida aqui para quem ler
só este briefing:** isto é **completude de CATALOGAÇÃO** (campos/artes
preenchidos), um selo **diferente** da "completude de POSSE" já existente em
`web/src/lib/entity-badges.ts` (`estadoCompletude`/`temCompletude`/
`unidadeCompletude`, briefings 0036/0037), que mede quantos episódios/
temporadas o dono TEM no disco dentro de uma árvore de série. Os dois selos
podem coexistir no MESMO card (uma série pode ter todas as temporadas — posse
completa — e ainda assim faltar sinopse ou elenco — catalogação incompleta).
Este briefing **não reusa** `entity-badges.ts` — ver §4 para o novo módulo.

## 2. Jornada do usuário

1. O dono já configurou a régua em `/settings` (0057) — por exemplo, exigiu
   `description` + `cast` + arte `poster`+`fanart` para `tv`.
2. Ele abre a Home (grid de cards). Uma série que só tem sinopse e pôster
   (sem elenco catalogado, sem fanart) mostra um ícone discreto de alerta
   (triângulo, `TriangleAlert` do `lucide-react` — mesmo ícone já usado em
   `orphans.tsx`/`query-states.tsx` para avisos) no canto superior-esquerdo
   do card, ao lado de onde hoje aparece o badge de revisão (quando há um).
3. Ao passar o mouse sobre o ícone (ou focar via teclado), um tooltip lista o
   que falta em PT-BR: "Catalogação incompleta: elenco, arte fanart".
4. Um item que atende à régua inteira (ou cujo tipo não tem nada exigido, ex.
   `collection` sem config) **não** mostra nada — capa limpa, mesmo princípio
   do selo de posse (0038): ausência de problema não gera ruído visual.
5. Um item **draft** (nunca casado, [0056](#7-fora-deste-briefing--follow-ups))
   também não mostra o selo — já tem o badge "não identificado" (que tem
   precedência) e não faz sentido avaliar completude de metadados que ainda
   nem existem.
6. O dono clica no card e abre o detalhe. Lá, uma seção "Catalogação
   incompleta" (mesmo ícone de alerta no título) lista os itens faltando em
   PT-BR, um por linha — a mesma lista do tooltip, só que sempre visível (sem
   depender de hover), útil também em touch/mobile.
7. Se o item atende à régua, a seção simplesmente não aparece no detalhe —
   nenhuma confirmação positiva ("está tudo certo!") é necessária; consistente
   com o princípio de sinalizar só o que precisa de atenção.
8. O dono muda a régua em `/settings` (ex. passa a exigir `runtime` também).
   A tela de detalhe do próximo item que ele abrir já reflete a régua nova
   (é calculada na hora). O selo na listagem/grid só atualiza depois da
   próxima execução do job de saúde da biblioteca ([0053](0053-job-saude-biblioteca.md))
   — ver §6 para o porquê dessa diferença de frescor.

## 3. Escopo

**Dentro:**
- Função Go pura que avalia UM item (dados já resolvidos) contra a config de
  um tipo (0057) e devolve `completo bool` + `faltando []string` (chaves
  estáveis, ex. `description`, `cast`, `artwork:fanart`).
- **Detalhe (`GET /api/v1/entities/{id}`)**: campo novo `catalog_completeness`
  no `ResolvedEntity`, calculado **ao vivo**, a cada request, reaproveitando
  os dados que `resolverEntity` já monta (sem query extra) — ver §6.
- **Listagem/membros (`GET /api/v1/entities`, `GET /api/v1/entities/{id}/members`)**:
  mesmo campo no `EntityCard`, mas lido de um **cache persistido**, recalculado
  em lote pelo job `saude_biblioteca` ([0053](0053-job-saude-biblioteca.md)) —
  ver §6 para por que a listagem NÃO calcula ao vivo por item.
- Nova tabela `entity_catalog_completeness` (cache recomponível, mesmo
  espírito de `person`/`credit`/`company` do 0029 — pode ser apagada e
  regerada sem perda) para guardar o resultado por item entre execuções do
  job.
- Badge novo no card (`poster-card.tsx`), ícone-only, `TriangleAlert`,
  acessível (`aria-label`/`title` com a lista do que falta).
- Seção nova no detalhe (`entity-detail.tsx`) listando o que falta, só
  renderizada quando incompleto.
- Novo módulo de helpers puros `web/src/lib/catalog-completeness-badges.ts`
  (espelha o padrão de `entity-badges.ts`, sem reusar suas funções).
- Extensão do resumo do job `saude_biblioteca` com a contagem de itens
  avaliados/incompletos (mesmo padrão do resumo de revisão/órfãs do 0053).

**Fora (explicitamente):**
- **Qualquer ação de correção/download do que falta** — isso é o
  [0059](#7-fora-deste-briefing--follow-ups) (grupo de botões de ação), que
  consome este selo mas não é implementado aqui.
- **Alertas/notificações proativas** do job sobre itens incompletos (e-mail,
  webhook, badge fora da tela de biblioteca) — o job só ATUALIZA o cache; o
  indicativo continua sendo passivo, só visível quando o dono está olhando a
  UI. Mesmo princípio do 0053 (job "só relata").
- **Avaliar itens draft** — draft é escopo do
  [0056](#7-fora-deste-briefing--follow-ups) (job tenta enriquecer via
  provider); um item sem match não tem metadados resolvidos para avaliar.
- **Recalcular sob demanda** (ex. ao salvar um override, ao re-importar UM
  item) fora do ciclo do job de saúde — o cache da listagem só atualiza na
  próxima execução do job (ver §6, follow-up em §7).
- **Granularidade de "quase completo"** (ex. barra de progresso 4/6 campos)
  — o pedido é binário (completo/incompleto) com a lista do que falta; uma
  barra de progresso é um refinamento visual possível, não pedido agora.
- **Alterar a config em si** — isso é 0057; este briefing só LÊ.

## 4. Contrato técnico

### Dados / modelo

Nova migration `internal/db/migrations/0013_entity_catalog_completeness.sql`:

```sql
-- +goose Up
-- Cache RECOMPONÍVEL (mesmo espírito do person/credit/company do 0029): o
-- resultado da avaliação de completude de catalogação (0057) por item,
-- recalculado em lote pelo job saude_biblioteca (0053/0058). Nunca guarda
-- dado do dono — pode ser apagado e regerado sem perda. 1:1 com entity;
-- ausência de linha = "ainda não avaliado" (item novo, ou draft).
CREATE TABLE entity_catalog_completeness (
    entity_id    TEXT PRIMARY KEY REFERENCES entity (id) ON DELETE CASCADE,
    complete     INTEGER NOT NULL, -- 0/1
    missing_json TEXT    NOT NULL, -- ex.: ["description","cast","artwork:fanart"]
    evaluated_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- +goose Down
DROP TABLE entity_catalog_completeness;
```

Chaves de `missing`/`faltando` (estáveis, usadas tanto no JSON persistido
quanto na resposta ao vivo do detalhe — o front mapeia as duas com o MESMO
helper):
- `description`, `runtime`, `certification`, `keywords`, `cast`, `media_file`
  — espelham 1:1 os bools de `tipoCompletenessConfig` (0057).
- `artwork:<kind>` — um item por kind AUSENTE da lista `artwork` configurada
  (ex. `artwork:fanart`), não um único `artwork` agregado — para o tooltip/
  detalhe dizer EXATAMENTE qual arte falta, não só "falta alguma arte".

### API

Novo shape reusado em dois DTOs (`EntityCard` e `ResolvedEntity`):

```json
"catalog_completeness": {
  "complete": false,
  "missing": ["cast", "artwork:fanart"]
}
```

- `null` quando o item é **draft** (nunca avaliado) ou quando ainda não
  passou pela primeira execução do job (só na listagem — ver §6; no detalhe
  nunca é null para item não-draft, pois o cálculo é sempre ao vivo).
- `complete: true, missing: []` quando o item atende à régua OU quando o tipo
  não tem entrada na config (0057: "tipo ausente = nada exigido" — nunca
  marca incompleto). A UI não renderiza nada nesse caso (badge/seção só
  aparecem com `complete === false`).
- Sem endpoint próprio — o campo só existe embutido nas respostas já
  existentes de `GET /api/v1/entities`, `GET /api/v1/entities/{id}`,
  `GET /api/v1/entities/{id}/members`. Nenhuma rota nova.
- `openapi.yaml`: schema `CatalogCompletenessResult` (`complete: boolean`,
  `missing: string[]`), referenciado como `catalog_completeness` (nullable)
  em `EntityCard` e `ResolvedEntity` + regenerar tipos (`gen:types`) + `tsc -b`.

### Backend (Go)

- **`internal/server/completeness.go`** (novo arquivo):
  - `type dadosCompletude struct { Description *string; Runtime *int;
    Certification *string; Keywords []string; TemElenco bool; TemArquivo
    bool; ArtesPresentes map[string]bool }` — shape DESACOPLADO da origem dos
    dados, para ser preenchido tanto a partir de `resolver.Entidade` (caminho
    ao vivo do detalhe) quanto de uma leitura mais leve no job (caminho em
    lote da listagem).
  - `type resultadoCompletude struct { Completo bool; Faltando []string }`
    (tags json `complete`/`missing`).
  - `func avaliarCompletude(cfg tipoCompletenessConfig, achou bool, dados
    dadosCompletude) resultadoCompletude` — pura, sem I/O. `achou=false`
    (tipo ausente na config) devolve `{true, []}` direto (regra do 0057).
    Senão, testa cada bool configurado contra `dados` e cada kind de
    `cfg.Artwork` contra `dados.ArtesPresentes`, acumulando as chaves
    ausentes na ordem fixa `description, runtime, certification, keywords,
    cast, media_file, artwork:<kind...>` (ordem estável = tooltip/lista
    previsíveis, útil pra teste e pra não reordenar a UI a cada render).
  - `func dadosCompletudeDeEntidade(e resolver.Entidade) dadosCompletude` —
    adapta o shape já resolvido do detalhe (Description/Runtime/
    Certification/Keywords/Cast/Owned + a lista de assets já carregada em
    `resolverEntity`) para `dadosCompletude`, sem nenhuma query extra.

- **Caminho ao vivo (detalhe)** — `internal/server/entities.go`, dentro de
  `resolverEntity` (linha ~622-700, onde `resolvida resolver.Entidade` já
  está montada com Ownership/Ancestors/StillURL anotados): adicionar
  `s.anotarCompletudeCatalogacao(ctx, &resolvida, ent)` como último passo.
  Ela: (a) devolve cedo (campo fica `nil`) se `ehDraft(ent.Status)`; (b) lê
  `obterCatalogCompletenessSettings(ctx)` (helper do 0057, já cacheado em
  memória — nenhum I/O de banco além do que a config já paga); (c) monta
  `dadosCompletude` via `dadosCompletudeDeEntidade`; (d) chama
  `avaliarCompletude` e preenche `resolvida.CatalogCompleteness`. Custo
  marginal: zero queries novas — só um loop sobre bools e a lista de assets
  já em memória.

- **Caminho em lote (job) — `internal/server/completeness_recalc.go`**
  (novo arquivo):
  - `func (s *Server) RecalcularCompletudeCatalogacao(ctx context.Context)
    (avaliados, incompletos int, err error)` — itera TODAS as entities
    não-draft em páginas (reusa `ListarEntities` com `LIMIT/OFFSET`, mesmo
    padrão de paginação interna já usado noutros lugares do pacote), e para
    cada uma: se o tipo não está na config, grava `{true, []}` sem chamar o
    resolver (evita o custo de resolução para tipos sem régua); senão chama
    `s.resolverEntity(ctx, id, "")` (idioma vazio = base neutra, sem cadeia
    de fallback — completude depende de PRESENÇA do campo, não de qual
    idioma populou; reaproveita o MESMO caminho do detalhe, sem duplicar a
    extração de rich fields) e grava o resultado via `UPSERT` em
    `entity_catalog_completeness` (`ON CONFLICT(entity_id) DO UPDATE`).
    Erro de UM item (ex. `sql.ErrNoRows` numa entity purgada no meio do
    loop) não aborta os demais — best-effort, mesmo espírito de
    `escanearTodas`/`autoMatchLote` do 0053/0054.
  - Nova query sqlc `UpsertEntityCatalogCompleteness` (`entity_id, complete,
    missing_json` → upsert) em `internal/db/queries/entity.sql`.

- **Wiring no job `saude_biblioteca`** (`internal/job/libraryhealth`):
  - Nova interface pequena `CompletenessRecalculator` (mesmo padrão de
    `Matcher`/`Contador` — `libraryhealth` **nunca** importa
    `internal/server`, para não criar ciclo, já que `*server.Server` é quem
    injeta o `Handler` de dentro de `server.WithLibraryHealthJob`):
    ```go
    type CompletenessRecalculator interface {
        RecalcularCompletudeCatalogacao(ctx context.Context) (avaliados, incompletos int, err error)
    }
    ```
  - `Handler` ganha um campo `completeness CompletenessRecalculator`
    (opcional — `nil` degrada para "não recalcula", mesmo padrão de
    `matcher == nil` no `rodarAutoMatch`), `New(...)` ganha o parâmetro.
  - `Executar`: depois do auto-match (0054), chama
    `h.rodarRecalculoCompletude(ctx)` (best-effort, log-only em erro, mesmo
    padrão de `rodarAutoMatch`) e inclui `avaliados`/`incompletos` na
    mensagem final: `"...; N itens avaliados, M incompletos em catalogação"`.
  - `internal/server/server.go`, `WithLibraryHealthJob()`: passa `s` (que já
    satisfaz `Matcher`) também como `CompletenessRecalculator` —
    `libraryhealth.New(s.library, s.scanner, curation.New(s.db), s, s)` ou
    equivalente (a implementação decide se agrupa as duas interfaces
    satisfeitas por `*Server` num único parâmetro ou mantém dois — detalhe
    de assinatura, não de arquitetura).

- **Card DTO** — `internal/server/entities.go`: `entityCardDTO` ganha
  `CatalogCompleteness *resultadoCompletude \`json:"catalog_completeness"\``.
  `cardDTO` (linha ~239) e `cardDTODeMembro` (linha ~514) passam a ler as
  colunas novas (`complete`, `missing_json`) da query `ListarEntityCards`/
  `ListarEntityCardsDesc`/`ObterEntityCard`, que ganham um `LEFT JOIN
  entity_catalog_completeness ecc ON ecc.entity_id = e.id` (mesmo padrão dos
  `EXISTS`/subselects já usados para `owned`/`has_poster` — aqui é um JOIN
  simples porque a linha já vem pronta do cache, não precisa agregação).
  `NULL` (sem linha ainda) vira `catalog_completeness: null` no DTO.

### Frontend (Vite/React)

- **`web/src/lib/catalog-completeness-badges.ts`** (novo, puro, testável —
  espelha `entity-badges.ts` sem reusar suas funções, conforme decisão do
  0057 §6):
  - `export type CatalogCompleteness = components['schemas']['CatalogCompleteness'] | null`.
  - `precisaAtencaoCatalogacao(cc: CatalogCompleteness): boolean` →
    `!!cc && !cc.complete`.
  - `rotuloItemFaltando(chave: string): string` — mapa PT-BR: `description`
    → "sinopse", `runtime` → "duração", `certification` → "classificação
    indicativa", `keywords` → "palavras-chave", `cast` → "elenco",
    `media_file` → "arquivo de mídia"; `artwork:<kind>` → "arte " + rótulo do
    kind (mesmo mapa de rótulos de kind usado no formulário do 0057:
    poster/fanart/logo/clearart/disc/banner/thumb). Chave desconhecida ecoa
    crua (nunca quebra a UI, mesmo padrão de `rotuloTipo`).
  - `rotuloTooltipCompletude(cc: CatalogCompleteness): string` — monta
    "Catalogação incompleta: {lista join(', ')}" a partir de `missing`
    mapeado por `rotuloItemFaltando`.

- **`web/src/components/poster-card.tsx`**: novo componente
  `IndicadorCompletudeCatalogacao({ card })`, renderizado no MESMO container
  `flex-wrap` do topo-esquerdo, **depois** de `<EstadoMatchBadge card={card} />`
  (ambos cabem lado a lado — `flex-wrap` já existe). Ícone-only: `<Badge
  variant="outline" className="pointer-events-none gap-1 px-1.5"><TriangleAlert
  className="size-3.5" aria-hidden /></Badge>` com `aria-label`/`title` =
  `rotuloTooltipCompletude(card.catalog_completeness)`. Renderiza `null`
  quando `!precisaAtencaoCatalogacao(card.catalog_completeness)` — capa
  limpa no caso comum.

- **`web/src/routes/entity-detail.tsx`**: nova seção `SecaoCompletudeIncompleta`
  (ou dentro de um slot da `RichDetailView`, a decidir na implementação —
  qualquer um dos dois é aceitável desde que apareça perto do topo/metadados,
  não escondida no fim da página), renderizada só quando
  `precisaAtencaoCatalogacao(entity.catalog_completeness)`: título com ícone
  `TriangleAlert` + `<ul>` com um `<li>` por `rotuloItemFaltando(chave)`.

- **`web/src/lib/api-types.ts`**: regenerado (`gen:types`) a partir do
  `openapi.yaml` — sem edição manual.

### Arquivos afetados

- Criar: `internal/db/migrations/0013_entity_catalog_completeness.sql`,
  `internal/server/completeness.go` (+ `completeness_test.go`),
  `internal/server/completeness_recalc.go` (+ teste),
  `web/src/lib/catalog-completeness-badges.ts` (+ teste).
- Alterar: `internal/db/queries/entity.sql` (novas colunas no `LEFT JOIN` das
  queries de card + `UpsertEntityCatalogCompleteness`), `internal/server/entities.go`
  (`entityCardDTO`, `cardDTO`, `resolverEntity`/`anotarCompletudeCatalogacao`),
  `internal/resolver/resolver.go` (`Entidade.CatalogCompleteness`),
  `internal/job/libraryhealth/libraryhealth.go` (`CompletenessRecalculator`,
  `rodarRecalculoCompletude`, mensagem final), `internal/server/server.go`
  (`WithLibraryHealthJob`), `openapi.yaml` (+ tipos gerados),
  `web/src/components/poster-card.tsx`, `web/src/routes/entity-detail.tsx`.

## 5. Critérios de aceite

- [ ] `GET /api/v1/entities/{id}` de um item não-draft devolve
      `catalog_completeness` calculado NA HORA (mudar a config em 0057 e
      pedir o mesmo item de novo reflete a config nova sem precisar rodar o
      job).
- [ ] `GET /api/v1/entities/{id}` de um item **draft** devolve
      `catalog_completeness: null`.
- [ ] Item de um tipo AUSENTE da config (0057) devolve `{complete: true,
      missing: []}` (nunca incompleto por default de tipo sem régua).
- [ ] `RecalcularCompletudeCatalogacao` grava uma linha por entity não-draft
      em `entity_catalog_completeness`, e `GET /api/v1/entities` passa a
      devolver `catalog_completeness` não-nulo para elas.
- [ ] O card na listagem mostra o ícone de alerta só quando `complete ===
      false`; o `title`/`aria-label` lista os itens faltando em PT-BR.
- [ ] O detalhe mostra a seção "Catalogação incompleta" só quando
      incompleto, com a mesma lista.
- [ ] Rodar `POST /api/v1/jobs/saude_biblioteca/run` inclui na mensagem final
      a contagem de itens avaliados/incompletos.
- [ ] `entity_catalog_completeness` some junto quando a entity é purgada
      (`ON DELETE CASCADE`) — sem linha órfã.
- [ ] `avaliarCompletude` tem cobertura de teste unitário para cada bool +
      cada kind de artwork, isolado (sem banco).
- [ ] Catraca verde (inclui `tsc -b` do web após `gen:types`).

## 6. Notas de implementação / decisões

- **Por que o detalhe calcula AO VIVO e a listagem usa CACHE persistido —
  a decisão central deste briefing.** O comentário no topo de
  `internal/job/libraryhealth/libraryhealth.go` estabelece que review/órfãs
  são "QUERY AO VIVO" porque são predicados SQL baratos (`EXISTS`/`COUNT`
  sobre `entity`/`media_file`). A completude de catalogação **não é
  uniformemente barata**: `description`/`media_file`/`cast` SÃO baratos
  (colunas diretas ou `EXISTS` numa tabela relacional, mesmo padrão de
  `owned`/`has_poster` já usado em `ListarEntityCards`), mas
  `runtime`/`certification`/`keywords` **não são** — vêm de
  `richfields.IntPtrDe`/`CertificacaoDaRegiao`/etc., que fazem
  `json.Unmarshal` do `payload_json` de `media_external` (o payload CRU do
  provider, que pode ter dezenas de KB) e navegam o mapa resultante. Isso é
  aceitável UMA VEZ por request no detalhe (o `resolverEntity` já paga esse
  custo de qualquer jeito, com ou sem este briefing — a avaliação de
  completude é praticamente de graça ali). Mas multiplicado por até 50-200
  itens de UMA página de listagem (`limitPadrao`/`limitMaximo`), o mesmo
  custo por N vezes por request quebraria a única-query-com-subselects que
  `ListarEntityCards` é hoje. Por isso a listagem lê um valor JÁ CALCULADO
  (cache), preenchido em lote pelo job — o job já paga um custo parecido (ou
  maior) ao re-escanear a biblioteca inteira, então rodar `resolverEntity`
  uma vez por item ali é consistente com o que ele já faz, não um novo tipo
  de trabalho pesado no sistema.
- **Trade-off aceito: a listagem pode ficar até uma execução do job
  desatualizada.** Mesma classe de staleness que review/órfãs já têm em
  relação ao disco (só refletem a realidade depois de um re-scan) — não é um
  padrão novo, é o MESMO padrão aplicado a uma fonte de verdade diferente
  (a config de completude, não o disco). Se o dono muda a régua em
  `/settings`, o detalhe do próximo item que ele abrir já reflete a mudança;
  a listagem só na próxima rodada do job (agendado ou "Executar agora" na
  tela de Jobs, que já existe desde o 0052). Documentado explicitamente na
  jornada (§2, passo 8) para não ser surpresa.
- **Alternativa descartada — calcular ao vivo também na listagem.** Foi
  cogitada (é mais simples de implementar, sem migration nova nem job) mas
  descartada pelo custo por página explicado acima: seria N chamadas a
  `resolverEntity` (cada uma com queries próprias de `media_external`/
  `credit`/`asset` + parse de JSON) por página de grid, quebrando o padrão
  de UMA query com subselects que sustenta a paginação hoje.
- **Alternativa descartada — persistir também no detalhe.** Rejeitada porque
  o detalhe já resolve TUDO (`resolverEntity`) independente deste briefing;
  usar o cache ali só adicionaria staleness sem economizar nada (o custo já
  foi pago). Ao vivo no detalhe é estritamente melhor: mais simples E mais
  atual.
- **Tabela nova (`entity_catalog_completeness`) em vez de colunas em
  `entity`.** Segue o padrão já usado para dado DERIVADO/recomponível
  (`person`/`credit`/`company`, briefing 0029): uma tabela separada deixa
  claro que este é um CACHE (pode ser truncado e regerado por completo sem
  perda), diferente das colunas base de `entity` (dado de import) ou de
  `media_override` (dado do dono). Evita também alargar o `entity` com uma
  coluna cujo `NULL` significaria três coisas diferentes (nunca avaliado /
  draft / tipo sem régua) — a AUSÊNCIA de linha na tabela nova já cobre
  "nunca avaliado" sem precisar de sentinela.
- **Chaves de `missing` estáveis e por-kind (`artwork:<kind>`), não um
  `artwork: bool` agregado.** O dono quer saber EXATAMENTE o que falta (é o
  motivo de existir o tooltip/seção, não só um "está incompleto" genérico) —
  agregar todas as artes num único booleano perderia a informação de QUAL
  kind falta, que o 0059 vai precisar para saber que ação oferecer (ex.
  "baixar fanart" vs "baixar logo").
- **`avaliarCompletude` pura e desacoplada da origem dos dados
  (`dadosCompletude`).** Permite o MESMO teste unitário cobrir a função sem
  precisar de banco, e permite os dois caminhos (ao vivo no detalhe, em lote
  no job) convergirem na mesma regra sem duplicá-la — só o modo de
  PREENCHER `dadosCompletude` muda (um lê de `resolver.Entidade` já pronto,
  o outro chama `resolverEntity` dentro do loop do job).
- **Idioma vazio (`""`) na chamada de `resolverEntity` dentro do job.**
  Completude depende de o CAMPO existir, não de QUAL idioma o preencheu — a
  base neutra é suficiente e mais barata que resolver a cadeia de fallback
  completa para cada item do lote.
- **`entity_id` como PK (não uma tabela de histórico).** Só o estado ATUAL
  importa para o selo — não há necessidade de guardar avaliações passadas;
  `UPSERT` mantém a tabela do tamanho do catálogo, não do catálogo × número
  de execuções do job.
- **Não reusar `entity-badges.ts`.** Decisão herdada do 0057 §6: o novo
  módulo `catalog-completeness-badges.ts` evita colidir nomes
  (`temCompletude` já significa posse) e mantém os dois conceitos
  claramente separados no código, não só na UI.

## 7. Fora deste briefing / follow-ups

- **[0059] Grupo de botões de ação por item** — consome `catalog_completeness.missing`
  para sugerir/disparar downloads do que falta (ex. "baixar fanart"). Este
  briefing só sinaliza; a ação em si é 0059.
- **Recalcular sob demanda** (ao salvar um override, ao re-importar/re-casar
  UM item) em vez de esperar a próxima execução do job — reduziria a janela
  de staleness da listagem para itens editados manualmente. Não feito agora
  para manter o diff focado num único gatilho de recálculo (o job); se a
  staleness incomodar na prática, é um follow-up de baixo risco (chamar
  `avaliarCompletude` + upsert de UMA linha logo após `AtualizarEntity`/
  override, sem tocar o resto do fluxo em lote).
- **Barra de progresso / granularidade parcial** (ex. "4/6 itens
  atendidos") em vez de binário completo/incompleto — refinamento visual
  possível, não pedido.
- **Extender a UI de completude para `season`/`episode`** — hoje esses tipos
  não aparecem na listagem principal (`type NOT IN ('season','episode')`),
  só em `/members`; o campo já funciona ali (mesmo `cardDTODeMembro`) SE um
  dia a config de 0057 ganhar entradas para esses tipos — nenhum trabalho
  extra deste briefing, é consequência do design aberto por tipo do 0057.
- **[0056] Job de saúde tenta enriquecer via provider itens draft** — não
  depende deste briefing; drafts continuam fora da avaliação de completude
  até serem casados.
