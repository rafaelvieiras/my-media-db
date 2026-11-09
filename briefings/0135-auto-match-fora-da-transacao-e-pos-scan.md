# 0135 — Auto-match fora da transação do scan e etapa de pós-scan por biblioteca

> Corrige o bug que fazia TODO item novo detectado no scan ficar cru (log real:
> `auto-match imediato do filme … falhou (sql: no rows in result set)`) e fecha a
> lacuna de "o scan acha, mas não processa": ao fim do scan, a própria biblioteca
> reconcília o que ficou em rascunho, sem esperar o cron do dia seguinte.

| | |
|---|---|
| **ID** | 0135 |
| **Tipo** | fix |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | done |
| **Docs relacionadas** | [06-biblioteca-e-scan](../docs/06-biblioteca-e-scan.md), [07-enriquecimento-providers](../docs/07-enriquecimento-providers.md), [ADR-0031](../docs/adr/0031-pos-scan-por-biblioteca.md), [ADR-0028](../docs/adr/0028-fila-de-execucao-unificada-scan-jobs.md) |
| **Depende de** | 0089, 0108, 0120, 0122 |

---

## 1. Objetivo

Duas dores encadeadas, medidas em produção (08/nov/2026):

1. **Item novo do scan fica cru, sempre.** O auto-match imediato de filme
   (briefing 0089) é disparado de dentro de `gravarArquivo`, que roda **dentro
   da transação de escrita do lote** (`escreverLote`, `internal/scanner/scan.go`).
   O auto-match:
   - **lê** a entity pelo pool de LEITURA (`s.leitura().ObterEntity`) — a linha
     ainda não commitada não existe para ele → `sql: no rows in result set`,
     o gatilho aborta e a entity fica `cataloged` sem metadado, sem arte e sem
     `external_ids` (caso real: *Enquanto Você Dormia (1995)*, entity
     `1d05ceb1-…`, scan da biblioteca Filmes em 08/nov 23:39);
   - **escreveria** pelo pool de ESCRITA, que tem **1 conexão**
     (`db.SetMaxOpenConns(1)`, ADR-0010): a primeira escrita do enrich
     esperaria a conexão presa pela própria transação do lote → deadlock
     (reproduzido em teste: o scan nunca termina).
2. **Nada é retentado no fim do scan.** O pipeline de match roda uma única vez
   por arquivo; o que falhou (rede, árvore do artista ainda não existia, série
   importada depois dos episódios) só é retentado pela reconciliação do job
   `saude_biblioteca` — 15 itens por tipo por execução, com throttle de 15 dias
   por item. Na prática: 185 faixas em rascunho ficam semanas na fila de
   revisão mesmo quando o match passaria na segunda tentativa.

## 2. Jornada do usuário

1. O dono joga um filme novo na pasta da biblioteca e clica em **Escanear** na
   biblioteca Filmes.
2. O scan cataloga o arquivo, e **logo em seguida** o item já aparece com
   título/ano/sinopse da fonte, `external_ids` preenchido e pôster (e o
   `.nfo`/artes na pasta quando o toggle de materialização está ligado).
3. Numa biblioteca de música onde o scan casou só parte das faixas, ao terminar
   o dono vê a fila de revisão **encolher na hora**: as faixas cujo match passou
   na segunda tentativa (ou cuja árvore do artista passou a existir no meio do
   scan) saem de rascunho sem clique nenhum.
4. Se o item realmente não tem correspondência na fonte, ele continua na fila de
   revisão — o comportamento não-destrutivo não muda.
5. O progresso do item de fila continua contando **arquivos**, não itens de
   enriquecimento: o pós-scan reporta a própria etapa como mensagem de
   progresso (`message`), sem inflar `progress_total`.

## 3. Escopo

**Dentro:**
- `internal/scanner`: novas entities criadas num lote passam a ser **acumuladas**
  e o gatilho pós-import (auto-match de filme + materialização opcional) roda
  **depois do `tx.Commit()`** do lote, nunca dentro dele. Música ganha o mesmo
  gancho de materialização opcional quando a faixa é resolvida (hoje só filme e
  série têm).
- Etapa de **pós-scan** ao fim de `executarScan` (depois de `finalizarCicloVida`),
  via interface opcional nova `PosScan` injetada por `ConfigurarPosScan` —
  mesmo padrão nil-safe de `FilmeImportador`/`Materializador`:
  - reconciliação das **faixas draft órfãs** daquela biblioteca
    (`ReconciliarFaixaDraft`, briefing 0120), capada;
  - reconciliação das **séries draft com arquivo** daquela biblioteca
    (`ReconciliarSerieDraft`), capada;
  - **auto-match** dos filmes pendentes daquela biblioteca
    (`TentarAutoMatchFilme`), capado — cobre o caso "o item entrou antes de o
    bug acima existir" (ex.: *Enquanto Você Dormia*).
  - materialização opcional (`MaterializarSeAutoDownloadLigado`) das entities
    que passaram a ter identidade nesta etapa, quando o toggle está ligado.
- Consultas sqlc novas para listar drafts/baixa confiança **por library_source**,
  sem o throttle de dias da reconciliação periódica (o pós-scan é o retry do
  scan que o dono acabou de pedir; throttle ali seria contraditório).
- Mensagem de progresso da etapa (`ReportarProgresso` com `message`), para o dono
  ver "reconciliando N rascunhos" no `/library/{id}/scan`.

**Fora (explicitamente):**
- Mudar o limiar de auto-match (92) ou o algoritmo de filme/série.
- Download de trailer/tema (toggle continua desligado por decisão do dono em
  08/nov/2026; briefing 0104/ADR-0026 intocados).
- Paralelizar o pós-scan ou transformá-lo em job separado na fila (evolução
  futura; hoje roda dentro do item `scan` da fila, concorrência 1).
- Reprocessar arte/NFO de TODA a biblioteca no pós-scan (isso é o job
  `midias_faltantes`).

## 4. Contrato técnico

### API
- Nenhuma rota nova. `GET /api/v1/library/{id}/scan` passa a poder devolver
  `message` durante o pós-scan (campo já existente no `queue_item`).

### Backend (Go)
- `internal/scanner/scan.go`:
  - `scanRun.novos []novaEntity` (id + tipo + `sourceID`) acumulado em
    `gravarArquivo`/`gravarArquivoMusica`/`gravarArquivoSerie`;
  - `escreverLote` chama `r.despacharNovos(ctx)` **após** `tx.Commit()`;
  - `despacharNovos` = o corpo que hoje vive em `dispararAutoMatchFilme` +
    materialização (mantido por tipo: filme → auto-match; música → materializa
    a faixa/álbum recém-resolvido; série já é feita na fase de fatos);
  - `executarScan` chama `r.posScan(ctx)` depois de `finalizarCicloVida`.
- `internal/scanner/manager.go`: `PosScan` (interface), `ConfigurarPosScan`,
  `caps` (`capReconciliacaoFaixas = 200`, `capReconciliacaoSeries = 50`,
  `CapAutoMatchPendentes = 50`) como constantes documentadas.
- `internal/server/posscan.go` (novo): implementa `PosScan` reusando
  `ReconciliarFaixaDraft`/`ReconciliarSerieDraft`/`TentarAutoMatchFilme` —
  **nunca** duplica lógica de matching.
- `internal/db/queries/review.sql` (ou `media_file.sql`): queries
  `ListarFaixasDraftOrfasPorSource`, `ListarSeriesDraftPorSource`,
  `ListarPendentesAutoMatchPorSource`.
- `internal/server/server.go`: `s.scanner.ConfigurarPosScan(s)` em
  `montarServicos`.

### Arquivos afetados
- `internal/scanner/scan.go`, `match.go`, `musica.go`, `manager.go`,
  `reconciliar.go` (reuso), testes.
- `internal/server/posscan.go` (novo), `server.go`, testes.
- `internal/db/queries/*.sql` + `internal/db/sqlcgen/*` (gerado).

## 5. Critérios de aceite

- [ ] Varredura de uma pasta com filme novo NÃO dispara auto-match dentro da
      transação: um `FilmeImportador` que escreve no banco (como o enrich) não
      trava o scan (teste de regressão do deadlock).
- [ ] Entity nova de filme com importador stub recebe `TentarAutoMatchFilme`
      **depois** do commit — e o entity já é visível ao leitor quando o gatilho
      roda (asserção: `ObterEntity` dentro do stub devolve a linha).
- [ ] Materialização opcional é chamada só quando o auto-match resolveu
      (comportamento atual preservado) e agora também para faixa de música.
- [ ] Ao fim do scan, `PosScan` é chamado uma vez por biblioteca, com os caps
      respeitados, e erro/deadline nele **não** derruba o scan (best-effort).
- [ ] Pós-scan reconcilía faixa draft e o `media_file` passa a apontar para a
      faixa resolvida.
- [ ] `go build ./... && go vet ./... && go test ./... -count=1` verdes.

## 6. Notas de implementação / decisões

- **Por que não mover só a chamada para o fim do arquivo (fora da tx, dentro do
  laço do lote)?** Porque o lote inteiro é uma transação: qualquer ponto entre
  `BeginTx` e `Commit` está sujo para o pool de leitura e mortal para o de
  escrita. O único lugar seguro é depois do `Commit`.
- **Por que o pós-scan não vira item próprio na fila?** O `execqueue` é
  FIFO/concordância 1 (ADR-0028); um item extra por scan atrasaria o próximo
  scan do dono. O pós-scan é curto e capado, e roda sob o mesmo `ctx` do item
  `scan` — cancelável.
- Alternativa descartada: passar o `*sqlcgen.Queries` da transação para o
  auto-match (leitura dentro da tx). Funcionaria para ler, mas o enrich
  **escreve** pela conexão de escrita — deadlock garantido. Não reabrir.

## 7. Fora deste briefing / follow-ups

- Fila dedicada de enriquecimento com prioridade e retry próprio.
- Reconciliar **episódios provisórios** (0133) no pós-scan — hoje só série e
  faixa têm reconciliação implementada.
