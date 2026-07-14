# 0052 — Fundação do sistema de jobs: registro dinâmico + agendador embutido

> Base para rodar tarefas periódicas dentro do próprio binário: tipos de job
> registrados em código (handler), configuração e histórico no banco, usuário
> habilita/desabilita/agenda/dispara manualmente. Primeiro job semeado: backup
> via `VACUUM INTO`. É a fundação explícita do futuro sistema de plugins.

| | |
|---|---|
| **ID** | 0052 |
| **Tipo** | feature |
| **Fase (roadmap)** | **Transversal / infraestrutura operacional** — não prevista explicitamente no [roadmap](../docs/12-roadmap.md). Ver §0 abaixo para a justificativa de não encaixar em Fase 1-4. |
| **Docs relacionadas** | [03-arquitetura](../docs/03-arquitetura.md), [04-modelo-de-dados](../docs/04-modelo-de-dados.md) (esboço `plugin_registry`), [11-decisoes-abertas](../docs/11-decisoes-abertas.md) (§ sistema de plugins), [ADR-0005](../docs/adr/0005-composicao-uniforme-extension-data.md), [ADR-0010](../docs/adr/0010-concorrencia-sqlite-wal-escritor-unico.md), [ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md), [deploy.md](../docs/deploy.md) |
| **Status** | done |
| **Depende de** | — |

---

## 0. Por que "transversal" e não uma fase existente

O roadmap é **UI-first** e quebrado em fases que entregam telas de biblioteca
(Fase 1-4) e, só na **Fase 5+**, cogita o "maquinário de plugins" ([11](../docs/11-decisoes-abertas.md)).
Este briefing não é UI de biblioteca nem o maquinário de plugins em si — é a
**infraestrutura de execução periódica** que tanto resolve uma dor operacional
de HOJE (backup automatizado sem depender de cron do host) quanto é,
explicitamente, o alicerce técnico sobre o qual o futuro registro de plugins
vai se apoiar (o próprio [04-modelo-de-dados](../docs/04-modelo-de-dados.md) já
lista `plugin_registry` no esboço "fase posterior"). Por não pertencer a
nenhuma fase de UI e por anteceder o maquinário de plugins descrito na Fase
5+, ele fica registrado como uma seção própria no índice de briefings, e pode
ser implementado a qualquer momento sem esperar o resto da Fase 5+.

## 1. Objetivo

Hoje o MyMediaDB não tem NENHUM mecanismo de execução periódica dentro do
binário. Duas dores concretas motivam isto:

1. **Backup.** `scripts/backup.sh` ([`3cb3c57`](../compose.prod.yaml)) faz um
   `VACUUM INTO` consistente, mas depende de ser chamado de FORA do container
   — hoje só por invocação manual ou por um `crontab` no HOST (o próprio
   comentário do script sugere `crontab -e`). O alvo final de produção é um
   Raspberry Pi 3B ("moon") rodando só o container via Watchtower — não há
   garantia de que o dono vá manter um cron de host configurado, e a aplicação
   não deveria depender disso para uma rotina básica de segurança dos dados.
2. **Produto.** O dono pediu explicitamente uma base para "jobs" — itens
   executados com frequência, **dinâmicos** (config no banco, não hardcoded),
   que ele possa habilitar/desabilitar, agendar e disparar manualmente
   ("dar um play"). E que sirva de **base para o sistema de plugins** no
   futuro.

Este briefing entrega **só a fundação**: o schema genérico, o padrão de
registro de tipo de job em Go (espelhando o `provider.Registry` já existente),
o agendador embutido (goroutine + ticker, sem cron do SO), os endpoints REST e
a tela de administração. Como prova de que o sistema funciona de ponta a
ponta, semeia **um** job real via migration: o backup por `VACUUM INTO`.

O job de **saúde da biblioteca** (encontrar arquivos órfãos/entidades sem
arquivo e jogar para a fila de revisão) citado no pedido original é
**FUTURO e SEPARADO** — depende desta fundação, mas não é desenhado aqui (§3).

## 2. Jornada do usuário

1. O dono abre uma nova tela **Jobs** (link no `AppHeader`, ao lado de
   Revisão/Órfãs).
2. Vê uma lista de jobs conhecidos pelo sistema. Ao ligar pela primeira vez
   após esta fundação, aparece **um**: *"Backup do banco (VACUUM INTO)"* —
   desabilitado por padrão, agendado para `0 3 * * *` (3h da manhã, todo dia).
3. Em cada card ele vê: nome, descrição curta, um **toggle habilitado/
   desabilitado**, o **agendamento** (expressão cron, editável), a **última
   execução** (quando, sucesso/erro, mensagem) e a **próxima execução prevista**
   (só quando habilitado e com agendamento).
4. Pode clicar **"Executar agora"** a qualquer momento — habilitado ou não —
   e ver o card mudar para "rodando…" e, ao terminar, o resultado (sucesso com
   a mensagem "backup gravado em mymediadb-20260713-030000.sqlite (7
   mantidos)", ou erro com a mensagem curta da falha).
5. Pode abrir o **histórico** do job (lista das últimas execuções, mais
   recente primeiro, com carimbo, gatilho — agendado ou manual —, status e
   mensagem) para auditar o que rodou.
6. Pode editar o agendamento (nova expressão cron) ou desligar o agendamento
   por completo (deixando o job só-manual) sem perder o histórico.
7. Se o container reiniciar (deploy automático via Watchtower) o agendamento
   sobrevive — está no banco, não em memória. Se o binário ficou fora do ar na
   hora agendada, o job roda **uma vez** no próximo ciclo do agendador após
   subir (sem empilhar execuções perdidas) e realinha para o próximo horário
   futuro.

## 3. Escopo

**Dentro:**
- Schema novo: `job_definition` (config dinâmica: habilitado, agendamento,
  última execução, próxima execução) e `job_execution` (histórico genérico,
  válido para QUALQUER tipo de job).
- Pacote `internal/job`: interface `Handler` (o tipo de job REGISTRADO em
  código Go, análogo a `provider.Provider`), `Registry` (análogo a
  `internal/provider/registry.go`) e `Manager` (análogo a `scanner.Manager`):
  guarda estado em memória de execuções em andamento, expõe execução manual e
  roda o **agendador embutido** (goroutine com ticker).
- **Um** job semeado via migration: `backup_vacuum` — porta a lógica de
  `scripts/backup.sh` (VACUUM INTO + rotação) para um `Handler` Go,
  registrado no boot. Desabilitado por padrão (§6).
- Endpoints REST: listar jobs, editar habilitado/agendamento, disparar
  execução manual, listar histórico de execuções.
- Tela `/jobs` no frontend: lista, toggle, editor de agendamento, botão
  "Executar agora", histórico.
- O desenho é **genérico o bastante** para o futuro job de saúde da
  biblioteca (§7) só precisar registrar um novo `Handler` — sem tocar o
  schema deste briefing. Não especificamos esse job aqui, só garantimos que
  `Handler.Executar(ctx, cfg)` tem acesso ao banco (via injeção na construção,
  como `enrich.Servico`/`scanner.Manager` já fazem) para, no futuro, escrever
  na fila de revisão/órfãs existente ([0018](0018-fila-revisao-orfas-correcao-match.md)).

**Fora (explicitamente):**
- **O job de saúde da biblioteca** (órfãos, entidades sem arquivo → fila de
  revisão com motivo) — briefing futuro e separado, que DEPENDE deste.
- **Maquinário de plugins de terceiros** (manifest, instalação/desinstalação,
  sandbox, widgets de frontend dinâmicos) — segue pós-MVP ([11](../docs/11-decisoes-abertas.md)).
  Este briefing só prepara o **padrão de registro**, não o carregamento
  dinâmico de plugins externos.
- Múltiplos agendamentos por job, dependência entre jobs (job B só depois de
  A), fila com prioridade/retry automático/backoff, execução distribuída
  (a aplicação é sempre um único binário/container — [03](../docs/03-arquitetura.md)).
- Notificação de falha (e-mail/webhook) — fica como follow-up (§7).
- Internacionalização de nome/descrição do job — texto simples por ora, como
  os demais textos administrativos do sistema.

## 4. Contrato técnico

### Dados / modelo (migration nova, ex. `0011_jobs.sql`)

```
job_definition   (id, nome, descricao, enabled, schedule_cron,
                  config_json, next_run_at, last_run_at, last_status,
                  last_message, created_at, updated_at)
                  -- id: SLUG ESTÁVEL (a chave do Handler.Nome(), ex.
                  --   "backup_vacuum"), NÃO um uuid — como app_settings.key,
                  --   porque preexiste ao registro (migration/seed) e o Go
                  --   precisa achá-lo por NOME, não por id gerado.
                  -- enabled: DEFAULT 0, CHECK IN (0,1) — só gate do
                  --   agendamento automático; execução manual funciona
                  --   independente deste flag.
                  -- schedule_cron: TEXT NULO. NULO = job só-manual (sem
                  --   agendamento). Expressão cron padrão de 5 campos.
                  -- config_json: TEXT NULO, blob JSON NAMESPACED por tipo de
                  --   job (ex. {"keep": 7} do backup) — mesmo princípio do
                  --   extension_data (ADR-0005): o núcleo não conhece o
                  --   formato, cada Handler interpreta o seu.
                  -- next_run_at: TEXT NULO, recalculado a cada mudança de
                  --   enabled/schedule_cron e ao fim de cada execução; NULO
                  --   quando desabilitado ou sem schedule_cron.
                  -- last_run_at/last_status/last_message: espelham a
                  --   ÚLTIMA linha de job_execution (redundância de leitura
                  --   para a lista não precisar de JOIN); last_status CHECK
                  --   IN ('success','error') quando não-nulo.
job_execution    (id, job_id→job_definition, trigger, status,
                  started_at, finished_at, message, created_at)
                  -- id: uuid (como media_file.id/entity.id).
                  -- job_id: FK ON DELETE CASCADE (apagar a definição do job
                  --   — caso teórico, não há endpoint de apagar nesta
                  --   fundação — leva o histórico junto).
                  -- trigger: CHECK IN ('schedule','manual').
                  -- status: CHECK IN ('running','success','error').
                  -- message: TEXT NULO — resultado curto (sucesso) ou erro
                  --   (falha); é o QUE o Handler devolveu, sem stack trace.
                  -- SEM tabela por tipo de job: TODO handler grava aqui,
                  --   igual entity_relation serve TODO par de entities
                  --   (nunca uma junção por tipo).
```

Índice `idx_job_execution_job` em `(job_id, started_at DESC)` — histórico por
job, mais recente primeiro; também usado pela poda de retenção (abaixo).

**Retenção do histórico**: cap de **100 execuções por job**. A cada INSERT em
`job_execution`, uma query apaga o excedente mais antigo além das 100 mais
recentes DAQUELE `job_id` (mesmo espírito do `KEEP=7` do `backup.sh`, só que
por contagem de execuções, não de dias — mais previsível para um job que pode
rodar com frequência variável). Configurável como constante Go, não exposto
na API por ora.

**Seed** (mesma migration, ou uma seguinte): uma linha em `job_definition`
para `backup_vacuum`, `enabled=0`, `schedule_cron='0 3 * * *'`,
`config_json='{"keep":7}'` (espelha o default do `backup.sh`).

### Pacote Go `internal/job`

- `handler.go`: interface `Handler { Nome() string; Executar(ctx
  context.Context, cfg json.RawMessage) (mensagem string, err error) }` — o
  contrato que QUALQUER tipo de job implementa (o futuro job de saúde da
  biblioteca, o de plugins, etc.). `cfg` é o `config_json` bruto da definição
  (pode ser vazio); `mensagem` vira `job_execution.message` em sucesso.
- `registry.go`: `Registry` com `Registrar(Handler)`/`Get(nome)
  (Handler,bool)`/`All()`/`Nomes()` — cópia estrutural de
  `internal/provider/registry.go` (mesmo padrão: zero-value utilizável, ordem
  de registro preservada, último a registrar vence).
- `manager.go`: `Manager` guarda `db`, `Registry`, mapa em memória de
  `running map[string]bool` (por `job_id`, mutex — mesmo padrão de
  `scanner.Manager.jobs`). Expõe:
  - `Executar(ctx, jobID, trigger) (execID string, err error)` — dispara uma
    execução (goroutine): cria a linha `job_execution` (`status=running`),
    chama `handler.Executar`, atualiza a linha e `job_definition` ao fim
    (recalcula `next_run_at` se `enabled` e `schedule_cron` setados,
    independente do `trigger` — cron.Next(agora) sempre aponta pro próximo
    horário futuro, então uma execução manual nunca "pula" o próximo
    agendado). Erro `ErrJobEmExecucao` se já roda (mesmo padrão de
    `scanner.ErrScanEmAndamento`, mapeado a 409); `ErrJobDesconhecido` se o
    id não tem `Handler` registrado (mapeado a 503 — job seedado mas o
    binário não tem o handler, ex. downgrade).
  - `RunScheduler(ctx context.Context)` — loop com `time.Ticker` (constante
    `pollInterval = 30s`; a resolução mínima de cron é 1 minuto, 30s é
    suficiente pra nunca atrasar mais que isso). A cada tick: query
    `job_definition WHERE enabled=1 AND schedule_cron IS NOT NULL AND
    next_run_at <= now`; para cada uma não em `running`, chama `Executar(...,
    trigger="schedule")` (best-effort — erro de uma não impede as outras).
    Parado via `ctx.Done()` (o mesmo ctx do `signal.NotifyContext` do
    `main.go`).
- `internal/job/backup/backup.go` (novo): `Handler` do job semente. `Nome() =
  "backup_vacuum"`. `Executar` porta a lógica do `scripts/backup.sh` para Go:
  `VACUUM INTO` num caminho dentro do diretório de backups (`<dirname
  DB>/backups` por padrão, ou `MYMEDIADB_BACKUP_DIR` se setada — mesma
  convenção de env do `resolveAssetsDir`), nomeando
  `mymediadb-<timestamp>.sqlite`, e rotação mantendo os `keep` mais recentes
  (`cfg.keep`, default 7 se ausente/inválido). Devolve mensagem tipo "backup
  gravado em <arquivo> (N mantidos)".

### Decisão: formato de agendamento — expressão cron

Ver §6 para a justificativa completa. Resumo: **expressão cron de 5 campos**
(igual ao `crontab -e` já sugerido em `scripts/backup.sh`), calculada com um
parser cron pequeno e puro-Go (ex. `github.com/robfig/cron/v3`, usado
**só** para `cron.ParseStandard(expr).Next(t)` — NÃO usamos o scheduler
embutido dessa lib, o loop é nosso `Manager.RunScheduler`).

### API (`/api/v1`)

- **GET `/jobs`** — lista todos os `job_definition` com o estado atual
  (`enabled`, `schedule_cron`, `next_run_at`, `last_run_at`, `last_status`,
  `last_message`, e `running: bool` do estado em memória do Manager). `200`
  `{items: [...]}` (nunca vazio após esta fundação — sempre tem o seed).
- **PATCH `/jobs/{id}`** — edita config: corpo `{enabled?: bool,
  schedule_cron?: string|null}` (mesma convenção de patch parcial do
  `PATCH .../override`). Recalcula `next_run_at`. `200` com o recurso
  atualizado; `404` id desconhecido; `422` `schedule_cron` que não parseia
  como cron válido.
- **POST `/jobs/{id}/run`** — dispara execução manual (`trigger="manual"`),
  independente de `enabled`. `202` `{execution_id}`; `404` id desconhecido;
  `409` já em execução; `503` sem `Handler` registrado para o id (job
  seedado, binário sem o handler).
- **GET `/jobs/{id}/executions`** — histórico paginado (`?limit=` default 20,
  máx. 100; `?before=<started_at>` para paginar pra trás), mais recente
  primeiro. `200` `{items: [...]}`; `404` id desconhecido.

Envelope de erro padrão do projeto em tudo: `{ "error": { "code", "message"
} }`.

### Backend (Go) — wiring

- `internal/server/server.go`: novo campo `jobs *job.Manager`; nova `Option
  WithJobHandlers(handlers ...job.Handler)` (acumulativo, mesmo padrão de
  `WithSearchProvider`); construído em `montarServicos` sempre que `database
  != nil` (sem dependência de provider/assetsDir — diferente de
  `enrich`/`externalDetail`, o Manager de jobs não depende de fonte externa).
  Novo arquivo `internal/server/jobs.go` com os 4 handlers HTTP acima.
- `cmd/mymediadb/main.go`: registra `backup.New(...)` sob a option
  `WithJobHandlers`; chama `go srv.Jobs().RunScheduler(ctx)` (o mesmo `ctx`
  de `signal.NotifyContext` já usado pro shutdown do HTTP) ANTES de
  `ListenAndServe`, para o agendador parar de forma limpa junto do resto no
  `SIGINT`/`SIGTERM`.
- `openapi.yaml`: novos paths/schemas de `/jobs*`; regenerar tipos do
  frontend.

### Frontend (Vite/React)

- Rota `/jobs` (link no `AppHeader`, junto de Revisão/Órfãs).
- `JobCard`: nome, descrição, badge de status (habilitado/desabilitado,
  rodando agora), toggle (PATCH `enabled`), campo de agendamento (PATCH
  `schedule_cron`, com validação/feedback de erro 422), botão "Executar
  agora" (POST `/run`, loading enquanto `running`), última execução
  (sucesso/erro + mensagem + carimbo), próxima execução prevista.
- `JobHistoryList` (ou drawer/expansível no card): lista paginada de
  `GET .../executions` — carimbo, gatilho, status, mensagem.
- `api.ts`/`queries.ts`: `listJobs`, `patchJob`, `runJob`, `listJobExecutions`
  + hooks TanStack Query; invalidar `['jobs']`/`['job', id, 'executions']`
  após PATCH/run. Poll leve (ex. refetch a cada alguns segundos) enquanto
  algum job está `running`, para o card sair de "rodando…" sem o dono
  recarregar a página manualmente.

### Arquivos afetados (previsão)

- Criar: `internal/db/migrations/00XX_jobs.sql`, `internal/job/handler.go`,
  `internal/job/registry.go` (+ testes), `internal/job/manager.go` (+
  testes), `internal/job/backup/backup.go` (+ testes),
  `internal/server/jobs.go` (+ testes), `internal/db/queries/job.sql` (sqlc,
  ASCII).
- Alterar: `internal/server/server.go` (campo + Option + rota),
  `cmd/mymediadb/main.go` (registro do handler + start do scheduler),
  `openapi.yaml` (+ regen tipos web), `AppHeader` (link).
- Frontend: nova rota/página `web/src/routes/jobs...` (ou equivalente à
  convenção de rotas já em uso), componentes de card/histórico,
  `api.ts`/`queries.ts`.
- Opcional: `scripts/backup.sh` ganha uma nota no cabeçalho apontando que a
  rotina agora também existe como job embutido (o script continua válido
  para invocação manual/fora do container).

## 5. Critérios de aceite

- [ ] `GET /api/v1/jobs` lista o job semeado `backup_vacuum`, desabilitado,
  agendado para `0 3 * * *`, sem execuções ainda.
- [ ] `PATCH /api/v1/jobs/backup_vacuum` com `{"enabled": true}` habilita o
  job e calcula `next_run_at` (próxima ocorrência futura de `0 3 * * *`);
  `{"enabled": false}` desliga e zera `next_run_at`.
- [ ] `PATCH` com `schedule_cron` inválido devolve `422` e não altera o
  estado anterior.
- [ ] `POST /api/v1/jobs/backup_vacuum/run` dispara a execução mesmo com o
  job desabilitado; cria uma linha `job_execution` `running` → `success` (ou
  `error`) com mensagem; atualiza `last_run_at`/`last_status`/`last_message`
  em `job_definition`. Uma segunda chamada enquanto a primeira roda devolve
  `409`.
- [ ] O agendador embutido (goroutine + ticker) dispara sozinho um job
  `enabled` cujo `next_run_at` já passou, sem intervenção manual — testável
  encurtando `pollInterval`/forjando `next_run_at` no passado em teste.
- [ ] O job `backup_vacuum` produz de fato um arquivo `.sqlite` via `VACUUM
  INTO` no diretório de backups configurado e rotaciona mantendo só `keep`
  (do `config_json`, default 7).
- [ ] `GET /api/v1/jobs/backup_vacuum/executions` lista o histórico, mais
  recente primeiro, paginável; nunca ultrapassa 100 linhas por job (poda
  automática verificável em teste).
- [ ] Adicionar um NOVO tipo de job (Handler + seed) não exige alterar
  `job_definition`/`job_execution` nem os endpoints — só registrar o
  `Handler` em `main.go` e inserir a linha de seed (prova da extensibilidade
  pedida).
- [ ] Tela `/jobs` no frontend lista o card, permite habilitar/desabilitar,
  editar o agendamento, disparar execução manual com feedback de
  carregamento/resultado, e ver o histórico.
- [ ] `go build/vet/test -count=1` verdes; catraca local (Go + type-check
  web) passa; funções novas com gocyclo ≤ 10.

## 6. Notas de implementação / decisões

- **Scheduler embutido, não cron do SO.** O deploy-alvo é um único binário/
  container (Pi 3B via Watchtower, [deploy.md](../docs/deploy.md)); não há
  garantia de um crontab de host mantido pelo dono, e a aplicação não deve
  depender de infraestrutura externa para uma função própria. Uma goroutine
  com `time.Ticker` dentro do processo é suficiente e mais simples que expor
  a superfície de um `cron` do SO via container.
- **Cron expression, não intervalo simples.** Cogitado um formato mais
  simples (`{"every_seconds": N}`), mas ele não expressa bem "às 3 da manhã,
  todo dia" sem lógica extra de ancoragem em horário — e o dono JÁ pensa em
  termos de crontab (o próprio `scripts/backup.sh` sugere `0 3 * * *`).
  Expressão cron de 5 campos dá semântica exata e familiar, ao custo de uma
  dependência pequena, pura-Go (sem cgo, sem risco pro cross-compile
  armv7/arm64/amd64 do [Dockerfile multi-stage](../briefings/0003-empacotamento-dockerfile-multistage.md)),
  usada só como PARSER (`ParseStandard(...).Next(t)`) — o loop de disparo
  continua sendo o nosso `Manager.RunScheduler`, não o scheduler da lib.
- **`enabled` só governa o automático; manual sempre funciona.** Pedido
  explícito do dono ("habilitar/desabilitar" E "rodar na hora que ele
  quiser" são eixos independentes) — um job desabilitado ainda responde a
  `POST .../run`.
- **Sem backlog de execuções perdidas.** Se o processo ficou fora do ar na
  hora agendada, ao voltar o próximo tick só dispara UMA execução (não
  empilha "quantas vezes deveria ter rodado") e recalcula `next_run_at` a
  partir de agora — mais simples e seguro que tentar reconstruir um
  histórico de agendamentos perdidos.
- **`job_definition.id` é slug, não uuid.** Precisa casar com `Handler.Nome()`
  em código — é dado curado (seed via migration), não gerado em runtime,
  igual `app_settings.key`.
- **Sem tabela por tipo de job** ([ADR-0005](../docs/adr/0005-composicao-uniforme-extension-data.md)):
  `job_execution` é genérica para QUALQUER `Handler`; a configuração
  específica de cada tipo vive em `config_json` namespaced, interpretado só
  pelo próprio `Handler` — o núcleo nunca conhece o formato. Este é o ponto
  central para servir de base ao futuro sistema de plugins: um plugin
  registraria seu próprio `Handler` sem exigir nenhuma migração no núcleo.
- **Seed único, desabilitado por padrão.** Escolhido `backup_vacuum` (não um
  job "fake"/de exemplo) porque já existe, é idempotente, não-destrutivo
  (só cria/rotaciona arquivos) e prova o sistema ponta a ponta com valor
  real. `enabled=0` por padrão: escrever no disco automaticamente sem
  consentimento explícito do dono seria surpreendente, especialmente num
  Pi com cartão SD pequeno — o dono habilita depois de revisar o
  agendamento/retenção.
- **Retenção de histórico por contagem (100/job), não por tempo.** Mais
  previsível que "90 dias" para jobs de frequência variável; poda
  best-effort a cada INSERT, sem job de limpeza dedicado.
- **Compatibilidade com o job de saúde da biblioteca (futuro).** O
  `Handler.Executar(ctx, cfg)` recebe só contexto e config — a implementação
  concreta é construída com as dependências que precisar (banco, serviços
  internos) na hora do registro em `main.go`, exatamente como `enrich.Servico`
  e `scanner.Manager` já são hoje. Um futuro `libraryhealth.Handler` poderia
  ser construído com acesso ao banco e escrever nas filas de revisão/órfãs já
  existentes ([0018](0018-fila-revisao-orfas-correcao-match.md)) sem precisar
  de nenhuma mudança neste schema — só um novo `Handler` + uma nova linha de
  seed.

### Decisões confirmadas (eram perguntas em aberto)

- **Parser cron**: `github.com/robfig/cron/v3` (MIT, puro-Go) — padrão de-facto
  do ecossistema Go, usado só para `Next()`; o loop de disparo continua nosso.
  Confirmado em vez de escrever um parser próprio.
- **`pollInterval = 30s`** e **cap de retenção de 100 execuções/job**:
  confirmados como estão — fixos em código, não configuráveis por env/UI neste
  briefing (ver "Fora deste briefing" para configurabilidade futura, se
  aparecer necessidade real).
- **Tela `/jobs`**: entra dentro de uma seção **"Administração"**, agrupada com
  a tela de idiomas/settings existente ([0021](0021-ui-idiomas-admin-seletor.md)),
  em vez de item direto no `AppHeader` — mantém o header enxuto e sinaliza que
  é área de configuração/operação, não navegação do dia a dia.

## 7. Fora deste briefing / follow-ups

- **Job de saúde da biblioteca** (órfãos, entidades sem arquivo → fila de
  revisão com motivo) — briefing futuro, depende desta fundação.
- **Maquinário de plugins de terceiros** (manifest, install/enable/disable/
  purge, sandbox, widgets de frontend dinâmicos) — pós-MVP ([11](../docs/11-decisoes-abertas.md)).
- **Notificação de falha de job** (e-mail/webhook) — cogitado, fora do MVP.
- **Configuração de retenção/poll por UI** — hoje é constante Go; virar
  configurável (via `app_settings` ou `config_json`) se a necessidade
  aparecer.
- **Múltiplos agendamentos por job / dependência entre jobs / retry
  automático com backoff** — não há caso de uso concreto ainda; adicionar só
  quando surgir.
