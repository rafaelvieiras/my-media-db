# 0060 — Prévia de candidato na fila de Revisão

> Antes de qualquer ação na fila de Revisão, o card mostra QUAL seria o
> candidato encontrado (pôster/título/ano/score/fonte) — e o botão principal
> passa a confirmar ESSE candidato, não mais um palpite invisível.

| | |
|---|---|
| **ID** | 0060 |
| **Tipo** | fix — é uma correção de UX sobre uma feature já em produção (0018/0050): o comportamento existente ("Confirmar") não está quebrado tecnicamente, mas engana o dono ao sugerir que uma decisão informada foi tomada quando na verdade nenhum dado externo foi sequer consultado. Tratado como `fix` porque a motivação é reparar uma caixa-preta enganosa, não adicionar capacidade nova ao sistema — toda a lógica de busca/pontuação/aplicação já existe (0050); este briefing só a torna VISÍVEL antes da ação. |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Docs relacionadas** | [05-api](../docs/05-api.md), [06-biblioteca-e-scan](../docs/06-biblioteca-e-scan.md) |
| **Status** | done |
| **Depende de** | [0018](0018-fila-revisao-orfas-correcao-match.md), [0050](0050-auto-match-fila-de-revisao.md) |

---

## 1. Objetivo

O dono testou a tela de Revisão (`/review`, 0018) e relatou o bug real: ao
clicar em **"Confirmar"**, a tela "mostra o item que já existia" mas "não
mostra qual a relação externa que ele achou para que eu possa saber se devo
confirmar ou não". Lendo o código, a causa é confirmada — e é pior do que
parece à primeira vista:

- **"Confirmar"** (`confirmEntity`/`finalizarRevisao`,
  `internal/server/review.go:99-114`) nunca busca nem aplica nada externo.
  Só sobe `match_confidence` para 100 e promove o item de `draft` — ou seja,
  aceita cegamente o palpite que o **scanner** já tinha feito por parsing de
  nome de arquivo, sem o dono ver nem confirmar nenhum dado real.
- **"Auto-sincronizar"** (`autoSincronizar`/`autoMatchEntity`,
  0050) de fato busca (local-first, depois externo) e aplica — mas só
  **depois** de clicar, e só como uma frase de resultado. Sem candidato forte
  o bastante (score < `LimiarAutoMatch` = 92), a mensagem é só "nenhum
  candidato confiável encontrado" — o dono não sabe se não achou NADA ou se
  achou algo com 85% de confiança que ele mesmo aprovaria de bom grado.

Ou seja: **nenhum lugar da tela mostra, antes de uma ação, qual seria o
candidato** (pôster/título/ano/score/fonte). "Confirmar" e "Auto-sincronizar"
são duas ações sobrepostas e opacas, e é exatamente essa sobreposição —
não só a falta de prévia — que gera a confusão relatada.

Este briefing resolve os dois problemas juntos: (1) adiciona uma prévia
read-only que aparece ANTES de qualquer clique, reaproveitando a busca e a
pontuação que o 0050 já construiu; (2) redefine o botão principal do card
para agir sobre o candidato que a prévia mostrou, eliminando a ambiguidade
entre "confirmar o palpite do scanner" e "aplicar o candidato achado" — hoje
o dono não sabe qual das duas coisas "Confirmar" faz, e a resposta certa é
"nenhuma das duas, sozinho".

## 2. Jornada do usuário

1. O dono abre `/review`. Cada card, ao montar, busca sua prévia de candidato
   (local-first, depois externo — mesma ordem/pontuação do 0050) em segundo
   plano.
2. Enquanto a prévia carrega, o card mostra um estado de carregamento discreto
   sobre a área do candidato (pôster/título do item continuam visíveis
   normalmente — só o BLOCO DE PRÉVIA, abaixo, está carregando).
3. A prévia resolve em um destes três estados:
   - **Achou candidato**: mostra pôster (ou placeholder se a fonte não trouxe
     imagem), título, ano, score numérico e um selo da fonte ("catálogo
     local" ou "provedor externo"). Se o score bate o limiar automático
     (≥ 92), um selo extra indica "match forte"; abaixo disso, um selo neutro
     indica "confira antes de confirmar".
   - **Nenhum candidato encontrado**: mensagem explícita — "nenhum candidato
     encontrado — nem no catálogo local, nem na fonte externa".
   - **Falha ao buscar** (fonte externa fora do ar, erro de rede): mensagem
     curta de erro na prévia; o resto do card continua funcional (ver §6).
4. Com candidato encontrado, o botão principal do card é **"Confirmar
   candidato"**: aplica exatamente o que a prévia mostrou — liga à entity
   local (se veio do catálogo) ou grava a identidade externa e re-enriquece
   (se veio do provedor) — **mesmo que o score esteja abaixo de 92**. É uma
   escolha consciente do dono, informada pela prévia, diferente do
   auto-match automático (que só age sozinho, sem supervisão, acima do
   limiar).
5. Sem candidato algum, o botão principal vira **"Manter como está (sem
   metadados externos)"** — o mesmo comportamento de hoje (sobe a confiança,
   aceita o palpite do scanner), mas com um rótulo que não finge ter
   confirmado dado nenhum.
6. "Corrigir" (busca manual) continua disponível e inalterado — para quando a
   prévia erra ou o dono quer procurar outro título.
7. O botão de lote "Auto-sincronizar elegíveis", no topo da tela, continua
   como está — cobre o caso de "confiar no processo sem revisar item a
   item", um uso genuinamente diferente da revisão individual.

## 3. Escopo

**Dentro:**
- Endpoint novo de prévia (dry-run, sem side-effect) que devolve o melhor
  candidato encontrado (local OU externo) com pôster/título/ano/score/fonte,
  reaproveitando a busca+pontuação do 0050 SEM o corte pelo
  `LimiarAutoMatch`.
- Endpoint novo para aplicar o candidato mostrado na prévia, com o dono
  decidindo mesmo com score abaixo do limiar automático.
- Redesenho visual do `ReviewCard`: bloco de prévia (pôster/título/ano/score/
  fonte, ou estado vazio, ou estado de erro) acima dos botões de ação.
- Fusão de semântica do botão principal: "Confirmar candidato" quando há
  prévia, "Manter como está (sem metadados externos)" quando não há —
  documentada e justificada em §6.
- Remoção do botão "Auto-sincronizar" **individual** do card (redundante:
  a mesma busca/pontuação agora aparece ANTES do clique via a prévia, e
  "Confirmar candidato" já aplica o mesmo resultado com supervisão).

**Fora (explicitamente):**
- O botão "Corrigir" (busca manual) — continua igual; pôster nos resultados
  da busca manual (`CorrigirPainel`/`ResultadosCorrecao`) fica como
  follow-up (§7), não é feito aqui.
- O botão de lote **"Auto-sincronizar elegíveis"** do topo da tela — mantido
  sem alteração (cobre um uso diferente: confiar no processo sem revisar
  item a item).
- Mudar `LimiarAutoMatch`, a pontuação (`internal/matching.Pontuar`) ou
  qualquer parte do algoritmo de busca/scoring do 0050.
- Mudar o comportamento do job de saúde (`AutoMatchElegiveis`,
  0053/0054) ou do enriquecimento retroativo (0056) — ambos continuam
  usando o caminho automático existente, sem prévia (são processos em
  background, não uma tela supervisionada).
- Cache/paralelização de prévias em lote — um request por card no mount é o
  suficiente para o volume esperado da fila (ver §6).

## 4. Contrato técnico

### Dados / modelo
- Nenhuma tabela ou migração nova. A prévia é 100% derivada de leituras já
  existentes (`buscarLocal`, `buscarExterno`, `matching.Pontuar`) — nenhum
  campo novo precisa ser persistido.

### API

**`GET /api/v1/entities/{id}/match-preview`** — dry-run, read-only, SEM
side-effect. 404 se a entity não existe. Sempre `200` mesmo sem candidato
(resultado válido, não erro) — mesma convenção do `auto-match` (0050).

```json
{
  "found": true,
  "method": "local",
  "score": 87.3,
  "meets_auto_threshold": false,
  "target": {
    "title": "Duna",
    "year": 2021,
    "poster_url": "/api/v1/entities/8f2.../assets/poster",
    "local_id": "8f2a1c...",
    "source": null,
    "source_id": null
  }
}
```

Quando `method` é `"external"`, `target.local_id` é `null` e
`target.source`/`target.source_id` vêm preenchidos (`poster_url`, aqui, é a
URL CRUA do provider — a mesma que `provider.ResultadoExterno.PosterURL` já
traz na busca; o front roteia pelo proxy de imagem existente, `arteSrc`, como
já faz para outras artes cruas de provider).

Sem candidato em nenhuma fonte:
```json
{"found": false, "method": "none", "score": 0, "meets_auto_threshold": false, "target": null}
```

**`POST /api/v1/entities/{id}/confirm-candidate`** — aplica o candidato que a
prévia mostrou, **sem checar `LimiarAutoMatch`** (é uma decisão humana
informada, não um corte automático). 404 se a entity não existe; 422 se o
corpo não bate o `method` (falta `local_id` ou falta `source`/`source_id`).

```json
// candidato local
{"method": "local", "local_id": "8f2a1c..."}
```
```json
// candidato externo
{"method": "external", "source": "tmdb", "source_id": "603"}
```

Resposta: a entity resolvida (mesmo shape de `responderResolvida`, como
`confirmEntity`/`correctEntity` já devolvem), com o mesmo cabeçalho
`X-Duplicata-Mesclada` que `correct` já usa quando aplicável (branch externo,
via `resolverColisaoExterna`). Erros: 409 quando o `source`/`source_id`
colide com outra entity que já tem investimento do dono (rede de segurança do
0051, mesma que `/correct` já expõe hoje); 502 se a fonte externa falhar ao
entregar o detalhe; 503 se o enrich está desligado.

`/entities/{id}/confirm` (o "Manter como está") e `/entities/{id}/correct`
continuam existindo, inalterados — `confirm-candidate` é um TERCEIRO
endpoint, não uma reforma dos outros dois.

### Backend (Go)

- **Refatorar o corte de limiar para fora dos finders.** Hoje
  `melhorCandidatoLocal` e `melhorCandidatoExterno`
  (`internal/server/automatch.go:329,356`) descartam qualquer candidato com
  `pontos < LimiarAutoMatch` DENTRO do laço — a prévia precisa do melhor
  candidato **independente do limiar** (só o corte de `descartado`, o
  desqualificador rígido de ano divergindo >1, continua valendo — isso é
  incompatibilidade estrutural, não falta de confiança). Extrair o corte de
  score para o CHAMADOR: os finders passam a devolver sempre o
  maior-pontuado entre os não-descartados; `tentarAutoMatch` (0050) aplica o
  corte `>= LimiarAutoMatch` como hoje; o novo handler de prévia usa o mesmo
  finder sem cortar, só marcando `meets_auto_threshold` no DTO. Preserva 100%
  do comportamento do 0050 (mesmo resultado, resultado só filtrado num lugar
  diferente); os testes existentes de `automatch_test.go` continuam válidos
  com a asserção de threshold movida para o chamador.
- **Handler da prévia** (`matchPreview`, novo em `automatch.go` ou um arquivo
  novo `match_preview.go`): reusa `tituloAnoDoArquivo` → `buscarLocal` →
  finder local (sem corte) → se não achou, `buscarExterno` → finder externo
  (sem corte). Monta o DTO com `poster_url`: para local, o `PosterURL` que já
  vem de graça em `entityCardDTO` (a busca local já popula isso via
  `cardDTO`/`posterPath`, zero custo extra); para externo, o `PosterURL` que
  já vem de graça em `provider.ResultadoExterno` (a busca externa já traz
  isso). **Nenhuma chamada extra de rede** é necessária — os dois finders já
  usados pelo 0050 já carregam o pôster na resposta da busca, só não eram
  propagados até agora porque `autoMatchTargetDTO` não tinha campo para isso.
- **Handler de aplicação** (`confirmCandidate`, mesmo arquivo): valida
  `method` e os campos obrigatórios (422 se faltando). Branch `local`:
  `s.fundirEntities(ctx, body.LocalID, id)` — sobrevivente é sempre o
  `local_id` do corpo (o candidato escolhido), perdedora é sempre o item da
  fila; sem cenário de colisão aqui (o dono já escolheu os dois lados
  explicitamente, não há "qual dos dois vence" para decidir). Branch
  `external`: mesma sequência de `correctEntity`
  (`internal/server/review.go:141-169`) a partir de
  `resolverColisaoExterna` — reusa com o **ResponseWriter real** da
  requisição (NÃO a `capturaResposta` que `aplicarCandidatoExterno` usa para
  o fluxo automático/em lote do 0050): aqui é uma ação explícita de UM clique
  do dono, então uma colisão real deve virar um 409 de verdade na tela, não
  ser silenciosamente degradada a "nada aconteceu" como no auto-match
  desatendido. Para não duplicar a sequência `resolverColisaoExterna` →
  `idiomasPermitidos` → `s.enrich.Enriquecer` → `finalizarRevisao` que já
  existe dentro de `correctEntity`, extrair esse miolo para um helper
  compartilhado (ex. `aplicarIdentidadeExterna(w, ctx, id, tipo, source,
  sourceID string) bool`) chamado por AMBOS `correctEntity` e o branch
  externo de `confirmCandidate` — mesmo padrão de extração que o 0050 já usou
  para `aplicarCandidatoExterno`, só que aqui com o `w` real em vez de
  capturado.

### Frontend (Vite/React)

- **Query de prévia por card**: `matchPreviewQueryOptions(id)` em
  `web/src/lib/queries.ts`, chave `['match-preview', id]`, chamada no mount
  de cada `ReviewCard` (`useQuery`, não lazy) — um request por card. Aceitável
  para o volume da fila de Revisão (dezenas de itens por scan, não milhares —
  mesma ordem de grandeza que já justificou o botão de lote do 0050); se o
  volume crescer a ponto de doer, um endpoint em lote (espelhando
  `POST /review/auto-match`) é um follow-up natural (§7), não construído
  agora (YAGNI).
- **`ReviewCard`**: adiciona o bloco de prévia entre o cabeçalho (pôster do
  item + título + badge de motivo) e a linha de botões. Três estados: loading
  (skeleton discreto), achou candidato (pôster próprio + título + ano + selo
  de score + selo de fonte), não achou (mensagem neutra) — mais um quarto
  estado de erro de busca (mensagem curta, não bloqueia o resto do card).
  Pôster do candidato usa `posterSrc` (local) ou `arteSrc` (externo, roteia
  pelo proxy de imagem existente) — nenhum helper novo de imagem é
  necessário, ambos já existem em `web/src/lib/api.ts`.
- **Botão principal**: substitui o "Confirmar" atual.
  - `preview.found === true` → rótulo **"Confirmar candidato"**, mutation
    `confirmCandidate(id, preview.method === 'local'
    ? {method:'local', local_id: preview.target.local_id}
    : {method:'external', source: preview.target.source, source_id: preview.target.source_id})`.
  - `preview.found === false` (ou a prévia falhou ao carregar) → rótulo
    **"Manter como está (sem metadados externos)"**, mutation
    `confirmMatch(id)` (endpoint `/confirm` inalterado).
- **Remove** o botão "Auto-sincronizar" individual e seu estado
  (`autoSincronizar`, `avisoAuto`) do `ReviewCard` — a prévia + "Confirmar
  candidato" cobrem o mesmo caso de uso com supervisão, tornando o botão
  redundante no card individual. O botão de lote no topo da tela
  (`autoMatchLote`) e seu `resumoLote` continuam intocados.
- Invalidação após `confirmCandidate`: mesma de hoje —
  `invalidarRevisao(qc, id)` (`['review']`, `['entities']`, `['entity', id]`).

### Arquivos afetados
- `internal/server/automatch.go` — extrair o corte de `LimiarAutoMatch` dos
  finders para o chamador; `melhorCandidatoLocal`/`melhorCandidatoExterno`
  passam a sempre devolver o maior-pontuado não-descartado.
- Novo: `internal/server/match_preview.go` (+ `match_preview_test.go`) —
  handlers `matchPreview` e `confirmCandidate`, DTOs `matchPreviewDTO`/
  `confirmCandidateBody`.
- `internal/server/review.go` — extrai o miolo de `correctEntity`
  (`resolverColisaoExterna` → `idiomasPermitidos` → `Enriquecer` →
  `finalizarRevisao`) para um helper compartilhado com o branch externo de
  `confirmCandidate`.
- `internal/server/server.go` — registra `GET /entities/{id}/match-preview` e
  `POST /entities/{id}/confirm-candidate`.
- `openapi.yaml` — schemas `MatchPreview`, `MatchPreviewTarget`,
  `ConfirmCandidateBody` + os dois paths novos.
- `web/src/lib/api.ts` — `getMatchPreview`, `confirmCandidate` + tipos.
- `web/src/lib/queries.ts` — `matchPreviewQueryOptions`.
- `web/src/routes/review.tsx` — `ReviewCard` redesenhado (bloco de prévia,
  botão principal fundido, remoção do "Auto-sincronizar" individual).
- Sem migration nova.

## 5. Critérios de aceite

- [ ] `GET /entities/{id}/match-preview` nunca aplica nada no banco (dry-run
  puro) e devolve `200` mesmo sem candidato em nenhuma fonte.
- [ ] A prévia devolve o candidato de MAIOR score entre local e externo,
  mesmo quando o score é menor que `LimiarAutoMatch` (92) — só um candidato
  com ano divergindo em mais de 1 ano (`descartado`) é excluído.
- [ ] Quando a prévia acha candidato local, `poster_url` vem preenchido sem
  nenhuma chamada de rede extra além da busca já feita; idem para externo.
- [ ] `POST /entities/{id}/confirm-candidate` aplica o candidato local
  (`fundirEntities`) ou externo (mesma sequência de `correctEntity`) **mesmo
  com score abaixo de 92** — a checagem de limiar só existe no fluxo
  automático (0050), não neste endpoint.
- [ ] Uma colisão de identidade externa no branch `external` de
  `confirm-candidate` responde `409` de verdade na requisição (não degrada
  silenciosamente como o auto-match em lote faz).
- [ ] `ReviewCard` mostra pôster/título/ano/score/fonte do candidato ANTES de
  qualquer clique, ou a mensagem "nenhum candidato encontrado" quando
  aplicável.
- [ ] O botão principal do card muda de rótulo/ação conforme a prévia:
  "Confirmar candidato" (aplica o mostrado) quando há candidato, "Manter como
  está (sem metadados externos)" (comportamento antigo do "Confirmar") quando
  não há.
- [ ] O botão "Auto-sincronizar" individual não existe mais no
  `ReviewCard`; o botão de lote "Auto-sincronizar elegíveis" no topo da tela
  continua funcionando exatamente como antes.
- [ ] "Corrigir" (busca manual) continua funcionando sem nenhuma mudança de
  comportamento.
- [ ] Falha ao buscar a prévia (ex.: fonte externa fora do ar) não impede o
  resto do card de funcionar — o card degrada para o rótulo "Manter como
  está" e "Corrigir" continua disponível.

## 6. Notas de implementação / decisões

- **Decisão central: fundir a semântica dos botões (alternativa (a)), não só
  empilhar uma prévia visual sobre os três botões existentes (alternativa
  (b)).** A alternativa (b) — manter "Confirmar"/"Corrigir"/"Auto-sincronizar"
  como estão e só desenhar a prévia acima — foi descartada porque não resolve
  a confusão relatada: o dono continuaria podendo ver um candidato forte na
  prévia e clicar em "Confirmar" por hábito, que SEGUE aceitando cegamente o
  palpite do scanner sem tocar no candidato mostrado — a mesma armadilha de
  hoje, só que agora com um preview ao lado que o dono pode ignorar por
  engano. A opção (a) elimina essa armadilha ao fazer o botão principal
  SEMPRE corresponder ao que está na tela: se um candidato aparece, o botão
  aplica ele; se não aparece, o botão diz exatamente o que vai fazer (nada
  externo). Isso também resolve, de graça, a redundância entre "Confirmar" e
  "Auto-sincronizar" apontada no diagnóstico do bug — não sobra mais uma
  ação que "faz a mesma busca só que depois do clique e sem explicar por
  quê".
- **Por que remover o "Auto-sincronizar" individual e não o de lote**: o
  individual e a nova prévia fazem EXATAMENTE a mesma busca/pontuação
  (0050) — a única diferença era o momento (antes vs. depois do clique) e a
  falta de detalhe (frase vs. pôster+score). Com a prévia, o botão individual
  não tem mais nenhuma capacidade que "Confirmar candidato" não já ofereça
  com MAIS informação. O de lote, no topo da tela, resolve um problema
  diferente — processar dezenas de itens sem abrir um por um — que continua
  válido mesmo com a prévia por card (ver 0050 §1, motivação original:
  popular a biblioteca de uma vez via rclone/Google Drive).
- **Por que permitir aplicar abaixo do limiar automático (ponto 4 do
  pedido)**: o `LimiarAutoMatch = 92` (matching.go:22) é deliberadamente
  conservador porque o auto-match roda SEM supervisão — o custo de um
  falso-positivo automático é alto (contamina o catálogo em silêncio). Um
  candidato com score 80-91 mostrado NA TELA, com pôster/título/ano visíveis,
  já dá ao dono a mesma informação que ele usaria para confirmar manualmente
  via "Corrigir" — só que sem precisar digitar a busca de novo. Negar essa
  aplicação e forçar o dono a reabrir "Corrigir" e buscar o MESMO título de
  novo seria fricção sem ganho de segurança (a decisão de aplicar ou não já é
  humana nos dois casos).
- **Por que um endpoint novo (`confirm-candidate`) em vez de reaproveitar
  `/correct` para o branch externo**: cheguei a considerar despachar o
  branch externo direto para o `/correct` já existente (mesmo corpo
  `{source, source_id}`) e só criar endpoint novo para o branch local.
  Descartei porque isso obrigaria o front a escolher entre DOIS endpoints
  diferentes conforme `preview.method`, vazando um detalhe de implementação
  do backend (de onde veio o candidato) para a lógica do botão — que deveria
  só saber "aplicar o que a prévia mostrou". Um único endpoint que despacha
  internamente por `method` mantém o front com UMA mutation para o botão
  principal, e ainda cria um ponto de auditoria dedicado ("isto foi uma
  confirmação de prévia", diferente de "isto foi uma correção manual via
  busca") caso o dono queira essa distinção no futuro (ex. telemetria,
  0055).
- **Por que a prévia não corta pelo `LimiarAutoMatch` mas o auto-match
  continua cortando**: são o MESMO score, dois consumidores diferentes. A
  extração do corte do finder para o chamador (em vez de duplicar a lógica de
  scoring com um parâmetro `semLimiar bool`) segue o mesmo princípio que o
  0050 já usa (`aplicarCandidatoExterno` extraído para ser reusado por dois
  fluxos) — um único lugar pontua, cada chamador decide o que fazer com o
  resultado.
- **Pôster sem custo extra**: tanto `entityCardDTO` (candidatos locais)
  quanto `provider.ResultadoExterno` (candidatos externos) já carregam
  `poster_url`/`PosterURL` na resposta da BUSCA, não do detalhe — confirmado
  lendo `internal/server/entities.go:239-254` (`cardDTO`/`posterPath`) e
  `internal/provider/provider.go:307-322`. Isso é o que torna a prévia
  barata: nenhuma chamada adicional ao provider é necessária além da que o
  0050 já fazia.

## 7. Fora deste briefing / follow-ups

- Pôster nos resultados do "Corrigir" (`ResultadosCorrecao`) — hoje mostra só
  título/ano; ganharia a mesma riqueza visual da prévia, mas é escopo
  separado (painel de busca manual, não a prévia automática).
- Endpoint de prévia EM LOTE (`POST /review/match-preview`), caso o volume
  típico da fila cresça a ponto de N requests-por-mount pesar — não há sinal
  disso hoje.
- Telemetria distinguindo "confirmado via prévia" vs. "corrigido via busca
  manual" (mencionado como motivação lateral da decisão de endpoint em §6,
  não implementado aqui).
- Estender a prévia para a fila de Órfãs (`/orphans`) — hoje o 0050
  auto-match cobre só a fila de Revisão; a prévia poderia seguir o mesmo
  padrão lá se fizer sentido no futuro.
