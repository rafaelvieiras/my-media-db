# 0050 — Auto-match: busca e sincronização automática da fila de revisão

> Um botão "Auto-sincronizar" no item de baixa confiança/draft (e uma versão em
> lote na fila inteira) que busca local-first + provedor remoto, pontua os
> candidatos e aplica o melhor **sozinho**, quando a pontuação é alta o
> suficiente — sem o dono precisar abrir e buscar item por item.

| | |
|---|---|
| **ID** | 0050 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | done |
| **Docs relacionadas** | [06-biblioteca-e-scan](../docs/06-biblioteca-e-scan.md) (pontuação de confiança e fila de revisão), [ADR-0008](../docs/adr/0008-identidade-e-matching-kodi-jellyfin.md), [ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md) |
| **Depende de** | [0009](0009-scanner-fswalk-ffprobe-matching.md), [0014](0014-provider-tmdb-busca-local-first.md), [0015](0015-enriquecimento-import-proxy-imagem.md), [0018](0018-fila-revisao-orfas-correcao-match.md), [0026](0026-centralizacao-interno-first.md) |

---

## 1. Objetivo

O [0018](0018-fila-revisao-orfas-correcao-match.md) fechou o loop de curadoria,
mas **corrigir ainda é manual**: o dono abre cada item de baixa confiança/draft,
busca o título e escolhe o resultado certo, um de cada vez. Isso é aceitável
para volume baixo, mas vira o gargalo exato de um cenário comum: **popular a
biblioteca de uma vez** (ex.: apontar uma fonte nova para uma pasta cheia de
filmes já existentes, como ao migrar a biblioteca para uma montagem de Google
Drive) — dezenas de itens caem na fila no mesmo scan.

Este briefing propõe uma ação de **auto-match**: dado um item incerto, o
sistema tenta achar sozinho o candidato certo — primeiro no **catálogo local**
(evita duplicar entity já existente, reforça [0026](0026-centralizacao-interno-first.md)),
depois no **provedor remoto** configurado — e só aplica quando a pontuação do
melhor candidato é alta o suficiente para ser confiável. Sem candidato forte,
o item **fica intacto na fila** para o dono resolver manualmente, exatamente
como hoje.

## 2. Jornada do usuário

1. O dono cadastra uma fonte nova e roda o scan; 30 filmes entram na fila de
   **Revisão** (baixa confiança ou draft não-identificado).
2. Em vez de abrir um por um, o dono clica em **"Auto-sincronizar"** — tanto no
   card de um item individual quanto num botão de lote no topo da fila
   ("Auto-sincronizar elegíveis").
3. Para cada item: o sistema deriva título/ano do nome do arquivo (reusa o
   parser já usado pelo scan), busca local-first ([0014](0014-provider-tmdb-busca-local-first.md))
   e pontua os candidatos (local e externo).
   - Achou um candidato **local** com pontuação alta → **liga** o arquivo a essa
     entity existente (não cria duplicata).
   - Sem candidato local forte, mas achou um **externo** com pontuação alta →
     aplica como o "Corrigir" do [0018](0018-fila-revisao-orfas-correcao-match.md)
     (grava `external_ids`, re-enriquece).
   - Nenhum candidato bate o limiar → item **permanece na fila**, nada muda.
4. Ao final (individual ou lote), o dono vê um resumo: *"22 resolvidos
   automaticamente, 8 continuam para revisão manual"*. Os 8 restantes seguem o
   fluxo manual de sempre.

## 3. Escopo

**Dentro:**
- Ação individual (`POST` num item da fila) e ação em lote (todos os elegíveis
  da fila de Revisão de uma vez).
- Busca **local-first**: prioriza ligar a uma entity já existente no catálogo
  antes de considerar importar do provedor remoto.
- Pontuação textual do candidato (similaridade de título + ano) para decidir
  se aplica automaticamente — limiar **conservador**, deliberadamente mais
  alto que o `LIMIAR_BAIXA_CONFIANCA` do scan (aplicar errado é pior que deixar
  manual).
- Cobre filmes e séries (nível obra/série-topo) na mesma fila do 0018.
- Resumo do resultado (quantos resolvidos vs. quantos ficaram pendentes).

**Fora (explicitamente):**
- Mudar o algoritmo/limiar de matching do **scanner** ([0009](0009-scanner-fswalk-ffprobe-matching.md))
  — este briefing atua **depois** do scan, na fila de revisão.
- Tuning fino do limiar de confiança geral (é decisão de tuning, não de modelo
  — ver [06](../docs/06-biblioteca-e-scan.md), Decisões abertas).
- Merge/split de entidades duplicadas já existentes no catálogo (citado como
  follow-up em aberto no [0018 §7](0018-fila-revisao-orfas-correcao-match.md#7-fora-deste-briefing--follow-ups)).
- Auto-match dentro da árvore de série (temporada/episódio individual) — cobre
  só o nó de topo por ora.
- Verticais fora de movie/tv (música etc.).

## 4. Contrato técnico

### API
- `POST /api/v1/entities/{id}/auto-match` — tenta resolver um item da fila.
  Sempre `200`, nunca `5xx` por "não achou candidato" (é um resultado válido).
  ```json
  {"resolved": true, "method": "local", "score": 96.4,
   "target": {"id": "...", "title": "...", "year": 2020}}
  ```
  Quando não resolve: `{"resolved": false, "method": "none", "score": 0, "target": null}`.
- `POST /api/v1/review/auto-match` — lote. Corpo opcional `{"ids": ["..."]}`
  (omitido/vazio = todos os elegíveis, via a query `ListarRevisao` já existente
  em `internal/db/queries/review.sql`, mesma usada para popular `/review`).
  ```json
  {"resolved_count": 22, "pending_count": 8,
   "items": [{"id": "...", "resolved": true, "method": "local", "score": 95.1}, ...]}
  ```
- Reusa o envelope de erro padrão do resto da API.

### Backend (Go) — reaproveitamento confirmado por leitura do código

- **"Religar a uma entity local" já tem caminho pronto — não é mais um ponto
  em aberto.** `fundirEntities(ctx, sobreviventeID, perdedoraID string) error`
  em `internal/server/duplicata.go:118` (criado no fix residual do 0051) já
  faz exatamente isso: move `media_file`, `media_file_entity`,
  `media_external`, `asset`, `credit`/`aggregate_credit` e `entity_relation`
  da perdedora para a sobrevivente (pulando colisões) e apaga a perdedora via
  cascade. Hoje só é chamado a partir de colisão de `external_id`; o 0050
  adiciona um segundo gatilho (match textual) que chama a mesma função direto:
  `fundirEntities(ctx, candidatoLocal.ID, entityDaFila.ID)` — sobrevivente é o
  item já catalogado, perdedora é o item da fila (draft/baixa confiança).
- **Aplicar externo** reusa o corpo interno do `correctEntity`
  (`internal/server/review.go:122`, handler do `Corrigir` do 0018): mesma
  sequência `resolverColisaoExterna` → `s.enrich.Enriquecer(...)` →
  `finalizarRevisao`, chamada diretamente (não via HTTP) com o `source`/`source_id`
  do melhor candidato externo.
- **Deriva título/ano** via `internal/naming.Parse(name string) Info` (já
  exportada, `naming.go:86`) a partir do nome do arquivo ligado ao item da
  fila — não confia no `entity.title` bruto do draft.
- **Busca local**: shortlist via `buscarLocal`/`buscarCardsFTSSQL`
  (`internal/server/search.go`), já restrita por `content_type`; a ordem do
  BM25 do FTS5 vira só um pré-filtro (top ~10), a pontuação final é sempre
  recalculada com a similaridade abaixo (BM25 não é comparável a um limiar
  fixo).
- **Busca externa**: `provider.Provider.Buscar(ctx, query, tipo, lang)` no
  provider default por capacidade (mesma resolução de fonte que o
  `correctEntity` usa quando `source` não é informado).
- **Pontuação (novo pacote `internal/textsim`, sem dependência externa)**:
  Jaro-Winkler puro Go sobre título normalizado (minúsculo, sem acento/pontuação,
  espaços colapsados) → `Similaridade(a, b string) float64` em `[0,1]`.
  Score final em `[0,100]`:
  - `score := Similaridade(tituloConsulta, tituloCandidato) * 100`
  - se os dois anos existem e diferem em **mais de 1** → candidato
    **descartado** (não entra no ranking; remake/refilmagem não deve colar).
  - se os anos batem exatamente → `score += 5` (cap em 100).
  - diferença de exatamente 1 ano → sem ajuste (tolerância a erro comum de
    catalogação).
  - `LimiarAutoMatch = 92.0` — constante em `internal/server`, deliberadamente
    bem acima do `limiarBaixaConfianca = 70` (`review.go:29`) do 0018. Mesmo
    limiar para local e externo.
- **Prioridade**: sempre tenta local primeiro; só busca externo se nenhum
  candidato local bateu o limiar.

### Frontend
- Botão "Auto-sincronizar" no card de `/review` (ação individual) + botão de
  lote no topo da lista. Loading por item; toast/resumo ao final
  ("X resolvidos automaticamente, Y continuam para revisão"). Invalida
  `['review']`/`['entities']`/`['entity', id]` como as demais ações do 0018.

### Arquivos afetados
- Novo: `internal/textsim/textsim.go` (+ `textsim_test.go`) — Jaro-Winkler.
- Novo: `internal/server/automatch.go` (+ `automatch_test.go`) — orquestra
  local-first → scoring → aplica (`fundirEntities` ou corpo do `correctEntity`).
- `internal/server/server.go` — registro das rotas `POST /entities/{id}/auto-match`
  e `POST /review/auto-match`.
- `openapi.yaml` — schemas `AutoMatchResult`, `AutoMatchBatchRequest`,
  `AutoMatchBatchResult` + os dois paths.
- `web/src/lib/api.ts`/`queries.ts` — funções + tipos gerados.
- `web/src/routes/review.tsx` (ou componente de card equivalente do 0018) —
  botão individual + botão de lote + resumo.
- Sem migration nova (nenhuma tabela nova é necessária).

## 5. Critérios de aceite

- [ ] Ação individual resolve um item de baixa confiança/draft quando existe
  candidato (local ou externo) com score ≥ 92 (`LimiarAutoMatch`), sem
  intervenção manual do dono.
- [ ] Quando o melhor candidato é do **catálogo local**, o item é ligado à
  entity existente via `fundirEntities` — **nunca** cria uma entity duplicada.
- [ ] Candidato com ano divergindo em mais de 1 ano nunca é aplicado, mesmo
  com título idêntico.
- [ ] Quando nenhum candidato bate o limiar em nenhuma fonte, o item permanece
  intacto na fila (nada é aplicado "no palpite") e o endpoint individual
  responde `200 {resolved: false, method: "none"}` (nunca erro).
- [ ] Ação em lote processa todos os elegíveis (via `ListarRevisao`) ou o
  subconjunto de `ids` informado, e devolve o resumo agregado
  (`resolved_count`/`pending_count`/`items`).
- [ ] Não altera o algoritmo de matching do scanner nem o `LIMIAR_BAIXA_CONFIANCA`
  existente do 0018.

## 6. Notas de implementação / decisões

- **Motivação real, não hipotética**: nasceu do fluxo de popular a biblioteca a
  partir de uma montagem de rede (Google Drive via rclone) recém-conectada ao
  Docker de produção — dezenas de filmes de uma vez, revisão item-a-item vira
  o gargalo perceptível.
- **Local-first é deliberado**: evita que o auto-match crie entities duplicadas
  para obras que já existem no catálogo (mesma motivação do [0026](0026-centralizacao-interno-first.md)).
- **Limiar de auto-aplicação deve ser mais conservador que o de revisão**:
  o custo de deixar um item manual é baixo (ele já está visível na fila); o
  custo de auto-aplicar errado é alto (contamina o catálogo silenciosamente).
  Prefira falso-negativo (deixar manual) a falso-positivo.
- **"Religar a uma entity local existente" já tem caminho pronto**: reusa
  `fundirEntities` (`internal/server/duplicata.go:118`), criada no fix
  residual do 0051 para fundir duplicatas por colisão de `external_id`. O
  0050 só adiciona um gatilho novo (match textual via `internal/textsim`) que
  chama a mesma função — compartilha o caminho com o merge de duplicatas,
  como este ponto já previa.

## 7. Fora deste briefing / follow-ups

- Merge/split de entidades duplicadas (já apontado como aberto no 0018).
- Auto-match dentro da árvore de série (temporada/episódio).
- Tuning do limiar de confiança/auto-match com base em uso real.
