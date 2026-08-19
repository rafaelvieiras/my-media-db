# 0108 — Fila de execução unificada (scan + jobs) e contador de progresso real

> Substitui os dois sistemas paralelos de background (scan e jobs) por uma
> fila única persistida, com um worker sequencial e progresso granular por
> arquivo — fecha a lacuna que fazia `files_seen` parecer travado em scans de
> música.

| | |
|---|---|
| **ID** | 0108 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 5+ — Verticais e plugins (música) |
| **Status** | done |
| **Docs relacionadas** | [ADR-0028](../docs/adr/0028-fila-de-execucao-unificada-scan-jobs.md), [06-biblioteca-e-scan](../docs/06-biblioteca-e-scan.md) |
| **Depende de** | 0107 |

---

## 1. Objetivo

Hoje `internal/scanner.Manager` e `internal/job.Manager` são dois sistemas
independentes que reimplementam o mesmo padrão ("goroutine solta + mapa em
memória + mutex"), sem fila real e sem limite de concorrência entre execuções
de referências diferentes. O scan não persiste histórico (estado some no
restart) e o contador `files_seen` só avança por lote de 25 arquivos — em
`content_type=music`, onde cada arquivo pode custar chamadas de rede lentas
(MusicBrainz a 1 req/s), isso faz o progresso parecer travado em `0` por
minutos reais mesmo com entidades sendo criadas de verdade (achado real na
validação em produção do briefing 0107, seção 7).

Este briefing implementa a decisão da ADR-0028: uma fila única persistida,
worker sequencial (concorrência 1, nunca ocioso enquanto há item pendente),
progresso por arquivo processado.

## 2. Jornada do usuário

1. O dono dispara um scan (`POST /library/{id}/scan`) ou um job manual
   (`POST /jobs/{id}/run`). A resposta continua imediata (202/200,
   contrato inalterado) — por baixo, o pedido vira um item `queued` na fila.
2. Se já existe um item `queued`/`running` para a mesma fonte/job, o pedido
   é rejeitado com 409 (mesmo comportamento observável de hoje).
3. O worker global processa a fila em ordem de chegada — nunca dois itens ao
   mesmo tempo, nunca parado enquanto há fila.
4. Consultando `GET /library/{id}/scan`, o dono vê `files_seen` subir a cada
   arquivo processado (não a cada 25) — inclusive num scan de música lento,
   o número sobe a cada faixa catalogada, dando feedback real de que o scan
   está vivo.
5. Se o processo reinicia no meio de um scan/job, o histórico da execução
   (o que rodou, quando, resultado) sobrevive — hoje isso só é verdade para
   jobs, não para scan.

## 3. Escopo

**Dentro:**
- Novo pacote `internal/execqueue`: tabela `queue_item` (migration nova),
  `Manager`/`Queue` com `Enqueue(kind, refID, trigger) (id, error)`, worker
  goroutine único iniciado no boot, callback de progresso
  (`ReportarProgresso(itemID, current, total, message)`).
- Migração de dados de `job_execution` → `queue_item` (`kind='job'`) na
  mesma migration, preservando histórico existente.
- `internal/scanner.Manager` deixa de disparar sua própria goroutine
  (`go m.run(...)`) — `Start()` passa a chamar `execqueue.Enqueue("scan",
  sourceID, trigger)`. A lógica de scan em si (`coletarArquivos`,
  `processarLotes`, `computarFacts`, ramos filme/série/música) **não muda**
  — só quem a invoca e quem reporta progresso.
- `internal/job.Manager.Executar` passa a enfileirar via `execqueue` em vez
  de `go m.executar(...)`. Contrato `job.Handler{Nome, Executar}` inalterado.
- `saude_biblioteca` (`internal/job/libraryhealth`), que hoje chama
  `scanner.Manager.Start`/`Status` diretamente por fonte, passa a usar a
  mesma interface de enfileiramento/consulta do `execqueue` (via a
  interface `ScanRunner` já existente, adaptada ou substituída).
- Progresso por arquivo: `addSeen` (ou equivalente) passa a ser chamado a
  cada arquivo processado dentro do laço que hoje só reporta por lote de 25
  — o lote de 25 continua existindo só para a escrita em banco
  (`processarLotes`), desacoplado do reporte de progresso.
- `GET /api/v1/jobs/{id}/executions` passa a ler de `queue_item` filtrado
  por `kind='job'` — contrato de resposta (JSON) **inalterado** para o
  frontend.
- `GET /api/v1/library/{id}/scan` passa a ler o estado do item de fila
  correspondente (`kind='scan'`) — contrato de resposta inalterado
  (`running`, `files_seen`, `files_total`, `started_at`).

**Fora (explicitamente):**
- Múltiplos workers/paralelismo por categoria (scan não bloqueia jobs) —
  registrado como evolução futura na própria ADR-0028, não faz parte deste
  MVP.
- Retry automático ou timeout de execução — nenhum dos dois sistemas tinha
  isso antes; não é objetivo deste briefing introduzir.
- Mudança de UI dedicada à fila (ex.: tela "ver fila completa") — a tela de
  Jobs existente (`/dashboard/jobs`) continua consumindo o mesmo contrato
  de API; nenhuma tela nova aqui.

## 4. Contrato técnico

### Dados / modelo
- Migration nova: `CREATE TABLE queue_item (id TEXT PRIMARY KEY, kind TEXT
  NOT NULL CHECK (kind IN ('scan','job')), ref_id TEXT NOT NULL, trigger
  TEXT NOT NULL CHECK (trigger IN ('manual','schedule','watch')), status
  TEXT NOT NULL CHECK (status IN ('queued','running','success','error'))
  DEFAULT 'queued', progress_current INTEGER NOT NULL DEFAULT 0,
  progress_total INTEGER NOT NULL DEFAULT 0, message TEXT, enqueued_at TEXT
  NOT NULL DEFAULT (strftime(...)), started_at TEXT, finished_at TEXT);`
  + índice `idx_queue_item_kind_ref` (kind, ref_id) para o guard de dedup, e
  `idx_queue_item_status_enqueued` (status, enqueued_at) para o worker
  buscar o próximo item.
- Migração de dados: copiar todas as linhas de `job_execution` para
  `queue_item` com `kind='job'`, `ref_id=job_id`, mapeando colunas
  equivalentes; `DROP TABLE job_execution` na mesma migration (sem período
  de transição — não há consumidor externo do schema, só a própria API).

### API
- Nenhuma rota nova. Contratos existentes preservados byte-a-byte:
  `POST /api/v1/library/{id}/scan`, `GET /api/v1/library/{id}/scan`,
  `GET /api/v1/jobs`, `POST /api/v1/jobs/{id}/run`,
  `GET /api/v1/jobs/{id}/executions`.

### Backend (Go)
- `internal/execqueue/` (novo): `manager.go` (Enqueue, worker loop,
  ReportarProgresso, Status/consulta por kind+ref_id), `sqlc` queries
  (`internal/db/queries/queue_item.sql`, ASCII).
- `internal/scanner/manager.go`: `Start` chama `execqueue.Enqueue`;
  `Status` consulta `execqueue`; remover o campo `jobs map[sourceID]*job`
  em memória (substituído pela fila persistida).
- `internal/scanner/scan.go`: progresso por arquivo em vez de por lote —
  identificar o ponto exato onde `computarFacts` retorna por arquivo dentro
  de `processarLotes` e mover a chamada de progresso pra lá.
- `internal/job/manager.go`: `Executar` chama `execqueue.Enqueue`;
  `RunScheduler`/`tick` continuam responsáveis por decidir *quando*
  enfileirar (cron), não por rodar diretamente.
- `internal/job/libraryhealth/libraryhealth.go`: adaptar `ScanRunner`
  (ou equivalente) para o novo contrato de enfileiramento/consulta.
- `cmd/mymediadb/main.go`: iniciar o worker do `execqueue` no boot (`go
  execQueue.Run(ctx)`), antes ou junto do `RunScheduler`.

### Frontend (Vite/React)
- Nenhuma mudança — contratos de API inalterados, telas de Jobs e de
  progresso de scan continuam funcionando sem tocar no frontend.

### Arquivos afetados
- `internal/db/migrations/00XX_fila_execucao_unificada.sql` (novo)
- `internal/db/queries/queue_item.sql` (novo) + sqlc regenerado
- `internal/execqueue/manager.go`, `_test.go` (novo)
- `internal/scanner/manager.go`, `scan.go` (editar)
- `internal/job/manager.go` (editar)
- `internal/job/libraryhealth/libraryhealth.go` (editar)
- `cmd/mymediadb/main.go` (editar)
- `internal/server/scan.go`, `internal/server/jobs.go` (editar, se a forma
  de consultar status mudar de assinatura internamente)

## 5. Critérios de aceite

- [ ] `go test ./internal/execqueue/... -count=1`: enfileirar dois itens da
      mesma referência retorna 409 no segundo; itens de referências
      diferentes coexistem na fila mas rodam um de cada vez (nunca
      `running` simultâneo); progresso reportado via callback é lido
      corretamente por `Status`.
- [ ] Reiniciar o processo no meio de uma execução `running` marca o item
      como encerrado com erro apropriado na próxima leitura (não fica
      `running` para sempre órfão) — decidir e testar esse caso de borda.
- [ ] `go test ./internal/scanner/... -count=1`: `files_seen` sobe a cada
      arquivo processado (teste com fixture de N arquivos e assert de
      progresso intermediário, não só do resultado final).
- [ ] `go test ./internal/job/... -count=1` verde sem mudança de
      comportamento observável (mesmos testes existentes, adaptados ao
      novo backend de armazenamento).
- [ ] `GET /api/v1/jobs/{id}/executions` e `GET /api/v1/library/{id}/scan`
      continuam respondendo o mesmo shape JSON de antes (teste de
      contrato/regressão).
- [ ] Validação manual em produção (Batocera): disparar um scan real da
      pasta de música e confirmar que `files_seen` sobe de forma visível
      durante a execução (não fica em 0 até o fim).
- [ ] `go build ./...`, `go vet ./...`, `go test ./... -count=1` verdes.
- [ ] `make gate-local` limpo antes do commit final.

## 6. Notas de implementação / decisões

- Concorrência global = 1 (worker único) é decisão da ADR-0028, não decisão
  de implementação — não reabrir essa discussão aqui.
- `job_execution` é removida (não mantida em paralelo) — só existe um
  consumidor (a própria API), então não há custo de migração de dado
  externo.
- Ao mover o reporte de progresso para "por arquivo", cuidado para não
  aumentar o custo de I/O do banco: o progresso pode ser mantido em memória
  no `execqueue.Manager` (como já era no scanner) e só persistido
  periodicamente ou no fim — persistência é para sobreviver a restart do
  *histórico*, não necessariamente de cada incremento de progresso
  intermediário. Documentar a escolha feita.

## 7. Fora deste briefing / follow-ups

- Workers paralelos por categoria (scan e jobs em lanes separadas) — ADR-0028
  já registra como evolução futura.
- UI de fila detalhada (ver todos os itens enfileirados, não só o
  running) — avaliar depois que 0109-0111 (frontend) estiverem no ar.
- Implementado em 3 camadas (subagentes sequenciais/paralelos): camada 1
  (`internal/execqueue` + migration `queue_item`, `job_execution` mantida em
  paralelo), camada 2 (scanner vira `execqueue.Runner`, progresso por
  arquivo), camada 3 (job.Manager vira `execqueue.Runner` via adaptador
  `jobRunner`, `job_execution` finalmente dropada em migration própria de
  encerramento, wiring final em `cmd/mymediadb/main.go`).
- **Achado na validação (camada 3)**: `job.Handler.saude_biblioteca`
  disparava um scan via `Start()`+polling de `Status()` — com scan e job no
  MESMO worker sequencial (concorrência 1), isso deadlockava (o job, já
  rodando dentro do worker, esperava um item de scan que o próprio worker
  nunca chegaria a pegar). Corrigido: `libraryhealth.ScanRunner` agora chama
  `scanner.Manager.Executar` diretamente (o método que o worker chama), sem
  passar pela fila — roda o scan inline, no mesmo goroutine do job.
- Validação em produção (Batocera) do `files_seen` subindo em tempo real
  durante scan de música, adiada para a rodada de deploy conjunta com
  0109-0111 (ver notas dos demais briefings).
