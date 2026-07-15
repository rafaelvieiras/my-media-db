# 0056 — Enriquecimento externo retroativo via job de saúde

> O job `saude_biblioteca` (0053/0054) já re-escaneia fontes e roda auto-match
> em lote sobre a fila de Revisão — mas existe uma população de itens que
> **nunca chega** à fila e por isso nunca é revisitada: obras cujo nome de
> arquivo já foi parseado com sucesso (título+ano) no scan, sem nenhum id
> externo embutido, e que por isso ficam `cataloged` com confiança exata no
> limiar (70) — fora da fila, sem NENHUMA tentativa real contra um provider
> externo. Este briefing estende o mesmo job para também varrer essa
> população, periodicamente, com throttling e best-effort.

| | |
|---|---|
| **ID** | 0056 |
| **Tipo** | feature |
| **Fase (roadmap)** | Transversal / infraestrutura operacional — extensão do job `saude_biblioteca` ([0053](0053-job-saude-biblioteca.md)/[0054](0054-auto-match-no-import-manual-e-job-de-saude.md)), mesmo espírito operacional. |
| **Docs relacionadas** | [06-biblioteca-e-scan](../docs/06-biblioteca-e-scan.md), [07-enriquecimento-providers](../docs/07-enriquecimento-providers.md), [ADR-0008](../docs/adr/0008-identidade-e-matching-kodi-jellyfin.md), [ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md) |
| **Status** | done |
| **Depende de** | [0050](0050-auto-match-fila-de-revisao.md), [0053](0053-job-saude-biblioteca.md), [0054](0054-auto-match-no-import-manual-e-job-de-saude.md) |

---

## 1. Objetivo

O scanner ([0009](0009-scanner-fswalk-ffprobe-matching.md), `internal/scanner/match.go`)
**nunca chama nenhum provider externo** — ele só faz parsing de nome de
arquivo (`naming.Parse`) e classifica a entity puramente por isso
(`classificar`, `confianca`, `match.go:190-219`):

- Nome traz id embutido (`{tmdb-...}`/`{imdb-...}`) → `cataloged`,
  confiança 100 (`confIDExterno`).
- Nome parseia título **e** ano, sem id → `cataloged`, confiança **exatamente
  70** (`confTituloAno`) — este é o caminho mais comum de uma biblioteca real
  (`"O Foragido (1993).mkv"` parseia limpo, sem id embutido).
- Nome parseia só título (sem ano) → `cataloged`, confiança 40
  (`confTituloSoNom`).
- Nome não parseia nada → `draft`, confiança 0 (`confDraft`).

A fila de Revisão (`ListarRevisao`, [0018](0018-fila-revisao-orfas-correcao-match.md))
é `WHERE status='draft' OR (0 < match_confidence < limiar)`, com
`limiar = 70` (`limiarBaixaConfianca`/`curation.LimiarBaixaConfianca`/
`matching`). **Confiança exatamente 70 não é `< 70`** — então o caso mais
comum acima (título+ano parseado, sem id) **nunca entra na fila**, nunca é
visto pelo `POST /review/auto-match`, nunca é tocado por
`Matcher.AutoMatchElegiveis` (0054) — apesar de nunca ter sido checado contra
NENHUM provider real. É um item **local puro** (`external_ids_json IS NULL`)
que parece "resolvido" (título e ano bonitinhos, sem badge de baixa
confiança) mas na verdade nunca ganhou pôster, sinopse, elenco ou qualquer
metadado — só o que o nome do arquivo já dizia.

Draft (`status='draft'`) **já está** coberto pelo mecanismo existente — ele
sempre cai em `ListarRevisao` pela cláusula `status='draft'`, e o
`Matcher.AutoMatchElegiveis` do job já tenta local+externo nele a cada
execução (0054). O gap real não é "draft esquecido" — é o item **cataloged
com confiança 70**, que nem o 0050 nem o 0054 alcançam, porque nenhum dos
dois olha para fora de `ListarRevisao`.

Este briefing fecha esse gap: o job `saude_biblioteca` passa a varrer,
também, o **complemento exato** de `ListarRevisao` restrito a itens locais
puros (`external_ids_json IS NULL`, com arquivo no disco) — busca no
provider default, aplica sozinho com alta confiança (mesmo limiar do 0050,
`matching.LimiarAutoMatch = 92`), ou **sinaliza para a fila de Revisão**
(reaproveitando o mecanismo já existente de `match_confidence` baixo) quando
achou algo plausível mas não confiável o bastante — nunca inventa uma
terceira fila nem uma flag nova de UI.

## 2. Jornada do usuário

1. O dono aponta uma fonte nova para uma pasta com nomes de arquivo já bem
   formatados (`"Duna (2021).mkv"`, `"Oppenheimer (2023).mkv"`, sem id
   embutido). O scan roda, tudo vira `cataloged` com confiança 70 — a
   biblioteca parece "completa" na listagem (título e ano corretos), mas
   nenhum item tem pôster, sinopse ou elenco, porque nada foi buscado no TMDB
   ainda.
2. Às 4h, o job `saude_biblioteca` roda (mesmo agendamento do 0053). Depois
   do re-scan e do auto-match em lote da fila de Revisão (0054), ele agora
   também varre um **lote limitado** (ex.: 15 itens) desses "cataloged, nunca
   verificado" — os mais antigos sem tentativa primeiro.
3. Para cada um: busca local-first e depois no provider default (TMDB). Achou
   com confiança alta (≥ 92) → aplica sozinho (liga a uma entity local
   existente, ou importa e enriquece a partir do candidato externo) — o item
   ganha pôster/sinopse na hora, sem o dono perceber.
4. Achou algo plausível mas não confiável (ex.: dois filmes homônimos de anos
   próximos) → o item passa a aparecer na fila de **Revisão** — o dono já
   conhece essa tela e resolve manualmente como sempre resolveu itens de
   baixa confiança.
5. Não achou nada (provider fora do ar, sem resultado, título ambíguo demais)
   → nada muda visualmente; o job só registra que tentou, para não bater no
   provider de novo por um tempo (throttle).
6. Na manhã seguinte, o card de Jobs mostra, além do resumo de sempre: *"…;
   enriquecimento retroativo: 3 aplicado(s), 2 sinalizado(s) para revisão"* —
   o dono abre `/review` e já vê os 2 sinalizados, com os outros 3 já
   completos sem ele ter feito nada.
7. Ao longo dos dias seguintes, o job continua varrendo o restante do
   backlog em lotes pequenos, até esgotar (ou até o dono usar o botão manual
   por item do 0059 para acelerar um caso específico).

## 3. Escopo

**Dentro:**
- Extensão do `Handler` `saude_biblioteca` (`internal/job/libraryhealth`,
  0053): um novo passo, executado DEPOIS do re-scan e do auto-match em lote
  (0054), que varre o complemento de `ListarRevisao` restrito a itens locais
  puros com arquivo presente.
- Nova query sqlc (`internal/db/queries/review.sql`) que é o **complemento
  exato** (De Morgan) do `WHERE` de `ListarRevisao`: `external_ids_json IS
  NULL`, `status != 'draft'`, sem nenhum `media_file` de baixa confiança
  (`0 < match_confidence < limiar`), com ao menos um `media_file` `present`,
  restrita a `type IN ('movie','tv')` (mesmo escopo de nível obra/série-topo
  do 0050) — ver §4.
- Throttling por item: nova coluna `entity.last_external_match_attempt_at` +
  cláusula de "não tentado ainda OU tentado há mais de N dias" + `LIMIT` por
  execução — ver §4/§6 para os valores escolhidos e a justificativa.
- Reaproveita **integralmente** o motor de busca/pontuação já existente:
  `s.buscarLocal`/`s.buscarExterno` (`internal/server/search.go`),
  `matching.Pontuar`/`matching.LimiarAutoMatch` (0054), `s.fundirEntities`
  (`internal/server/duplicata.go`, 0051) para aplicar local, e a MESMA
  sequência de aplicação externa do "Corrigir"/auto-match
  (`resolverColisaoExterna` → `s.enrich.Enriquecer` → `finalizarRevisao`) —
  extraída para um pequeno helper compartilhado com `tentarAutoMatch`
  (0050), sem duplicar lógica.
- Comportamento **sinalizar para revisão**: quando um candidato (local ou
  externo) foi encontrado mas não bate `LimiarAutoMatch`, grava esse score
  (com um piso mínimo de plausibilidade e um teto para garantir que caia na
  fila — ver §6) via `AtualizarConfidencePorEntity` (query já existente do
  0018) — o item some da "invisibilidade" e aparece em `/review` como
  qualquer outro item de baixa confiança, sem UI nova.
- Best-effort, mesmo padrão do 0053/0054: sem provider configurado, ou erro
  do provider, ou erro num item individual, não derruba o job — só reduz o
  que esse passo consegue reportar.
- Testes: query nova cobrindo os quatro quadrantes (draft, baixa confiança,
  confiança=70 sem id, confiança=70 com id) + throttle (nunca tentado vs.
  tentado recentemente vs. tentado há muito tempo); `Handler` com fake do
  novo método da interface; aplicação local/externa/sinalização com fakes de
  provider.

**Fora (explicitamente):**
- **Trigger manual por item** ("baixar informações faltando" no grupo de
  botões da listagem) — é o 0059 (fora deste briefing, mas vai
  **depender** deste: reusa a mesma orquestração de busca+aplicação
  documentada aqui, só que disparada para UM id específico em vez de em
  lote pelo job).
- Múltiplos providers simultâneos além do já configurado como default —
  mesmo comportamento de `buscarExterno`/`mesclarBuscas` hoje (consulta
  todas as fontes registradas na ordem de prioridade já existente; este
  briefing não muda essa política).
- Mudar `matching.LimiarAutoMatch` ou o algoritmo de pontuação (0050/0054 já
  fixaram isso; aqui só se reaproveita, com um piso adicional só para a
  decisão de "vale sinalizar", ver §6).
- Árvore de série (temporada/episódio individual) — mesmo fora do 0050/0054.
- Qualquer notificação externa do resultado (fora desde o 0053).
- Config de completude de metadados por tipo (isso é o
  0057, independente deste briefing).
- Alterar a tela `/jobs` do frontend — genérica, o texto novo aparece sozinho
  no histórico/card já existente.

## 4. Contrato técnico

### Dados / modelo

Migration `0013_enriquecimento_retroativo.sql`:

```sql
-- +goose Up
-- Migration 0013 (briefing 0056): coluna de throttle do enriquecimento
-- externo retroativo do job saude_biblioteca. Sem ela, cada execucao do job
-- bateria de novo no provider externo para TODO item elegivel, mesmo que a
-- tentativa anterior (minutos ou horas atras) nao tenha achado nada --
-- caro e sujeito a rate-limit sem necessidade, numa biblioteca que muda
-- pouco de um dia para o outro. NULL = nunca tentado (prioridade maxima na
-- fila do proximo lote); preenchida sempre que o item e efetivamente
-- verificado contra o provider (aplicado, sinalizado ou sem candidato) --
-- nao e tocada quando o passo inteiro e pulado por falta de provider
-- configurado (nesse caso nao houve tentativa real nenhuma).
ALTER TABLE entity ADD COLUMN last_external_match_attempt_at TEXT;

-- +goose Down
-- Reverte 0013. Nenhum indice/coluna gerada referencia a coluna nova, DROP
-- COLUMN direto funciona (mesmo padrao das migrations 0003/0005/0007/0008).
ALTER TABLE entity DROP COLUMN last_external_match_attempt_at;
```

Sem índice dedicado: o `WHERE` da query nova já é ancorado em
`external_ids_json IS NULL` (índice existente via colunas geradas
`external_id_tmdb`/`external_id_imdb`, ADR já documentado no 04) e o volume
por execução é sempre pequeno (biblioteca pessoal — mesmo racional de
volume do 0053).

Novas queries em `internal/db/queries/review.sql` (mesmo arquivo de
`ListarRevisao`/`ListarOrfas` — é o terceiro membro da mesma família de
"filas sobre `entity`/`media_file`"):

```sql
-- name: ListarElegiveisEnriquecimentoRetroativo :many
-- Complemento EXATO (De Morgan) do WHERE de ListarRevisao: entities SEM
-- NENHUM external_id (external_ids_json IS NULL) que NAO estao na fila de
-- Revisao -- ou seja, status != 'draft' E sem nenhum media_file de baixa
-- confianca (0 < match_confidence < limiar). Na pratica, hoje isso e quase
-- sempre o caso confianca=70 (confTituloAno do scanner, titulo+ano
-- parseados do arquivo, sem id embutido) -- NUNCA verificado contra
-- provider nenhum, apesar de "parecer" catalogado. Restrito a type
-- movie/tv (nivel obra/serie-topo, mesmo escopo do briefing 0050) e a
-- entities com pelo menos um media_file 'present' (entity so-importada sem
-- arquivo nao e alvo do scanner, fora de escopo aqui). Throttle: nunca
-- tentado (NULL) tem prioridade, depois o mais antigo tentado primeiro;
-- sqlc.arg('desde') e o corte de "tentado ha pouco tempo, pula por agora";
-- sqlc.arg('limite') poda o lote por execucao (custo/rate-limit do
-- provider externo, briefing 0056).
SELECT e.id, e.type, e.title, e.release_date, e.last_external_match_attempt_at
FROM entity e
WHERE e.external_ids_json IS NULL
  AND e.type IN ('movie', 'tv')
  AND e.status != 'draft'
  AND EXISTS (
        SELECT 1 FROM media_file m
        WHERE m.entity_id = e.id AND m.state = 'present'
      )
  AND NOT EXISTS (
        SELECT 1 FROM media_file m
        WHERE m.entity_id = e.id
          AND m.state = 'present'
          AND m.match_confidence > 0
          AND m.match_confidence < sqlc.arg('limiar')
      )
  AND (
        e.last_external_match_attempt_at IS NULL
        OR e.last_external_match_attempt_at < sqlc.arg('desde')
      )
ORDER BY
    (e.last_external_match_attempt_at IS NULL) DESC,
    e.last_external_match_attempt_at ASC,
    e.title COLLATE NOCASE ASC,
    e.id
LIMIT sqlc.arg('limite');

-- name: AtualizarTentativaMatchExterno :exec
-- Registra que o item FOI verificado nesta execucao (aplicado, sinalizado
-- ou sem candidato) -- throttle do briefing 0056: o proximo lote so volta a
-- considerar este id depois de sqlc.arg('desde') (acima) ter passado.
UPDATE entity
SET last_external_match_attempt_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = sqlc.arg('id');
```

`AtualizarConfidencePorEntity` (já existente, `review.sql`/0018) é reusada
tal como está para o caso "sinalizar" — sem query nova para isso.

### API

Nenhum endpoint novo. O resultado deste briefing só aparece na mensagem do
job `saude_biblioteca` (`GET /api/v1/jobs`/histórico de execução, já
genérico desde o 0052) e, indiretamente, em `GET /review` (itens
sinalizados aparecem lá) e nos endpoints de enriquecimento já existentes
(itens aplicados ganham `external_ids`/assets como qualquer import normal).

### Backend (Go)

- **`internal/server/retroativo.go`** (novo arquivo, mesmo pacote de
  `automatch.go` — evita ciclo, já que a lógica depende de `s.buscarLocal`,
  `s.buscarExterno`, `s.enrich`, `s.fundirEntities`, todos privados ao
  `Server`):
  - Constantes: `limiteEnriquecimentoRetroativoPorExecucao = 15` (teto de
    itens verificados por execução do job — throttling de custo/rate-limit),
    `intervaloReTentativaRetroativa = 30 * 24 * time.Hour` (não retenta o
    MESMO item antes de 30 dias), `limiarSinalizacaoRetroativa = 50.0`
    (piso: candidato com score abaixo disso é ruído — não vale interromper
    o dono, só registra a tentativa).
  - `func (s *Server) EnriquecerRetroativo(ctx context.Context) (aplicados, sinalizados int, err error)`
    — método público que satisfaz a nova interface do job (ver abaixo).
    Passos: se `s.enrich == nil` (sem provider configurado/sem assetsDir,
    mesma guarda de `tentarAutoMatch`), devolve `(0, 0, nil)` sem tocar o
    banco — nenhuma tentativa real acontece, então nada é carimbado (itens
    ficam com prioridade máxima assim que um provider for configurado,
    não esperam o throttle de 30 dias por um período em que não havia
    provider nenhum). Senão, lista os elegíveis via
    `ListarElegiveisEnriquecimentoRetroativo` (limiar =
    `limiarBaixaConfianca`, desde = `time.Now().Add(-intervaloReTentativaRetroativa)`,
    limite = `limiteEnriquecimentoRetroativoPorExecucao`); erro real de banco
    na LISTAGEM propaga (mesmo padrão de `AutoMatchElegiveis`). Para cada
    linha: `ObterEntity` (pula se sumiu — purga concorrente), chama
    `tentarEnriquecerOuSinalizar` (abaixo), sempre grava
    `AtualizarTentativaMatchExterno` no pool de escrita ao final da
    tentativa (best-effort: erro aqui só loga, não aborta o item nem o
    lote), e agrega `aplicados`/`sinalizados`.
  - `func (s *Server) tentarEnriquecerOuSinalizar(ctx context.Context, ent sqlcgen.Entity) (resultado string, err error)`
    — `resultado` em `{"applied", "flagged", "none"}` (só para log/teste, não
    vira DTO de API). Deriva título/ano via `s.tituloAnoDoArquivo` (reusado
    de `automatch.go`, 0050) — `ok=false` (nome não rende título nem
    parseando de novo) devolve `"none", nil` sem buscar nada. Busca local
    (`s.buscarLocal`) e pontua CADA candidato com `matching.Pontuar` **sem**
    o corte de `LimiarAutoMatch` embutido (variante nova, só deste arquivo —
    NÃO reusa `melhorCandidatoLocal`/`melhorCandidatoExterno` de
    `automatch.go`, que já descartam candidato abaixo do limiar antes de
    devolver; aqui precisamos do melhor score mesmo abaixo do limiar, para
    decidir "sinalizar"). Melhor candidato local com score ≥
    `LimiarAutoMatch` → aplica via `s.fundirEntities(ctx, candidato.ID,
    ent.ID)` → `"applied"`. Senão, com `s.enrich != nil`, busca externo
    (`s.buscarExterno`) e pontua do mesmo jeito (sem corte); melhor ≥
    `LimiarAutoMatch` → aplica via `aplicarCandidatoExterno` (extraído de
    `tentarAutoMatch`, ver nota abaixo) → `"applied"`. Sem candidato forte
    em nenhuma fonte: pega o MAIOR score entre o melhor local e o melhor
    externo (ambos já calculados); se ≥ `limiarSinalizacaoRetroativa`,
    grava `AtualizarConfidencePorEntity` com
    `min(score, limiarBaixaConfianca-1)` (o teto garante que o item
    realmente caia em `ListarRevisao` — um candidato pontuado, digamos, 85
    (abaixo de 92, mas ≥ 70) precisa ser rebaixado para menos de 70 para
    aparecer na fila; ver §6) → `"flagged"`. Abaixo do piso, ou nenhum
    candidato em nenhuma fonte: `"none"`.
  - **Extração mínima e reusada por `tentarAutoMatch` (0050) também**:
    `func (s *Server) aplicarCandidatoExterno(ctx context.Context, ent sqlcgen.Entity, cand provider.ResultadoExterno, score float64) (autoMatchResultDTO, error)`
    — corpo idêntico ao bloco hoje inline em `tentarAutoMatch`
    (`automatch.go`, captura `resolverColisaoExterna` →
    `s.enrich.Enriquecer` → `s.finalizarRevisao`, mesma guarda de colisão
    409 degradando para pendente). `tentarAutoMatch` passa a chamar este
    método em vez do corpo inline — diff mecânico, mesmo comportamento
    (critério de aceite: testes existentes do 0050/0054 continuam verdes
    sem alteração de expectativa). Único ponto de aplicação externa no
    pacote, usado pelos DOIS fluxos (fila de Revisão e retroativo).
- **`internal/job/libraryhealth/libraryhealth.go`**: a interface `Matcher`
  (0054) ganha um segundo método:
  ```go
  type Matcher interface {
      AutoMatchElegiveis(ctx context.Context) (resolved, pending int, err error)
      EnriquecerRetroativo(ctx context.Context) (aplicados, sinalizados int, err error)
  }
  ```
  `*server.Server` já vai satisfazer a interface estendida assim que
  `EnriquecerRetroativo` existir — `libraryhealth.New(...)` **não muda de
  assinatura** (continua recebendo um único `matcher Matcher`, já é `s`).
  `Handler.Executar`: depois de `rodarAutoMatch` + recontagem (0054), chama
  um novo `rodarEnriquecimentoRetroativo(ctx) (aplicados, sinalizados int)`
  (mesmo padrão best-effort de `rodarAutoMatch`: `matcher == nil` ou erro →
  `(0, 0)`, loga e segue). Se `sinalizados > 0`, **recontagem de revisão
  novamente** (itens sinalizados passam a aparecer na fila — o resumo final
  precisa refletir o estado depois deste passo, não antes). Mensagem final
  ganha uma cláusula extra, omitida quando `aplicados==0 && sinalizados==0`
  (mesmo espírito da omissão de `resolvidos` quando zero, 0054):
  `"...; enriquecimento retroativo: <A> aplicado(s), <S> sinalizado(s) para revisão"`.
- **`internal/server/server.go`**: nenhuma mudança de assinatura em
  `WithLibraryHealthJob()` — `s` já é passado como `Matcher` para
  `libraryhealth.New`; a interface estendida é satisfeita automaticamente
  assim que `EnriquecerRetroativo` existir no pacote `server`.

### Frontend (Vite/React)

Nenhuma mudança prevista — a tela `/jobs` já é genérica (exibe a mensagem de
texto do histórico como está) e `/review` já renderiza qualquer item que a
query `ListarRevisao` devolver, incluindo os recém-sinalizados por este
briefing.

### Arquivos afetados

- Novo: `internal/server/retroativo.go` (+ `retroativo_test.go`).
- Migration: `internal/db/migrations/0013_enriquecimento_retroativo.sql`.
- Alterar: `internal/db/queries/review.sql` (duas queries novas),
  `internal/server/automatch.go` (extrai `aplicarCandidatoExterno`, chamado
  por `tentarAutoMatch`), `internal/job/libraryhealth/libraryhealth.go`
  (`Matcher` ganha `EnriquecerRetroativo`, `Executar` chama o novo passo,
  mensagem final), `internal/job/libraryhealth/libraryhealth_test.go`
  (fake do `Matcher` ganha o novo método).
- Gerado: `internal/db/sqlcgen/review.sql.go` (via `sqlc generate` —
  `ListarElegiveisEnriquecimentoRetroativo`/`AtualizarTentativaMatchExterno`).

## 5. Critérios de aceite

- [ ] Uma entity `cataloged`, sem `external_ids_json`, com `media_file`
  `present` e `match_confidence = 70` (caminho comum do scanner, título+ano
  parseado sem id) **nunca aparece** em `GET /review` antes deste briefing,
  e é elegível a `ListarElegiveisEnriquecimentoRetroativo` — provado por
  teste de query cobrindo os quatro quadrantes de status/confiança.
- [ ] Rodar `saude_biblioteca` com um provider fake que devolve candidato
  forte (score ≥ 92) para um item elegível aplica sozinho (liga a entity
  local existente via `fundirEntities`, ou importa/enriquece via provider) —
  o item some da população elegível (ganhou `external_ids`) sem intervenção
  do dono.
- [ ] Candidato encontrado com score entre `limiarSinalizacaoRetroativa` (50)
  e `LimiarAutoMatch` (92) faz o item aparecer em `GET /review` depois da
  execução do job (via `match_confidence` atualizado para
  `< limiarBaixaConfianca`), nunca aplicado automaticamente.
- [ ] Candidato abaixo de `limiarSinalizacaoRetroativa`, ou nenhum candidato
  em nenhuma fonte, não altera o item — só grava
  `last_external_match_attempt_at`.
- [ ] Item já tentado há menos de `intervaloReTentativaRetroativa` (30 dias)
  não é retornado por `ListarElegiveisEnriquecimentoRetroativo` na execução
  seguinte (throttle provado com fixture de tempo controlado, não
  `time.Now()` real).
- [ ] Uma execução nunca processa mais que
  `limiteEnriquecimentoRetroativoPorExecucao` (15) itens, mesmo com um
  backlog maior — itens nunca tentados (`NULL`) têm prioridade sobre itens
  já tentados há muito tempo.
- [ ] Sem `s.enrich` configurado (sem provider/sem assetsDir), o passo
  devolve `(0, 0, nil)` sem tocar `last_external_match_attempt_at` — nenhum
  item "gasta" o throttle por um período sem provider nenhum.
- [ ] Erro do provider num item individual não aborta o lote nem o job —
  best-effort, mesmo padrão de `rodarAutoMatch`/falha de fonte no re-scan.
- [ ] `tentarAutoMatch` (0050) continua passando nos testes existentes sem
  alteração de expectativa depois da extração de `aplicarCandidatoExterno`
  (diff mecânico, comportamento idêntico).
- [ ] `POST /jobs/saude_biblioteca/run` com itens elegíveis reflete
  `"<A> aplicado(s), <S> sinalizado(s) para revisão"` na mensagem final, e a
  contagem de `<R> em revisão` já inclui os recém-sinalizados (recontagem
  pós-passo, não antes).
- [ ] `go build/vet/test -count=1` verdes; `tsc -b` do web verde (nenhum
  arquivo de frontend tocado); catraca local não piora.

## 6. Notas de implementação / decisões

- **Por que o gap não é "draft esquecido"**: a leitura inicial do pedido
  sugeria itens `status='draft'` fora da fila de Revisão — mas
  `ListarRevisao` tem `WHERE status='draft' OR ...`, então TODO draft já
  está na fila e já é retentado a cada execução do job via
  `Matcher.AutoMatchElegiveis` (0054). O gap real, confirmado lendo
  `internal/scanner/match.go` linha a linha, é o item `cataloged` com
  `match_confidence` exatamente no limiar (70 = `confTituloAno`) — o
  resultado mais comum de um scan bem-sucedido em nomes de arquivo bem
  formatados, e justamente por parecer "resolvido" (título/ano corretos) é
  o que menos chama atenção do dono para revisar manualmente. A query nova é
  o complemento EXATO (De Morgan) do `WHERE` de `ListarRevisao` — não uma
  aproximação — para não haver sobreposição nem buraco entre as duas.
- **Piso de sinalização (`limiarSinalizacaoRetroativa = 50`) é uma decisão
  NOVA deste briefing, não herdada**: o 0050 só precisava de UM corte
  (aplicar ou não — o item já estava visível de qualquer jeito, "não
  aplicar" bastava). Aqui, "não aplicar" sozinho deixaria o item
  invisível para sempre (o item não estava na fila antes, e continuaria
  fora se nada for escrito) — por isso a sinalização existe. Mas sinalizar
  QUALQUER candidato, por mais fraco, encheria a fila de ruído (dois
  títulos parecidos por coincidência). 50 é conservador na direção oposta
  de 92: alto o bastante para descartar ruído óbvio, baixo o bastante para
  não perder candidatos plausíveis-mas-incertos — mesmo espírito de "prefira
  falso-negativo a falso-positivo" do 0050 aplicado ao ato de INTERROMPER o
  dono, não ao de aplicar sozinho.
- **Teto do valor gravado (`min(score, limiarBaixaConfianca-1)`)**: sem o
  teto, um candidato pontuado entre 70 e 91 (achou algo bom, mas não bom o
  bastante para os 92 do auto-match) seria gravado com um `match_confidence`
  que NÃO bate `< limiar` — e o item continuaria invisível, o oposto do
  pretendido. O teto é uma clampagem deliberada, não perda de precisão
  relevante: a fila de Revisão já ordena por confiança ASC (pior primeiro),
  então vários itens sinalizados colapsando perto do teto (69) só significa
  que, DENTRO desse grupo, a ordenação vira por título — aceitável, o
  objetivo é visibilidade, não um ranking fino.
- **`aplicarCandidatoExterno` extraído para eliminar duplicação real**: sem
  a extração, o corpo de `resolverColisaoExterna` → `Enriquecer` →
  `finalizarRevisao` (com o padrão `capturaResposta` para não vazar escrita
  HTTP) precisaria existir DUAS vezes — em `tentarAutoMatch` e neste
  briefing. Mesmo padrão de extração já usado no 0054
  (`autoMatchLote`/`AutoMatchElegiveis` a partir do corpo de
  `autoMatchReview`).
- **`last_external_match_attempt_at` não é carimbado quando `s.enrich ==
  nil`**: um período de "provider caiu"/"esqueci de configurar a chave" não
  deve custar 30 dias de throttle assim que o dono resolver o problema —
  o item volta a ter prioridade MÁXIMA (nunca tentado) na primeira execução
  com provider disponível de novo.
- **Throttle é por-item, não por-execução**: a alternativa mais simples
  seria "só roda este passo 1x por semana" (flag no `job_definition`), mas
  isso atrasaria TODOS os itens igualmente, mesmo os nunca tentados — o
  throttle por `last_external_match_attempt_at` deixa o job rodar todo dia
  (mesmo cron do re-scan) processando um lote pequeno e sempre priorizando
  quem nunca foi verificado, sem re-bater nos mesmos itens sem sucesso
  todo dia.
- **Sem parâmetro em `config_json` para os números (15/30 dias/50)**: mesma
  filosofia do 0053 — constantes Go, não configuráveis via UI, até
  aparecer necessidade real de ajuste (tuning pós-uso, não especulativo).
- **Coordenação com o botão manual do 0059 (fora deste briefing, mas
  observação para quem implementar)**: o 0059 vai disparar a MESMA
  orquestração (`tentarEnriquecerOuSinalizar` ou equivalente) para um item
  específico, fora do lote do job — ao fazer isso, deveria também carimbar
  `last_external_match_attempt_at` daquele item, para o job do dia seguinte
  não competir pelo mesmo item que o dono acabou de resolver manualmente
  minutos atrás. Não é um requisito DESTE briefing (0059 ainda não existe),
  só uma consistência esperada a herdar.

## 7. Fora deste briefing / follow-ups

- **Trigger manual por item** ("baixar informações faltando") — 0059,
  que depende deste briefing para reusar a mesma orquestração de
  busca+aplicação+sinalização, disparada sob demanda em vez de em lote.
- **Config de completude de metadados desejável por tipo de mídia** —
  0057, independente (não influencia elegibilidade nem limiares
  deste briefing).
- **Paralelismo entre providers/múltiplas fontes simultâneas** — hoje já
  consulta todas as fontes registradas em ordem de prioridade
  (`mesclarBuscas`); paralelizar chamadas de rede entre fontes diferentes
  não foi cogitado (volume baixo, mesmo racional do 0053 para scans).
- **Tuning dos números (15/30 dias/50/92) com base em uso real** — mesmo
  espírito do 0050 §7 para `LimiarAutoMatch`.
- **Extrair um "estado de completude" explícito na entity** (em vez de
  inferir via `external_ids_json IS NULL` + ausência na fila) — cogitado,
  mas a inferência via predicado já é suficiente e evita mais uma coluna
  redundante; revisitar se o 0057/0058 precisarem de um sinal mais rico do
  que "tem ou não tem id externo".
