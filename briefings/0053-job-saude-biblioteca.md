# 0053 — Job de saúde da biblioteca: re-scan de todas as fontes + resumo de revisão/órfãs

> Follow-up explícito do [0052](0052-fundacao-sistema-de-jobs.md): um novo
> `Handler` (`saude_biblioteca`) que re-escaneia toda `library_source`
> cadastrada e resume, ao final, quantos itens novos caíram na fila de
> revisão e quantos ficaram órfãos — fechando a lacuna do [0018](0018-fila-revisao-orfas-correcao-match.md)
> de que essas filas só refletem a realidade do disco depois de um scan
> manual.

| | |
|---|---|
| **ID** | 0053 |
| **Tipo** | feature |
| **Fase (roadmap)** | **Transversal / infraestrutura operacional** — mesma seção do [0052](0052-fundacao-sistema-de-jobs.md); é o segundo `Handler` do sistema de jobs, não uma tela de biblioteca. |
| **Docs relacionadas** | [03-arquitetura](../docs/03-arquitetura.md), [ADR-0005](../docs/adr/0005-composicao-uniforme-extension-data.md), [ADR-0010](../docs/adr/0010-concorrencia-sqlite-wal-escritor-unico.md), [ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md) |
| **Status** | done |
| **Depende de** | [0052](0052-fundacao-sistema-de-jobs.md), [0018](0018-fila-revisao-orfas-correcao-match.md) |

---

## 1. Objetivo

O [0052](0052-fundacao-sistema-de-jobs.md) entregou a fundação do sistema de
jobs (schema genérico, `Handler`/`Registry`/`Manager`, agendador embutido,
endpoints `/jobs*`, tela de administração) e citou explicitamente, como
FUTURO e SEPARADO, o **job de saúde da biblioteca**. Este briefing entrega
esse job.

O motivo concreto: as filas de curadoria do [0018](0018-fila-revisao-orfas-correcao-match.md)
(`GET /review`, `GET /orphans`) **não são tabelas — são QUERY ao vivo** sobre
`entity`/`media_file`. Elas só refletem a realidade do disco no instante em
que **alguém rodou um scan manual** daquela `library_source`. Se um arquivo
some do disco e ninguém dispara `POST /library/{id}/scan` de novo, a entity
correspondente continua com `media_file.state='present'` **para sempre** — a
órfã nunca aparece, e o dono não tem como saber que perdeu um arquivo sem
entrar manualmente em cada fonte e re-escanear.

O job `saude_biblioteca` fecha essa lacuna: agendado (ex. de madrugada), ele
re-escaneia **todas** as fontes cadastradas sozinho, sem o dono precisar
lembrar de nenhuma delas, e resume o estado das filas ao final — o dono abre
a tela de Jobs de manhã e já sabe "rodou, achou 3 novos itens em revisão e 1
órfã", sem precisar entrar em cada datasource.

## 2. Jornada do usuário

1. Depois de instalar esta versão, o dono abre a tela **Jobs** (já existente,
   [0052](0052-fundacao-sistema-de-jobs.md)) e vê um **segundo** card: *"Saúde
   da biblioteca (re-scan + revisão/órfãs)"* — desabilitado por padrão,
   agendado para `0 4 * * *` (4h da manhã, uma hora depois do backup).
2. Ele habilita o toggle (ou clica **"Executar agora"** para testar na hora).
3. O card muda para "rodando…" enquanto o job re-escaneia, em sequência, cada
   fonte cadastrada (mesmo comportamento de `POST /library/{id}/scan`, só que
   disparado pelo job em vez do dono clicando em cada uma).
4. Ao terminar, o card mostra o resultado, ex.: *"3 fonte(s) escaneada(s) com
   sucesso; 5 em revisão, 2 órfã(s)"*. Se uma fonte estava offline (HD
   externo desligado, por exemplo) mas as outras rodaram: *"2 de 3 fonte(s)
   escaneada(s) com sucesso (1 falharam: /mnt/filmes: datasource inacessível
   (offline)); 5 em revisão, 2 órfã(s)"* — o job não trava por causa de UMA
   fonte ruim.
5. Se **todas** as fontes falharem (ex.: o dono desmontou tudo por engano), o
   job aparece como **erro** no histórico, com a lista de motivos — sinal
   claro de que algo está errado, em vez de um "sucesso" mentiroso.
6. Com o agendamento ligado, isso roda sozinho toda madrugada; o dono só
   precisa abrir `/review`/`/orphans` quando quiser agir sobre o que o
   resumo indicou.

## 3. Escopo

**Dentro:**
- Novo `Handler` `internal/job/libraryhealth`, `Nome() = "saude_biblioteca"`:
  lista as `library_source`, dispara e **aguarda** o re-scan de cada uma
  (reusando `scanner.Manager.Start`/`Status`, sem duplicar lógica de scan),
  best-effort entre fontes, e resume com as contagens atuais de
  revisão/órfãs ao final.
- Novo pacote `internal/curation`: extrai das queries sqlc já existentes
  (`ListarRevisao`/`ListarOrfas`, [0018](0018-fila-revisao-orfas-correcao-match.md))
  duas funções de **contagem** (`ContarRevisao`/`ContarOrfas`) reusáveis fora
  de `internal/server` — sem reimplementar nenhum predicado SQL, só
  reaproveitando a MESMA query e contando as linhas.
- Migration nova (`0012_seed_saude_biblioteca.sql`): **só semeia** a linha de
  `job_definition` do novo job — o schema de `job_definition`/`job_execution`
  do 0052 já é genérico o bastante (prova o critério de aceite do 0052 de que
  um novo tipo de job não exige alterar o schema).
- Wiring: nova `Option` `server.WithLibraryHealthJob()` (registrada em
  `cmd/mymediadb/main.go`) — ver §6 para por que não é literalmente
  `WithJobHandlers(libraryhealth.New(...))` como o backup.
- Testes unitários do `Handler` com fakes de `library`/`scanner`/`curation` (sem
  tocar disco/banco real) + testes do `internal/curation` com banco migrado
  real + testes de wiring em `internal/server`.

**Fora (explicitamente):**
- **Qualquer ação automática sobre o resultado.** O job só **relata** — não
  purga órfãs, não confirma/corrige itens de baixa confiança. Purga continua
  sendo SEMPRE explícita, do dono (ADR-0012); este job não é uma exceção.
- **Notificação externa do resultado** (e-mail/webhook) — já era fora do 0052
  e continua fora aqui; o dono lê o card/histórico na tela de Jobs.
- **Paralelismo entre fontes.** Os re-scans rodam em **sequência** (uma fonte
  de cada vez) — ver §6 para a justificativa.
- **Parâmetro de configuração real em `config_json`** — o job não tem
  nenhum hoje; `config_json='{}'` é só um placeholder namespaced (ADR-0005).
- **Mudar o schema de `job_definition`/`job_execution`** — critério de aceite
  do 0052 é justamente que isso NÃO seja necessário.
- **Alterar a tela `/jobs` do frontend** — ela já é genérica (lista qualquer
  `job_definition`); o card do novo job aparece sozinho, sem nenhuma mudança
  de UI.

## 4. Contrato técnico

### Dados / modelo (migration `0012_seed_saude_biblioteca.sql`)

Sem alteração de schema — só um `INSERT` em `job_definition`:

```sql
INSERT INTO job_definition (id, nome, descricao, enabled, schedule_cron, config_json)
VALUES (
    'saude_biblioteca',
    'Saúde da biblioteca (re-scan + revisão/órfãs)',
    'Re-escaneia todas as fontes cadastradas e resume quantos itens novos caíram na fila de revisão e quantos ficaram órfãos.',
    0,
    '0 4 * * *',
    '{}'
);
```

`Down` reverte com `DELETE FROM job_definition WHERE id = 'saude_biblioteca'`.

### Pacote Go `internal/curation`

- `curation.go`: `Servico` construído com `*db.DB` (mesmo padrão de
  `library.New(database)`); `LimiarBaixaConfianca = 70` (mirror documentado
  do `limiarBaixaConfianca` privado de `internal/server/review.go` e do
  `LIMIAR_BAIXA_CONFIANCA` do front — acoplamento já reconhecido no
  [0018](0018-fila-revisao-orfas-correcao-match.md) §6, não uma duplicação
  de lógica nova); `ContarRevisao(ctx)`/`ContarOrfas(ctx)` chamam
  `sqlcgen.ListarRevisao`/`ListarOrfas` (as MESMAS queries do `GET
  /review`/`GET /orphans`) e devolvem `len(rows)`.

### Pacote Go `internal/job/libraryhealth`

- `libraryhealth.go`: `Handler{ library LibraryLister; scanner ScanRunner;
  contador Contador }`, três interfaces pequenas (satisfeitas
  estruturalmente por `*library.Service`, `*scanner.Manager` e
  `*curation.Servico`, sem precisar declará-las nesses pacotes) para permitir
  fakes determinísticos em teste.
- `Nome() = "saude_biblioteca"`.
- `Executar(ctx, cfg)`:
  1. `library.Listar(ctx)` — erro aqui aborta tudo (nada para escanear).
  2. Para cada fonte, **sequencialmente**: `scanner.Start(source)` e então
     `aguardarConclusao` (polling de `scanner.Status(source.ID)` a cada
     `pollInterval=200ms` até `Running=false`, checando `ctx.Done()` a cada
     volta). Falha de uma fonte (`Start` retornou erro, ex.
     `ErrScanEmAndamento`/`ErrSourceOffline`, ou o scan terminou com
     `LastError`) vira uma entrada em `falhas`, sem abortar as demais.
  3. Se havia ao menos uma fonte e **todas** falharam, devolve erro
     (mensagem agregando os motivos) — não mente dizendo "sucesso" quando
     nada funcionou.
  4. Conta `curation.ContarRevisao`/`ContarOrfas` e monta a mensagem final:
     `"<N> fonte(s) escaneada(s) com sucesso[ (<K> falharam: ...)]; <R> em
     revisão, <O> órfã(s)"` (ou `"nenhuma fonte de biblioteca cadastrada; ..."`
     se não há nenhuma fonte).

### Backend (Go) — wiring

- `internal/server/server.go`: nova `Option WithLibraryHealthJob()` que
  constrói `libraryhealth.New(s.library, s.scanner, curation.New(s.db))` e
  registra no `Registry` de `s.jobs` — **dentro** do fechamento da option
  (que roda depois de `New` já ter montado `s.library`/`s.scanner`, mesmo
  pré-requisito que `WithJobHandlers` já assume para `s.jobs`). Ver §6 para
  por que não é `WithJobHandlers(libraryhealth.New(...))` direto em
  `main.go`.
- `cmd/mymediadb/main.go`: `opcoesServer` adiciona
  `server.WithLibraryHealthJob()` à lista de options, junto do
  `WithJobHandlers(backup.New(...))` já existente.

### Arquivos afetados

- Criar: `internal/curation/curation.go` (+ `curation_test.go`),
  `internal/job/libraryhealth/libraryhealth.go` (+
  `libraryhealth_test.go`), `internal/db/migrations/0012_seed_saude_biblioteca.sql`,
  `internal/server/library_health_job_test.go`.
- Alterar: `internal/server/server.go` (`WithLibraryHealthJob`),
  `cmd/mymediadb/main.go` (`opcoesServer`), `internal/server/jobs_test.go`
  (`TestListJobsSeed` agora espera os DOIS jobs semeados).

## 5. Critérios de aceite

- [x] `GET /api/v1/jobs` lista `saude_biblioteca` além de `backup_vacuum`,
  desabilitado, agendado para `0 4 * * *`, sem execuções ainda.
- [x] Com `server.WithLibraryHealthJob()` aplicada, `POST
  /api/v1/jobs/saude_biblioteca/run` dispara e termina com sucesso mesmo sem
  nenhuma `library_source` cadastrada (mensagem "nenhuma fonte de biblioteca
  cadastrada; N em revisão, M órfã(s)"). Sem a option, o mesmo `POST` devolve
  `503` (job seedado, sem `Handler` registrado) — mesma semântica do 0052
  para `backup_vacuum` sem `WithJobHandlers`.
- [x] O `Handler` **espera de fato** cada scan disparado terminar antes de
  contar as filas — provado em teste com um `ScanRunner` fake cujo "scan"
  só marca `Running=false` depois de um delay assíncrono; o `Executar` só
  retorna depois desse delay.
- [x] Falha de UMA fonte (offline, scan em andamento, erro no meio do scan)
  não aborta as demais; a mensagem final relata quantas tiveram sucesso e o
  motivo de cada falha.
- [x] Se **todas** as fontes falham, `Executar` devolve erro (não um
  "sucesso" enganoso).
- [x] `internal/curation.ContarRevisao`/`ContarOrfas` devolvem exatamente a
  mesma contagem que `GET /review`/`GET /orphans` devolveriam (mesmo
  predicado, mesma query sqlc) — provado com fixtures que cobrem draft,
  baixa confiança, alta confiança (exclusão) e órfã.
- [x] Adicionar este job não exigiu NENHUMA alteração em
  `job_definition`/`job_execution` nem nos endpoints `/jobs*` — só a
  migration de seed + o `Handler` (prova o critério de extensibilidade do
  0052).
- [x] `go build/vet/test -count=1` verdes; `tsc -b` do web verde (nenhum
  arquivo de frontend tocado — a tela `/jobs` já é genérica); catraca local
  (`make gate-local`) não piora.

## 6. Notas de implementação / decisões

- **`WithLibraryHealthJob()` em vez de `WithJobHandlers(libraryhealth.New(...))`
  em `main.go`.** O job `backup_vacuum` só precisa de `*sql.DB` e um
  diretório — ambos resolvidos em `main.go` sem depender de nada interno ao
  `Server`, então `backup.New(...)` pode ser construído lá e passado pronto
  para `WithJobHandlers`. Já `saude_biblioteca` precisa do `scanner.Manager`
  e do `library.Service`, que são campos **privados** do `Server`,
  construídos dentro de `server.New` **antes** do loop de `options` (mesmo
  jeito que `WithJobHandlers` já depende de `s.jobs` poder ser construído
  lá dentro). Duas saídas possíveis: (a) expor `scanner.Manager`/
  `library.Service` para fora do `Server` e reconstruí-los em `main.go` só
  para montar o `Handler` — duplicaria a fiação e quebraria o encapsulamento
  que o `Server` já tem hoje; (b) uma `Option` dedicada cujo `func(*Server)`
  roda **depois** que `s.library`/`s.scanner` já existem, construindo o
  `Handler` ali dentro. Escolhida (b): `main.go` continua só chamando
  `server.WithLibraryHealthJob()`, sem precisar saber COMO o `Handler` é
  montado por dentro — mesmo espírito de `WithJobHandlers` já reusar `s.jobs`
  se ele existir.
- **Scan sequencial, não paralelo.** O escritor único do SQLite (ADR-0010) já
  serializa a escrita de qualquer jeito, e cada scan em si já processa em
  lotes; rodar N scans em paralelo só adicionaria contenção na mesma conexão
  de escrita sem ganho real de throughput, e complicaria a lógica de
  "aguardar todos terminarem" (teria que ser um `sync.WaitGroup` em vez de um
  loop simples). Sequencial é mais simples, mais previsível em log, e
  suficiente para o volume esperado (biblioteca pessoal, poucas fontes).
- **Sem timeout próprio no polling de `aguardarConclusao`.** O loop só sai
  quando o scan termina OU `ctx` é cancelado. Isso espelha uma limitação já
  existente em TODO o sistema de jobs (0052): `Manager.executar` roda
  `Handler.Executar` sempre em `context.Background()` (deliberado — o
  trabalho não deve abortar só porque a requisição HTTP que disparou
  terminou), então nenhum `Handler` hoje tem um prazo próprio. Adicionar um
  timeout só para este job seria inconsistente com o resto; se aparecer a
  necessidade real (ex. um scan que trava um HD montado via rede), o lugar
  certo é resolver no nível do `Manager`/`scanner`, não aqui.
- **`Start()` sempre cria um job NOVO no `scanner.Manager`.** Verificado no
  código (`internal/scanner/manager.go`): cada chamada a `Start` substitui a
  entrada do mapa por uma `*job` nova (com `lastError` zerado por
  zero-value), então `scanner.Status(sourceID).LastError` lido depois do
  polling deste `Handler` NUNCA é resíduo de um scan anterior — é sempre o
  resultado da execução que ESTE `Handler` acabou de disparar. Documentado
  aqui porque não é óbvio à primeira vista e foi verificado antes de confiar
  nele para decidir sucesso/falha por fonte.
- **`LimiarBaixaConfianca` duplicado como constante, não extraído de
  `internal/server`.** `internal/server/review.go` mantém seu
  `limiarBaixaConfianca` privado (usado também por `automatch.go`) sem
  alteração, para não aumentar o raio de mudança deste briefing.
  `internal/curation.LimiarBaixaConfianca` é o mesmo valor, documentado como
  mirror — o MESMO padrão de acoplamento já aceito entre back e front
  (`LIMIAR_BAIXA_CONFIANCA` em `web/src/lib/entity-badges.ts`) desde o 0018.
  Extrair um único símbolo compartilhado entre `internal/server` e
  `internal/curation` é uma limpeza futura de baixo risco, não feita aqui
  para manter o diff focado no job novo.
- **`config_json='{}'` no seed.** O job não tem nenhum parâmetro real hoje
  (paralelismo é fixo em sequencial, ver acima). O objeto vazio só marca a
  convenção (ADR-0005: `config_json` namespaced por tipo) para o dia em que
  aparecer um parâmetro de verdade — não é usado por `Handler.Executar`
  (`cfg` é ignorado).
- **`schedule_cron='0 4 * * *'`, uma hora depois do backup (`0 3 * * *`).**
  Evita concorrência de I/O pesado (VACUUM INTO + varredura de árvore de
  arquivos) no mesmo instante. Uma hora de folga é confortável para o porte
  de biblioteca doméstica que o projeto assume (ver
  [deploy.md](../docs/deploy.md), alvo Raspberry Pi 3B).
- **`enabled=0` por padrão**, mesmo espírito do `backup_vacuum`: uma ação
  automática que gera I/O relevante em disco (ler toda a árvore de cada
  fonte) não deveria começar sozinha sem o dono revisar o agendamento
  primeiro, especialmente num Pi com cartão SD.
- **Este job nunca purga.** Ele só **relata** contagens; a purga (única porta
  destrutiva, ADR-0012) continua sendo ação explícita do dono via
  `DELETE /entities/{id}` ou `/entities/purge` — nenhuma automação deste job
  toca nisso, por design.

## 7. Fora deste briefing / follow-ups

- **Notificação do resultado** (e-mail/webhook quando o job encontra
  novidades relevantes) — mesmo fora do 0052, cogitado, sem caso de uso
  concreto ainda.
- **Ação automática sobre o resultado** (ex.: auto-purgar órfãs muito
  antigas) — contraria ADR-0012 (purga é sempre explícita do dono); não
  cogitado.
- **Paralelismo configurável entre fontes** — só se o volume de fontes de
  algum dono realmente justificar (não é o caso hoje).
- **Extrair `LimiarBaixaConfianca` para um único símbolo compartilhado**
  entre `internal/server` e `internal/curation` (hoje mirror documentado) —
  limpeza de baixo risco, não feita aqui para manter o diff focado.
