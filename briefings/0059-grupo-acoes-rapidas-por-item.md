# 0059 — Grupo de ações rápidas por item (detalhe)

> O botão "Editar" solto no topo do detalhe vira um GRUPO de ações: Editar +
> "Baixar informações faltando" (dispara o 0056 sob demanda, só para este item)
> + "Baixar arte faltando" (usa o 0058 para saber quais kinds faltam e baixa via
> os providers de arte do 0048/0049).

| | |
|---|---|
| **ID** | 0059 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Docs relacionadas** | [07-enriquecimento-providers](../docs/07-enriquecimento-providers.md), [08-artwork-e-assets](../docs/08-artwork-e-assets.md), [05-api](../docs/05-api.md) |
| **Status** | done |
| **Depende de** | [0056](0056-enriquecimento-externo-retroativo-job-saude.md), [0058](0058-indicador-completude-catalogacao-por-item.md) |

---

## 1. Objetivo

O [0056](0056-enriquecimento-externo-retroativo-job-saude.md) ensinou o sistema
a buscar e aplicar (ou sinalizar) enriquecimento externo para itens
`cataloged`-nunca-verificados — mas só em **lote**, uma vez por dia, com um teto
de 15 itens por execução. O dono que abre o detalhe de UM filme específico e
percebe que falta pôster/sinopse não tem hoje nenhuma forma de pedir "tenta
agora, só este" — só pode esperar o próximo lote do job, ou usar `/review`
manualmente (que só serve os itens que JÁ estão na fila, não este, que nem
chegou lá).

O [0058](0058-indicador-completude-catalogacao-por-item.md) fechou o outro lado
do problema: agora o dono SABE, item a item, o que falta (`catalog_completeness.
missing`, ex. `["cast", "artwork:fanart"]`) — mas o indicador é **passivo**,
só sinaliza. Ele já reserva explicitamente essa ação para este briefing (0058
§3 "Fora", §7).

Este briefing fecha o ciclo interativo: o botão solto "Editar" no topo do
detalhe (`web/src/routes/entity-detail.tsx`, hoje ~linha 282-288) vira um
**grupo de ações rápidas**, com dois botões novos que disparam, **sob demanda e
só para o item aberto**, exatamente a mesma orquestração de busca/aplicação já
desenhada e testada em outros briefings — sem duplicar lógica de matching nem
de download de arte, e sem inventar uma segunda UI de "escolher candidato"
onde já existe a fila de Revisão.

## 2. Jornada do usuário

1. O dono abre o detalhe de `"Duna (2021).mkv"` — importado pelo scan, título
   e ano corretos, mas sem pôster nem sinopse (o caso do 0056: `cataloged`,
   confiança 70, nunca verificado contra provider nenhum). O selo de
   catalogação incompleta do 0058 já aparece na seção "Catalogação
   incompleta": `description, cast, artwork:poster, artwork:fanart`.
2. No topo, ao lado de "Editar", aparecem agora dois botões: **"Baixar
   informações faltando"** e **"Baixar arte faltando"**.
3. Ele clica em "Baixar informações faltando". O botão entra em estado de
   carregamento ("Buscando…"). O backend roda a MESMA busca local→externo do
   0056/0050 só para este id.
   - **Achou candidato forte** (score ≥ `LimiarAutoMatch`): aplica sozinho — a
     tela recarrega com pôster, sinopse, elenco. O botão mostra brevemente
     "Aplicado" e desaparece (o item já não tem mais o que baixar por esse
     caminho — mas pode continuar faltando arte, ver `catalog_completeness`
     recalculado).
   - **Achou algo plausível, não confiável o bastante**: o item é sinalizado
     (mesmo mecanismo do 0056 — `match_confidence` rebaixado para entrar na
     fila). O botão mostra uma mensagem inline: *"Não encontramos uma
     correspondência confiável — o item foi enviado para a fila de Revisão."*
     com um link para `/review`. Ele já conhece essa tela — não abre nenhum
     modal novo de "qual desses candidatos é o certo".
   - **Nada encontrado** (provider fora do ar, sem resultado): mensagem
     inline neutra, "Nenhuma correspondência encontrada desta vez." — não é
     um erro vermelho, é um resultado válido (mesmo espírito do 0050/0056:
     "não achou" nunca é falha).
4. Ele clica em "Baixar arte faltando". O botão entra em "Baixando…". O
   backend olha `catalog_completeness.missing`, filtra as chaves
   `artwork:<kind>`, e para cada kind consulta o inventário de arte (fanart.tv
   + camadas já cacheadas, mesmo endpoint do 0048) e baixa a melhor opção
   disponível (reusa a mesma lógica de `selectArtwork`/`baixarEMaterializarArte`
   do 0048, sem passar pelo lightbox do 0049).
   - **Achou e baixou** algo para pelo menos um kind faltando: a galeria e a
     seção de completude atualizam; mensagem "Pôster e fanart baixados."
   - **Nenhum kind faltando tinha opção disponível** nos providers (ex.: filme
     obscuro sem clearart em nenhuma fonte): mensagem inline neutra, "Nenhuma
     arte disponível nos provedores para o que falta." — de novo, não é erro.
   - Se o item **não tem `catalog_completeness`** (é `draft`, ou nunca foi
     avaliado pelo job — 0058) ou não tem NENHUM `artwork:<kind>` faltando, o
     botão nem aparece (nada a fazer).
5. Se o item já está completo (`catalog_completeness.complete === true` ou
   `null`), nenhum dos dois botões novos aparece — só "Editar", exatamente
   como hoje. Sem ruído visual para o caso comum de item já catalogado.

## 3. Escopo

**Dentro:**
- Novo componente de grupo de botões no topo do detalhe (`entity-detail.tsx`,
  slot `titleBadges` da `RichDetailView`), substituindo o `<Button>` solto do
  "Editar" por um `<div>`/`<span>` de grupo com os três botões lado a lado
  (Editar sempre visível; os dois novos condicionais).
- **Endpoint novo `POST /api/v1/entities/{id}/enrich-missing`**: chama a MESMA
  orquestração extraída no 0056 (`tentarEnriquecerOuSinalizar`), disparada
  para UM id, fora do lote do job. Reusa 100% da lógica de busca/pontuação/
  aplicação/sinalização — nenhuma duplicação.
- **Endpoint novo `POST /api/v1/entities/{id}/download-missing-artwork`**: lê
  `catalog_completeness.missing` do próprio item (recalculado ao vivo, mesmo
  caminho do 0058 no detalhe), filtra as chaves `artwork:<kind>`, e para cada
  kind chama o inventário (`getEntityArtwork`/coleta já existente do 0048) +
  a mesma função de download/materialização do `selectArtwork` (0048) — sem
  passar pela escolha manual do dono, escolhe a melhor opção disponível por
  kind (ver §4/§6 para o critério de "melhor").
- Estados de loading/sucesso/mensagem inline nos dois botões novos, no mesmo
  padrão visual de `ExcluirColecao`/`LanguageSettingsForm` (mutation.isPending,
  mensagem de erro/status inline via `role="status"`/`role="alert"`, sem
  toast — o projeto não usa biblioteca de toast).
- Invalidação de queries (`entity`, `entities`, `artwork`) no sucesso de
  qualquer um dos dois botões, para a tela refletir o resultado sem F5.
- Testes: handlers novos (backend) cobrindo os três desfechos de cada botão
  (aplicado/sinalizado/nada — informações; baixou/nada disponível — arte) +
  404 entity inexistente; frontend (vitest) cobrindo a renderização
  condicional dos botões e os três estados visuais.

**Fora (explicitamente):**
- **UI de "escolher qual candidato mais se parece"** além de mandar para a
  fila de Revisão já existente. **Decisão explícita**: a fila de Revisão
  (0018) JÁ É o mecanismo de "o sistema achou algo incerto, o dono decide" —
  criar um segundo fluxo de escolha (modal com candidatos lado a lado, por
  exemplo) duplicaria uma decisão de UX já tomada e um caminho de código já
  existente (`/review` + `POST /entities/{id}/correct`). "Baixar informações
  faltando" **nunca abre um modal de escolha** — ou aplica sozinho, ou manda
  para `/review`, ponto.
- **Trailer/tema musical** — fora do 0057/0058 também; não avaliado como
  "faltando" e não baixado por nenhum botão deste grupo.
- **Lote/múltiplos itens de uma vez** — os dois botões novos operam SEMPRE
  sobre o item aberto no detalhe. Nenhuma ação em massa na listagem/grid.
  Lote continua sendo só o job (0056) e o `/review` (0050/0054).
- **Escolher manualmente QUAL arte baixar por kind** — isso já existe (galeria
  + lightbox do 0049, `POST /artwork/select`). "Baixar arte faltando" é
  automático (melhor opção por kind faltando); o dono que quiser escolher
  outra vai na galeria como já faz hoje. Os dois caminhos coexistem sem
  conflito (o botão não impede o dono de trocar a preferida depois).
- **Alterar a config de completude (0057)** ou o cálculo do selo (0058) —
  este briefing só CONSOME os dois.
- **Novo estado de UI persistente por item** (ex. "já tentei baixar hoje") —
  o botão simplesmente reflete o resultado da última chamada; não há
  histórico nem contador na UI (o histórico de execução do job, se aplicável,
  já existe em `/jobs`; a ação manual não escreve lá).

## 4. Contrato técnico

### Dados / modelo

Nenhuma migration nova. Este briefing só orquestra chamadas contra o que já
existe: `entity.last_external_match_attempt_at` (0056), `media_file.
match_confidence` (0018/0050), `entity_catalog_completeness`/cálculo ao vivo
(0058), `asset`/`media_external` (0048).

Único ajuste de dado: quando `POST .../enrich-missing` roda a orquestração do
0056 para este item, ela DEVE gravar `last_external_match_attempt_at` como
qualquer outra tentativa real (reusa o mesmo helper `AtualizarTentativaMatchExterno`
do 0056) — é a consistência que o 0056 §6 já antecipou como esperada: o job do
dia seguinte não deve competir pelo mesmo item que o dono acabou de resolver
manualmente.

### API

**`POST /api/v1/entities/{id}/enrich-missing`** — dispara sob demanda a
orquestração do 0056 (`tentarEnriquecerOuSinalizar`) para UM item, fora do
lote/throttle do job.

- 404 se a entity não existe.
- Sem guarda de elegibilidade por status/confiança: ao contrário do job
  (que só varre o complemento de `ListarRevisao`), o botão manual pode ser
  chamado em QUALQUER item não-draft — inclusive um já com `external_ids`
  preenchido (o dono pode querer forçar uma nova tentativa depois de trocar
  algo). Ver §6 para a justificativa de não replicar o filtro do job aqui.
- Ignora o throttle de 30 dias (`intervaloReTentativaRetroativa`) — é uma
  ação explícita do dono, não o job em lote; mas AINDA carimba
  `last_external_match_attempt_at` ao final (consistência com o job, 0056 §6).
- 200 sempre (nunca 5xx por "não achou candidato" — mesmo padrão do
  `/auto-match` do 0050), corpo:
  ```json
  { "outcome": "applied", "method": "local", "score": 96.0 }
  { "outcome": "flagged", "score": 61.0 }
  { "outcome": "none" }
  ```
  `outcome` espelha o `resultado` de `tentarEnriquecerOuSinalizar`
  (`"applied"|"flagged"|"none"`, 0056 §4) — reaproveitado como o DTO público
  deste endpoint, sem reinventar um vocabulário novo. `method`/`score`
  presentes só quando `outcome=="applied"`.
- 500 só em erro real de banco/infra (mesma semântica do 0056/0050).

**`POST /api/v1/entities/{id}/download-missing-artwork`** — baixa a melhor
opção disponível para cada `artwork:<kind>` que aparece em
`catalog_completeness.missing` do item.

- 404 se a entity não existe.
- Passo 1: recalcula `catalog_completeness` AO VIVO (reusa
  `s.anotarCompletudeCatalogacao`/`avaliarCompletude` do 0058 — mesmo caminho
  do detalhe, nunca o cache da listagem, que pode estar desatualizado). Se
  `complete==true` ou o item é draft (`catalog_completeness` nil), devolve
  200 com `{"downloaded": [], "unavailable": []}` sem tocar nada — nada a
  fazer, não é erro.
- Passo 2: filtra `missing` pelas chaves `artwork:<kind>`, extraindo os kinds
  (`strings.TrimPrefix(chave, "artwork:")`).
- Passo 3: para cada kind faltando, chama o MESMO agregador de inventário do
  0048 (`getEntityArtwork`'s helpers internos, refatorados para uma função
  não-HTTP `s.arteDisponivelPorKind(ctx, id, kind) ([]artworkItem, bool)` —
  extração mínima do corpo já escrito em `artwork.go`, para não duplicar a
  lógica de mesclar assets locais + camadas + inventário fanart.tv) e escolhe
  a "melhor opção" por um critério simples e determinístico (ver §6): maior
  `Likes` (fanart.tv) e, em empate/ausência de likes, a primeira da lista
  (mesma ordem estável do agregador). Chama `s.baixarEMaterializarArte` (0048,
  já existe, sem duplicar) com a `SrcURL` escolhida.
- 200 sempre, corpo:
  ```json
  {
    "downloaded": ["poster", "fanart"],
    "unavailable": ["clearart"]
  }
  ```
  `downloaded` = kinds que ganharam um asset novo; `unavailable` = kinds que
  estavam faltando mas não tinham NENHUMA opção em nenhum provider agora.
  Um kind que já tinha asset (corrida entre completude recalculada e um
  upload manual concorrente) não aparece em nenhuma das duas listas —
  idempotente, sem erro.
- 502 nunca aqui: diferente do `/artwork/select` (que baixa uma URL ESCOLHIDA
  pelo dono e por isso trata falha de rede como erro do pedido), este
  endpoint escolhe entre VÁRIAS opções — se uma falhar ao baixar
  (`baixarEMaterializarArte` retorna erro), tenta a próxima candidata do
  mesmo kind antes de desistir; só cai em `unavailable` quando NENHUMA
  candidata do kind baixou (best-effort, mesmo espírito do 0053/0056). Erro
  real de banco (não de download) ainda propaga como 500.
- 503 se `s.assetsDir == ""` (sem diretório de assets configurado — mesma
  guarda de `uploadAsset`/`selectArtwork`).

`openapi.yaml`: dois `paths` novos, schemas `EnrichMissingResult` e
`DownloadMissingArtworkResult`, seguindo o padrão dos DTOs já existentes
(`autoMatchResultDTO`, resposta de `/artwork/select`). Regenerar tipos
(`gen:types` + `tsc -b`).

### Backend (Go)

- **`internal/server/retroativo.go`** (0056, já existente após aquele
  briefing): adicionar `enrichMissing(w http.ResponseWriter, r *http.Request)`
  — handler HTTP fino: `exigirEntity` → `s.tentarEnriquecerOuSinalizar(ctx,
  ent)` → mapeia `resultado` (`"applied"|"flagged"|"none"`) para o DTO
  `enrichMissingResultDTO{Outcome, Method, Score}` → sempre carimba
  `AtualizarTentativaMatchExterno` no final (mesma linha do passo em lote do
  0056, extraída para um pequeno helper `s.carimbarTentativaExterna(ctx, id)`
  reusado pelos dois caminhos — em vez de duplicar a chamada sqlc). Nenhuma
  guarda de elegibilidade (status/throttle) — só `exigirEntity` (404) e
  `s.enrich == nil` degrada para `outcome: "none"` (mesmo padrão do 0050 sem
  provider configurado).
- **`internal/server/artwork_missing.go`** (novo arquivo, mesmo pacote de
  `artwork.go`/`artwork_select.go` — depende de `s.coletarArtesLocais`,
  `s.coletarArtesDeCamadas`, `s.coletarInventarioFanart`, `s.baixarEMaterializarArte`,
  todos privados ao pacote `server`):
  - `func (s *Server) downloadMissingArtwork(w http.ResponseWriter, r *http.Request)`
    — handler: `exigirEntity` → `assetsIndisponivel` (503) → recalcula
    completude ao vivo (reusa o helper do 0058) → filtra `artwork:<kind>` →
    itera kinds → `s.baixarMelhorArteDoKind(ctx, id, kind) (baixou bool, err
    error)` → agrega `downloaded`/`unavailable`.
  - `func (s *Server) baixarMelhorArteDoKind(ctx context.Context, id, kind
    string) (bool, error)` — monta os grupos do agregador do 0048 (extraído
    de `getEntityArtwork` para reuso não-HTTP, ver nota abaixo), filtra pelo
    `kind`, ordena por `Likes` desc (estável — mesma ordem de chegada em
    empate) e tenta `baixarEMaterializarArte` em ordem até uma funcionar;
    `false, nil` (não `false, err`) quando NENHUMA candidata existe OU
    NENHUMA baixou — só erro de banco (não de download) sobe como `err`.
  - **Extração mínima de `artwork.go`**: `getEntityArtwork` (handler HTTP)
    passa a chamar um novo `func (s *Server) montarGruposArtwork(ctx
    context.Context, id string) map[string][]artworkItem` que concentra as
    três chamadas (`coletarArtesLocais`/`coletarArtesDeCamadas`/
    `coletarInventarioFanart`) — mesmo padrão de extração já usado no 0056
    para `aplicarCandidatoExterno`. `baixarMelhorArteDoKind` chama o MESMO
    `montarGruposArtwork`, sem duplicar a lógica de mesclagem de fontes.
- **`internal/server/server.go`**: duas rotas novas dentro de `v1.Route("/entities", ...)`,
  perto das ações já existentes (`auto-match`, `artwork/select`):
  ```go
  ent.Post("/{id}/enrich-missing", s.enrichMissing)
  ent.Post("/{id}/download-missing-artwork", s.downloadMissingArtwork)
  ```

### Frontend (Vite/React)

- **`web/src/components/entity-action-buttons.tsx`** (novo componente):
  `EntityActionButtons({ id, catalogCompleteness }: { id: string;
  catalogCompleteness: CatalogCompleteness }` — agrupa os três botões
  (Editar sempre; os dois novos condicionais a `precisaAtencaoCatalogacao`
  reaproveitado do 0058 para decidir se mostra "Baixar informações faltando"
  quando `!catalog_completeness?.complete`, e a presença de alguma chave
  `artwork:` em `missing` para "Baixar arte faltando" especificamente).
  Renderizado no lugar do `<Button>` solto em `entity-detail.tsx` (slot
  `titleBadges`).
  - Cada botão é sua própria sub-função/mutation dentro do componente (não
    dois componentes separados — compartilham o `id` e o padrão de
    invalidação), no mesmo padrão de mutation isolada usado em
    `ExcluirColecao`/`LanguageSettingsForm`: `useMutation` +
    `mutation.isPending` → rótulo "Buscando…"/"Baixando…"; `onSuccess` invalida
    `['entity', id]`, `['entities']`, `['artwork', id]`; mensagem de resultado
    lida do corpo da resposta, exibida inline **abaixo** do grupo de botões
    (não substitui o botão — ele volta ao rótulo normal, a mensagem fica até
    a próxima ação ou a navegação).
  - **"Baixar informações faltando"** (`Sparkles`/`Download` ícone,
    `lucide-react` — a decidir na implementação, qualquer ícone de
    "buscar"/"baixar" coerente com o resto do app serve): `POST
    /entities/{id}/enrich-missing`.
    - `outcome === "applied"` → mensagem "Aplicado — {method === 'local' ?
      'ligado a um item já catalogado' : 'enriquecido via provider'}."
      (`role="status"`, verde/neutro).
    - `outcome === "flagged"` → mensagem "Não encontramos uma correspondência
      confiável — o item foi enviado para a fila de Revisão." com um
      `<Link to="/review">`.
    - `outcome === "none"` → mensagem "Nenhuma correspondência encontrada
      desta vez." (neutro, não `role="alert"` — não é erro).
    - Erro de rede/500 → mensagem de erro (`role="alert"`, mesmo padrão de
      `ApiError` já usado em `ExcluirColecao`).
  - **"Baixar arte faltando"** (`Image`/`ImageDown` ícone): `POST
    /entities/{id}/download-missing-artwork`.
    - `downloaded.length > 0` → mensagem listando os kinds baixados (rótulo
      PT-BR via o mesmo mapa de `rotuloItemFaltando`/rótulo de kind do 0058,
      ex. "Pôster e fanart baixados.").
    - `downloaded.length === 0 && unavailable.length > 0` → "Nenhuma arte
      disponível nos provedores para o que falta." (neutro).
    - `downloaded.length === 0 && unavailable.length === 0` (nada faltava) →
      botão nem deveria ter aparecido; tratado como no-op silencioso se
      chamado mesmo assim (corrida com outra aba, por exemplo).
- **`web/src/lib/api.ts`**: duas funções novas, `enrichMissing(id)` e
  `downloadMissingArtwork(id)`, no mesmo padrão de `autoMatchEntity`/
  `selectArtwork` já existentes (POST, corpo vazio, tipos gerados).
- **`web/src/routes/entity-detail.tsx`**: troca o `<Button asChild ...>
  Editar</Button>` solto (linha ~282-288) por
  `<EntityActionButtons id={id} catalogCompleteness={data.
  catalog_completeness} />` dentro do mesmo slot `titleBadges` (mantém a
  posição — depois dos selos, `ml-auto` no grupo inteiro, não em cada botão
  individualmente, para os três ficarem juntos à direita).
- **`web/src/lib/api-types.ts`**: regenerado (`gen:types`) a partir do
  `openapi.yaml` — sem edição manual.

### Arquivos afetados

- Criar: `web/src/components/entity-action-buttons.tsx` (+ teste vitest),
  `internal/server/artwork_missing.go` (+ teste).
- Alterar: `internal/server/retroativo.go` (handler `enrichMissing` +
  `carimbarTentativaExterna` extraído), `internal/server/artwork.go`
  (extrai `montarGruposArtwork` de `getEntityArtwork`), `internal/server/
  server.go` (duas rotas novas), `internal/server/completeness.go` (expõe
  `avaliarCompletudeAoVivo`/reusa o helper do 0058 fora do `resolverEntity`
  se necessário para chamar sob demanda), `web/src/routes/entity-detail.tsx`
  (troca do botão solto pelo grupo), `web/src/lib/api.ts`, `openapi.yaml`
  (+ tipos gerados).

## 5. Critérios de aceite

- [ ] `POST /entities/{id}/enrich-missing` num item `cataloged` sem
  `external_ids` e com candidato forte (fake provider, score ≥ 92) aplica
  sozinho (`outcome: "applied"`) e o item ganha pôster/sinopse — mesma
  asserção do critério equivalente do 0056, só que disparado individualmente.
- [ ] O mesmo endpoint com candidato médio (score entre 50 e 92) sinaliza
  (`outcome: "flagged"`) e o item passa a aparecer em `GET /review`.
- [ ] O mesmo endpoint sem candidato nenhum devolve `outcome: "none"`, 200,
  sem alterar o item (além de carimbar `last_external_match_attempt_at`).
- [ ] `POST /entities/{id}/enrich-missing` funciona mesmo se
  `last_external_match_attempt_at` foi carimbado há menos de 30 dias (ignora
  o throttle do 0056 — é ação manual, não o lote do job).
- [ ] `POST /entities/{id}/download-missing-artwork` num item com
  `catalog_completeness.missing = ["artwork:poster", "artwork:fanart"]` e
  inventário fanart.tv fake disponível para ambos baixa os dois
  (`downloaded: ["poster","fanart"]`), materializando dois assets
  `origin=external` novos.
- [ ] O mesmo endpoint com um kind faltando SEM nenhuma opção nos providers
  devolve esse kind em `unavailable`, sem erro.
- [ ] O mesmo endpoint num item já `complete` (ou draft) devolve
  `{"downloaded": [], "unavailable": []}` sem tocar nada.
- [ ] Falha de download de UMA candidata (fake provider 500/timeout) tenta a
  próxima candidata do mesmo kind antes de desistir; erro real de banco ainda
  propaga como 500.
- [ ] No frontend: os dois botões novos só renderizam quando
  `catalog_completeness` indica algo faltando (informações faltando: qualquer
  chave não-`artwork:`; arte faltando: pelo menos uma chave `artwork:`); item
  completo/draft mostra só "Editar".
- [ ] Clicar em cada botão mostra o estado de loading, depois a mensagem
  correta para cada um dos desfechos (`applied`/`flagged`/`none` para
  informações; baixou/nada disponível para arte) — coberto por teste vitest
  com mocks dos três/dois desfechos.
- [ ] Nenhum modal novo de "escolher candidato" é introduzido — grep no diff
  não deve trazer nenhum componente de seleção de match externo além do que
  já existe em `/review`.
- [ ] `go build/vet/test -count=1` e `tsc -b`/vitest do web verdes; catraca
  local não piora.

## 6. Notas de implementação / decisões

- **Por que `/enrich-missing` NÃO replica o filtro de elegibilidade do job
  (0056 `ListarElegiveisEnriquecimentoRetroativo`)**: o filtro do job existe
  para NÃO desperdiçar chamadas de provider varrendo itens que não precisam
  (já têm `external_ids`, já estão na fila, etc.) — é uma otimização de
  **lote**. O botão manual é uma ação explícita e pontual do dono clicando
  num item específico que ele está OLHANDO agora; replicar o filtro só
  bloquearia casos legítimos (ex.: um item já tem `external_ids` mas o dono
  quer forçar uma nova tentativa de match depois de uma correção manual em
  outro lugar). `tentarEnriquecerOuSinalizar` já é seguro de chamar em
  qualquer entity não-purgada — sem filtro extra necessário aqui.
- **Throttle ignorado, mas ainda carimbado**: o dono clicando o botão É a
  "tentativa" — não faz sentido bloqueá-lo com o mesmo throttle que existe
  para NÃO incomodar provider/rate-limit num job automático querendo rodar
  todo dia. Mas ainda carimba `last_external_match_attempt_at` ao final,
  porque o PRÓXIMO lote do job (amanhã) não deveria re-tentar o mesmo item
  que acabou de ser resolvido manualmente — essa coordenação já estava
  antecipada no 0056 §6 como um follow-up esperado deste briefing.
- **"Melhor opção" de arte = maior `likes` (fanart.tv), sem escolha do dono**:
  diferente do fluxo manual da galeria (0049, onde o dono ESCOLHE a arte no
  lightbox), este botão é "resolve sozinho" — precisa de um critério
  determinístico. `likes` é o único sinal de qualidade que a fanart.tv já
  fornece (`ArteInventario.Likes`, 0048); fontes sem likes (TMDB
  poster/backdrop, um único candidato) usam a ordem de chegada do agregador
  (estável, já determinística). Não é um ranking sofisticado — é "suficiente
  para uma ação automática", o dono pode sempre trocar depois na galeria.
- **Reuso deliberado, sem NENHUM caminho de aplicação/download novo**: os
  dois endpoints deste briefing são orquestradores finos sobre lógica já
  escrita e testada (0048/0049/0050/0056/0058). O único código genuinamente
  novo é: (a) o loop "para cada kind faltando, tenta a melhor candidata" e
  (b) a extração de `montarGruposArtwork`/`carimbarTentativaExterna` para
  reuso não-HTTP — ambas mecânicas, não uma nova política de matching/arte.
- **Por que não um único botão "Baixar tudo que falta"**: a decisão de
  usuário (pedido original) já separa os dois em botões distintos — são ações
  de naturezas diferentes (match de identidade vs. download de arte), com
  desfechos diferentes (uma pode mandar para revisão; a outra nunca precisa
  de decisão humana, só "achou ou não achou"), e o dono pode querer só uma
  das duas (ex.: já confirmou a identidade manualmente, só falta arte).
  Manter separados também mantém cada endpoint pequeno e de responsabilidade
  única, no espírito do resto do backend.
- **Sem UI de escolha de candidato — reforço da decisão de escopo (§3)**: a
  tentação natural seria, no caso `flagged`, abrir um modal mostrando os
  candidatos encontrados para o dono escolher ali mesmo, sem precisar ir para
  `/review`. Descartado deliberadamente: a fila de Revisão já resolve esse
  problema (mostra o item, o dono aciona "Corrigir" com o id certo, ou usa o
  auto-match da própria fila) — duplicar essa decisão de UX aqui criaria dois
  lugares fazendo a mesma coisa de formas diferentes, um dos quais
  (`/review`) já está mais maduro (histórico, filtro, ordenação por
  confiança). O link direto para `/review` na mensagem de "sinalizado" é o
  suficiente.
- **Sem toast**: o projeto não usa biblioteca de toast (`grep` em
  `web/src/components`/`web/src/routes` não traz `sonner`/`toast`); mensagens
  de resultado seguem o padrão já estabelecido de span inline com
  `role="status"`/`role="alert"` abaixo da ação (`ExcluirColecao`,
  `LanguageSettingsForm`).
- **`baixarMelhorArteDoKind` tenta múltiplas candidatas em sequência, não só
  a primeira**: sem isso, um único link quebrado no inventário faria o kind
  inteiro cair em `unavailable` mesmo havendo outras opções válidas — pouco
  provável mas barato de tratar (best-effort já é o padrão do 0053/0056/0048
  para chamadas de rede a provider externo).

## 7. Fora deste briefing / follow-ups

- **Ação em massa** (vários itens da listagem/grid de uma vez, ex. selecionar
  N cards e clicar "baixar o que falta em todos") — mantém o job (0056) como
  único caminho de lote; um botão de lote manual é um follow-up de UX
  possível, não pedido agora.
- **Trailer/tema musical** — segue fora até um briefing dedicado (0048 §7).
- **Histórico da ação manual** (ex. "última tentativa manual: há 2 minutos"
  visível na UI) — a única persistência é `last_external_match_attempt_at`
  (já usado só para throttle do job, não exposto na UI); expor isso ao dono
  é um refinamento visual possível, não pedido.
- **Escolher entre múltiplos providers de arte** quando mais de um está
  configurado (hoje só fanart.tv é `ArteProvider`) — quando existir um
  segundo, `baixarMelhorArteDoKind` precisará de uma política explícita de
  desempate entre fontes; fora de escopo até existir essa segunda fonte.
