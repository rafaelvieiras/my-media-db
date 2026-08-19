# ADR-0028 — Fila de execução unificada para scan e jobs

- **Status:** Aceita
- **Data:** 2026-08-19
- **Agenda:** — · **Spec:** [06-biblioteca-e-scan](../06-biblioteca-e-scan.md)

## Contexto

`internal/scanner.Manager` e `internal/job.Manager` são dois sistemas paralelos
que reimplementam o mesmo padrão — goroutine solta por execução, estado em
mapa protegido por mutex — de forma independente e redundante:

- Scan não tem histórico em banco (estado vive só em memória, some no
  restart) nem limite de concorrência entre `library_source`s diferentes:
  cada `POST /library/{id}/scan` bem-sucedido dispara sua própria goroutine,
  sem fila nem cap algum (documentado como lacuna conhecida no próprio
  `internal/scanner/scan.go:9-11`).
- Jobs têm histórico persistido (`job_execution`) e cron embutido, mas jobs
  *diferentes* entre si também rodam livremente em paralelo — nada impede
  `backup_vacuum` e `saude_biblioteca` simultâneos competindo pela única
  conexão de escrita do SQLite (`internal/db/db.go`, `Write.SetMaxOpenConns(1)`,
  ADR-0010).
- O contador de progresso do scan (`files_seen`/`files_total`) incrementa por
  lote de 25 arquivos (`loteEscrita`), não por arquivo — em `content_type=music`,
  onde cada arquivo pode custar múltiplas chamadas de rede ao MusicBrainz a
  1 req/s, isso faz o contador parecer travado em `0` por minutos reais mesmo
  com entidades sendo criadas de verdade (achado na validação em produção do
  briefing 0107).

Rodar tudo isso "solto" já sobrevive hoje só porque o volume é baixo; a
adição da vertical de música (0105-0107) e o pedido de granularidade real de
progresso tornam a duplicação e a ausência de limite de concorrência um
problema de manutenção e de previsibilidade de recursos (I/O, ffprobe,
chamadas a provider) que vale corrigir na raiz em vez de remendar os dois
sistemas separadamente de novo.

## Decisão

Unificar scan e jobs sob **uma única fila de execução em background**,
persistida em banco, com **um worker global sequencial** (concorrência 1): a
fila sempre mantém no máximo uma execução em andamento e, assim que ela
termina, imediatamente inicia a próxima item enfileirado (nunca fica ociosa
enquanto há trabalho pendente) — é essa a leitura operacional de "sempre
precisa ter uma execução aberta".

- Novo pacote `internal/execqueue` com uma tabela `queue_item` (`id`, `kind`
  — `'scan'` | `'job'` —, `ref_id` — `library_source.id` ou
  `job_definition.id` —, `trigger` — `manual` | `schedule` | `watch` —,
  `status` — `queued` | `running` | `success` | `error` —, `progress_current`,
  `progress_total`, `message`, `enqueued_at`, `started_at`, `finished_at`).
  Substitui `job_execution` (migração de dados: linhas existentes viram
  `kind='job'`) — a tabela antiga não sobrevive como uma segunda fonte de
  verdade.
- Enfileirar não executa: `Enqueue(kind, refID, trigger)` grava a linha
  `queued` e retorna. Um único goroutine de worker, iniciado no boot
  (`cmd/mymediadb/main.go`), consome a fila em FIFO por `enqueued_at`,
  delega a execução real ao `scanner` (mesma lógica de
  `coletarArquivos`/`processarLotes`/`computarFacts`, só a orquenstração
  muda) ou ao `job.Handler` já registrado (contrato `Handler.Executar`
  inalterado), e atualiza `progress_current`/`message` via callback durante a
  execução — não só no fim.
- **Dedup por referência, não por fila inteira**: duas entradas para o mesmo
  `(kind, ref_id)` com uma já `queued`/`running` são rejeitadas
  (`409`, mesmo contrato de erro que `ErrScanEmAndamento`/`ErrJobEmExecucao`
  hoje) — evita enfileirar o mesmo scan/job duas vezes, mas **não** impede
  itens de referências diferentes de coexistirem na fila (só não rodam ao
  mesmo tempo).
- Progresso de scan passa a incrementar por **arquivo processado**, não por
  lote de 25 — o lote de 25 continua existindo só como granularidade de
  escrita em banco (`processarLotes`), progresso e escrita são preocupações
  separadas agora.
- API pública não muda de forma observável: `POST /library/{id}/scan`,
  `GET .../scan`, `GET/POST /jobs*` continuam existindo com o mesmo contrato
  de resposta — por baixo, os handlers passam a chamar `execqueue.Enqueue`
  em vez de `scanner.Manager.Start`/`job.Manager.Executar` diretamente.
  `saude_biblioteca` (que hoje chama `scanner.Manager.Start` internamente
  para cada fonte) passa a enfileirar cada fonte e aguardar via polling do
  próprio `execqueue`, igual já faz hoje contra o `scanner.Manager`.

## Consequências

- Um único lugar (`internal/execqueue`) concentra concorrência, histórico e
  progresso — elimina a duplicação "goroutine + mapa + mutex" que existia
  duas vezes.
- Histórico de scan passa a sobreviver a restart (ganho novo — hoje não
  existe) e é consultável pela mesma superfície que já lista execuções de
  job.
- Execução serializada globalmente (concorrência 1) é uma escolha
  deliberada de simplicidade — casa com o princípio "local-first e leve"
  (AGENTS.md item 4) e com o fato de o SQLite já ter um único escritor; o
  custo é que um scan longo (ex.: biblioteca de música grande, rate-limited)
  atrasa jobs agendados atrás dele na fila. Aceito como MVP; evolução para
  N workers com fila por categoria (scan não bloqueia backup, por exemplo)
  fica para quando o volume real justificar.
- `job_execution` deixa de existir como tabela própria; qualquer consumidor
  (API, testes) passa a ler `queue_item` filtrado por `kind='job'`.
- Não migra `library_source.state`/`last_scan` (continuam como estão) nem
  muda o modelo de dados de música/scan definido em ADR-0027 — é
  puramente uma mudança de orquestração de execução.
