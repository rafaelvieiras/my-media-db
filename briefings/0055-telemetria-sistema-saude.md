# 0055 — Telemetria de sistema: tamanho do banco, uso operacional e disco

> A tela/endpoint de saúde passa a mostrar, além do ping da API/banco, o
> tamanho do SQLite, um retrato operacional (contagens de catálogo, uptime,
> memória) e o uso de disco de cada datasource + do host.

| | |
|---|---|
| **ID** | 0055 |
| **Tipo** | feature |
| **Fase (roadmap)** | Transversal / infraestrutura operacional — mesma família do [0052](0052-fundacao-sistema-de-jobs.md)/[0053](0053-job-saude-biblioteca.md), mas sem depender deles. |
| **Docs relacionadas** | [03-arquitetura](../docs/03-arquitetura.md), [ADR-0010](../docs/adr/0010-concorrencia-sqlite-wal-escritor-unico.md) |
| **Status** | done |
| **Depende de** | — |

---

## 1. Objetivo

Hoje `GET /api/v1/health` (e a tela `/health` que o consome) só respondem
"a API está de pé e o banco pinga" — `{status, version, db:{ok,schema}}`. Isso
basta para o `HEALTHCHECK` do Docker, mas não ajuda o dono a responder
perguntas operacionais básicas que só ele pode ver de fora do container: *"meu
banco tá ficando grande?"*, *"quantos itens eu já catalogei?"*, *"o HD da
biblioteca de filmes tá enchendo?"*, *"o processo tá de pé há quanto tempo?"*.
Sem isso, a única forma de responder é entrar no host e rodar `du`/`df`/
`sqlite3` manualmente — inviável para o público-alvo (self-hosted, Raspberry
Pi, sem acesso fácil a shell no dia a dia).

Este briefing entrega uma leitura **local, em snapshot, sob demanda**: nada de
histórico, gráfico, alerta proativo ou integração com Prometheus/Grafana —
só números atuais, calculados quando o dono abre a tela (ou bate no endpoint),
reaproveitando dado que o sistema já coleta (tamanho por `PRAGMA`, uso de disco
por `media_file.size` já gravado pelo scanner) em vez de reprocessar o disco do
zero.

## 2. Jornada do usuário

1. O dono abre a tela **Saúde** (`/health`, já existente desde o 0001 — hoje só
   mostra um card "API online — v1.2.3"/badge de status).
2. Abaixo do card de status atual (inalterado), uma nova seção **"Sistema"**
   aparece com três blocos:
   - **Banco de dados**: tamanho em disco do arquivo `.sqlite` (formatado,
     ex. "128,4 MB") e a versão de schema (já existia, migrada para cá).
   - **Uso**: quantas entities existem por tipo (filmes, séries, temporadas,
     episódios, coleções, pessoas, empresas), quantos arquivos de mídia
     (presentes/ausentes), quantas fontes cadastradas, tamanho da fila de
     revisão e de órfãs (mesmas contagens do [0053](0053-job-saude-biblioteca.md)/
     [0018](0018-fila-revisao-orfas-correcao-match.md)), quantas execuções de
     job já rodaram, e há quanto tempo o processo está no ar (uptime).
   - **Armazenamento**: uma linha por datasource cadastrado (`library_source`),
     mostrando quanto de mídia rastreada ela ocupa e quanto de espaço
     livre/total tem o filesystem em que ela vive (barra de progresso simples);
     se o path estiver inacessível (offline), a linha mostra isso em vez de
     travar as demais.
3. Enquanto os dados carregam, a seção mostra um esqueleto/"Carregando…"
   (mesmo padrão de `LoadingState` já usado em `/jobs`); se a chamada falhar,
   um `ErrorState` — o card de status básico continua funcionando mesmo que
   a telemetria falhe (são chamadas separadas, ver §4).
4. Não há atualização automática nem push: o dono recarrega a página (ou
   navega para fora e volta) para um snapshot mais novo. O servidor cacheia o
   cálculo por um TTL curto (ver §6) para não recalcular o uso de disco a cada
   clique — reloads dentro do TTL respondem na hora.

## 3. Escopo

**Dentro:**
- Endpoint novo `GET /api/v1/health/system` com o snapshot de telemetria
  (banco + uso + armazenamento), **separado** de `GET /api/v1/health` (ver §6
  — o endpoint de healthcheck do Docker bate a cada 10s e precisa continuar
  барato).
- Tamanho do banco via `PRAGMA page_count`/`page_size` (tamanho lógico do
  arquivo principal `.sqlite`; ver §6 para o porquê de não somar
  `-wal`/`-shm`).
- Contagens de uso: entities por `type`, media_file por `state`, total de
  `library_source`, fila de revisão/órfãs (reaproveitando
  `internal/curation.ContarRevisao`/`ContarOrfas` do 0053 — **zero** SQL novo
  duplicado), total de `job_execution`, uptime do processo, `runtime.MemStats`
  básico (heap alocado) e `runtime.NumGoroutine()`.
- Uso de disco por fonte: soma de `media_file.size` (já coletado pelo scanner,
  **sem** varrer o filesystem de novo) por `library_source_id`, mais espaço
  livre/total do filesystem que hospeda aquele `Path` via `unix.Statfs`
  (`golang.org/x/sys/unix`, já é dependência indireta do módulo — só promove a
  import direto).
- Cache em memória com TTL curto (mesmo padrão do `cacheExterno` de
  `internal/server/search.go`) para não recalcular a cada request.
- Extensão da tela `/health` existente (`web/src/routes/health.tsx`) com a
  nova seção "Sistema" — **não** cria rota nova.
- Testes de unidade do handler/serviço (fakes de banco/filesystem onde couber)
  e do componente de frontend (estados loading/erro/dados).

**Fora (explicitamente):**
- **Qualquer alerta ou notificação proativa** (ex. "disco cheio", e-mail,
  webhook) — este briefing só relata, não age nem avisa.
- **Histórico/gráfico ao longo do tempo.** É sempre um snapshot do agora; sem
  série temporal, sem tabela de amostras persistida.
- **Integração com observabilidade externa** (Prometheus, Grafana, StatsD,
  OpenTelemetry) — é tudo local, consumido só pela própria UI do MyMediaDB.
- **Alterar o contrato de `GET /api/v1/health`** (usado pelo `HEALTHCHECK` do
  Docker e por quem já integra com ele) — este permanece exatamente como está
  hoje; a telemetria nova mora só no endpoint novo.
- **Uso de disco por diretório arbitrário do host** fora das
  `library_source` cadastradas (ex. não reporta uso do disco do sistema
  operacional em si, só do filesystem que hospeda cada fonte).
- **Configuração de TTL do cache pelo dono** — é uma constante de código,
  não uma preferência exposta na UI (mesmo espírito do `ttlCacheExterno`).

## 4. Contrato técnico

### Dados / modelo

Sem migração nova — todo o dado já existe:
- Tamanho do banco: `PRAGMA page_count` / `PRAGMA page_size` (query direta,
  sem tabela).
- Contagens: `entity` (agrupado por `type`), `media_file` (agrupado por
  `state`), `library_source` (`COUNT(*)`), `job_execution` (`COUNT(*)`),
  filas de revisão/órfãs via `internal/curation`.
- Uso de disco por fonte: `SUM(media_file.size)` filtrado por
  `library_source_id` e `state='present'` — já indexado por
  `idx_media_file_library_source` (migration 0003).

Novas queries sqlc em `internal/db/queries/telemetria.sql` (nome do arquivo
livre; sem tabela nova, só leitura agregada):
```sql
-- name: ContarEntitiesPorTipo :many
SELECT type, COUNT(*) AS total
FROM entity
GROUP BY type;

-- name: ContarMediaFilesPorState :many
SELECT state, COUNT(*) AS total
FROM media_file
GROUP BY state;

-- name: ContarLibrarySources :one
SELECT COUNT(*) AS total FROM library_source;

-- name: ContarJobExecutions :one
SELECT COUNT(*) AS total FROM job_execution;

-- name: SomarTamanhoMediaFilePorFonte :one
SELECT COALESCE(SUM(size), 0) AS total_bytes
FROM media_file
WHERE library_source_id = ? AND state = 'present';
```

### API

Novo endpoint, mesmo namespace do health atual:

**`GET /api/v1/health/system`**

Resposta `200`:
```json
{
  "computed_at": "2026-07-15T19:40:00.000Z",
  "database": {
    "size_bytes": 134676480,
    "page_count": 32880,
    "page_size": 4096,
    "schema_version": 12
  },
  "usage": {
    "uptime_seconds": 431980,
    "entities": {
      "total": 812,
      "by_type": { "movie": 340, "tv": 12, "season": 96, "episode": 340, "collection": 18, "person": 0, "company": 0 }
    },
    "media_files": { "total": 690, "present": 682, "missing": 8 },
    "library_sources": 3,
    "review_queue_size": 5,
    "orphans_count": 2,
    "job_executions_total": 47,
    "goroutines": 14,
    "mem_alloc_bytes": 18874368
  },
  "storage": {
    "sources": [
      {
        "source_id": "a1b2...",
        "path": "/mnt/filmes",
        "used_bytes": 892173058048,
        "filesystem": { "free_bytes": 120259084288, "total_bytes": 2000398934016 },
        "offline": false
      },
      {
        "source_id": "c3d4...",
        "path": "/mnt/series",
        "used_bytes": 0,
        "filesystem": null,
        "offline": true,
        "error": "estatisticas de filesystem indisponiveis: no such file or directory"
      }
    ]
  }
}
```
- `database.schema_version` é o MESMO valor de `checkDB().Schema` — reexposto
  aqui por conveniência (a tela `/health` nova não precisa somar duas
  respostas para montar um único card de "banco").
- `usage.entities.by_type` inclui só os tipos que ocorrem (chave ausente =
  zero, não `0` explícito) — evita fixar no contrato uma lista de tipos que
  já é livre no schema (`entity.type` é `TEXT` sem `CHECK`, ADR de
  extensibilidade).
- `storage.sources[].offline`/`error` espelham a semântica de
  `avaliarState` já usada em `library.Listar` (fonte cujo `Path` não responde
  a `os.Stat`/`Statfs`); `filesystem: null` quando offline (sem número
  inventado).
- Sem banco injetado (`s.db == nil`), devolve `503` com o shape de erro padrão
  (`{error:{code:"db_unavailable",...}}`) — mesma degradação que outros
  endpoints dependentes de banco já praticam neste projeto (nunca `panic`,
  nunca 200 com dado inventado).
- Erros do cálculo (ex. `PRAGMA` falhou) também viram `503`; este endpoint
  NUNCA derruba `GET /api/v1/health` (são handlers e code paths
  independentes).

`GET /api/v1/health` **não muda** (contrato do 0001/0006 preservado).

### Backend (Go)

- Novo pacote `internal/telemetry`:
  - `telemetry.go`: `Servico` construído com `*db.DB` (mesmo padrão de
    `library.New`/`curation.New`). `Snapshot(ctx) (Snapshot, error)` monta o
    payload completo (banco + uso), SEM a parte de armazenamento por fonte
    (que precisa da lista de `library_source`, resolvida por quem chama —
    evita o pacote `telemetry` importar `internal/library` só por isso;
    `internal/server` já tem `s.library`).
  - `TamanhoBanco(ctx) (DatabaseSize, error)`: roda os dois `PRAGMA` pela
    conexão de LEITURA (`s.database.Read`, ADR-0010 — é uma leitura, não
    precisa do escritor único).
  - `ContagensUso(ctx) (UsageCounts, error)`: as 4 queries sqlc acima +
    `curation.ContarRevisao`/`ContarOrfas` (injetado como dependência, mesmo
    padrão de interface pequena do `libraryhealth.Handler` — permite fake em
    teste sem banco real).
  - `UsoDisco(ctx, sourceID string) (int64, error)`: roda
    `SomarTamanhoMediaFilePorFonte`.
  - `EstatisticasFilesystem(path string) (FilesystemStats, error)`: chama
    `unix.Statfs(path, &stat)`; devolve `free_bytes = stat.Bavail * uint64(stat.Bsize)`,
    `total_bytes = stat.Blocks * uint64(stat.Bsize)`. Erro (path não existe/
    não montado) é devolvido para o chamador decidir o `offline`/`error` da
    fonte — o pacote `telemetry` não decide sozinho o que é "offline" (isso é
    conceito de `internal/library`; aqui só reporta o erro cru).
  - `uptime.go`: `processoIniciadoEm = time.Now()` capturado em `init()` (ou
    injetado via `New(startedAt time.Time, ...)` para testabilidade) —
    `UptimeSegundos() int64` é só `time.Since(processoIniciadoEm).Seconds()`.
- `internal/server/health_system.go` (novo arquivo, ao lado de `health.go`):
  - `systemHealthHandler`: monta `telemetry.Servico` (guardado como campo
    `s.telemetry`, construído em `montarServicos` quando há banco — mesmo
    padrão de `s.jobs`), chama `Snapshot`, itera `s.library.Listar(ctx)` para
    montar `storage.sources` (uma chamada de `UsoDisco` +
    `EstatisticasFilesystem` por fonte), aplica o cache (ver §6), serializa.
  - Registrado em `internal/server/server.go`:
    `v1.Get("/health/system", s.systemHealthHandler)`.
- `internal/server/telemetry_cache.go` (ou dentro do mesmo arquivo do
  handler): cache em memória com mutex + TTL, MESMO desenho de
  `cacheExterno` (`internal/server/search.go`) — chave única (não há
  parâmetro variável aqui, então é um único slot cacheado, mais simples que
  o cache de busca externa que é por `(query,tipo)`).
- Promove `golang.org/x/sys/unix` de indireto para direto no `go.mod` (`go mod
  tidy` depois do primeiro uso) — já resolvido no `go.sum`, sem baixar nada
  novo.

### Frontend (Vite/React)

- `web/src/lib/api.ts`: `getSystemHealth(): Promise<SystemHealth>` — mesmo
  padrão de `getHealth` (fetch + `!res.ok` lança).
- `web/src/lib/queries.ts`: `systemHealthQueryOptions = queryOptions({
  queryKey: ['health', 'system'], queryFn: getSystemHealth })` — chave
  aninhada sob `['health', ...]` mas request SEPARADA de `healthQueryOptions`
  (dois `useQuery` na mesma tela; uma falha na telemetria não derruba o
  card de status básico).
- `web/src/lib/api-types.ts`: tipos gerados via openapi a partir do schema do
  handler novo (`SystemHealth`, `DatabaseHealth`, `UsageCounts`,
  `StorageSnapshot`, `SourceStorage`) — seguindo o mesmo pipeline
  `gen:types` já usado no projeto.
- `web/src/routes/health.tsx`: adiciona a seção "Sistema" abaixo do `Card`
  existente (que fica intocado). Três sub-blocos (banco/uso/armazenamento),
  cada um com seu próprio tratamento de `isPending`/`isError` vindo de
  `systemHealthQueryOptions` — reaproveita `LoadingState`/`ErrorState`
  (`@/components/query-states`, mesmo padrão de `/jobs`).
- Formatação de bytes: função utilitária nova `formatBytes` (ou reaproveitar
  se já existir uma em `web/src/lib/format.ts` — confirmar na implementação;
  se não existir, criar ali, não inline na tela) para exibir
  "128,4 MB"/"1,86 TB" em vez de bytes crus.
- Barra de uso por fonte: componente simples (`<progress>`-like via `div` com
  `width%` ou o `Progress` do design system se já existir em
  `web/src/components/ui/` — confirmar na implementação) mostrando
  `used_bytes` da fonte sobre `total_bytes` do filesystem; fonte `offline`
  mostra um badge "indisponível" no lugar da barra.

### Arquivos afetados

- Criar: `internal/telemetry/telemetry.go` (+ `telemetry_test.go`),
  `internal/db/queries/telemetria.sql` (+ sqlc gera
  `internal/db/sqlcgen/telemetria.sql.go`), `internal/server/health_system.go`
  (+ `health_system_test.go`), `internal/server/telemetry_cache.go`.
- Alterar: `internal/server/server.go` (campo `s.telemetry`, construção em
  `montarServicos`, rota `GET /health/system`), `go.mod`/`go.sum` (promover
  `golang.org/x/sys` a direto), `web/src/lib/api.ts`, `web/src/lib/
  queries.ts`, `web/src/lib/api-types.ts` (gerado), `web/src/routes/
  health.tsx` (+ `health.test.tsx`).

## 5. Critérios de aceite

- [ ] `GET /api/v1/health` continua respondendo exatamente o mesmo shape de
  hoje (`{status, version, db:{ok,schema}}`) — nenhum campo novo, nenhuma
  regressão de latência (prova de que a telemetria pesada não entrou nesse
  handler).
- [ ] `GET /api/v1/health/system` devolve `200` com banco/uso/armazenamento
  preenchidos quando há banco e ao menos uma `library_source` cadastrada.
- [ ] `database.size_bytes` bate com `page_count * page_size` do `PRAGMA` no
  momento do cálculo (provado comparando com uma leitura direta em teste).
- [ ] `usage.entities.by_type` reflete contagens reais por `type` inserido em
  fixture de teste (movie/season/episode/collection misturados).
- [ ] `usage.review_queue_size`/`orphans_count` batem exatamente com o que
  `GET /review`/`GET /orphans` devolveriam para o mesmo fixture (mesma
  garantia que o 0053 já prova para `internal/curation`).
- [ ] `storage.sources[].used_bytes` bate com a soma de `media_file.size`
  daquela fonte com `state='present'` — SEM nenhuma chamada a
  `filepath.WalkDir` no code path deste endpoint (prova de que reaproveita o
  dado do scanner em vez de re-varrer o disco).
- [ ] Fonte com `Path` inacessível (diretório removido/desmontado) aparece em
  `storage.sources` com `offline:true` e `filesystem:null`, sem abortar as
  demais fontes nem devolver `5xx` para o endpoint inteiro.
- [ ] Duas chamadas a `GET /api/v1/health/system` dentro do TTL do cache
  devolvem o MESMO `computed_at` (prova de que o segundo request não
  recalculou); uma chamada após o TTL expirar devolve um `computed_at` novo.
- [ ] Sem banco injetado (`s.db == nil`), `GET /api/v1/health/system` devolve
  `503` com o shape de erro padrão, e `GET /api/v1/health` continua 200
  (degradação independente entre os dois endpoints).
- [ ] A tela `/health` mostra a seção "Sistema" com os três blocos, cada um
  com seu próprio estado de loading/erro; o card de status básico (já
  existente) renderiza normalmente mesmo se `GET /health/system` falhar.
- [ ] `go build/vet/test -count=1` verdes; `tsc -b` do web verde; `make
  gate-local` não piora.

## 6. Notas de implementação / decisões

- **`GET /api/v1/health/system` separado de `GET /api/v1/health`, não um
  campo a mais no mesmo endpoint.** O `Dockerfile` já define `HEALTHCHECK
  --interval=10s` batendo em `/api/v1/health` (confirmado em `Dockerfile`).
  Calcular uso de disco por fonte é uma operação bem mais cara que um `Ping`
  — mesmo reaproveitando `media_file.size` (sem walk), ainda envolve N
  syscalls `Statfs` (uma por fonte) e algumas agregações SQL. Colocar isso no
  mesmo handler do healthcheck faria o Docker (e qualquer coisa que
  monitore o container) pagar esse custo a cada 10 segundos, para sempre,
  mesmo quando ninguém está olhando a tela. Separar os dois deixa o
  healthcheck do jeito que está (rápido, usado por infraestrutura) e a
  telemetria como uma chamada sob demanda (só quando o dono abre a tela).
- **Cache em memória com TTL, não sem cache nenhum.** Mesmo sob demanda, se o
  dono deixar a aba de "Saúde" aberta e o TanStack Query refizer o fetch (foco
  de janela, reconexão), recalcular tudo de novo a cada poucos segundos ainda
  é desperdício, especialmente em hardware modesto (Pi 3B, alvo de deploy do
  projeto — ver `docs/deploy.md`) ou biblioteca em disco de rede. TTL
  proposto: `ttlTelemetria = 60 * time.Second` — fresco o bastante para
  refletir uma mudança recente (ex. acabou de rodar um scan), sem recalcular
  a cada clique. Mesmo padrão de `cacheExterno`/`ttlCacheExterno` já
  estabelecido no projeto (`internal/server/search.go`), só que cache
  singleton (não por chave) porque este endpoint não tem parâmetro variável.
- **Tamanho do banco via `PRAGMA page_count * page_size`, não `os.Stat` do
  arquivo `.sqlite`.** Duas razões: (1) o `db.DB` (`internal/db/db.go`) não
  guarda o `Config.Path` original depois de `Open` — só os dois `*sql.DB` —
  então `os.Stat` exigiria adicionar um campo novo ao `DB` ou injetar o path
  separadamente em `server.New` (mais um parâmetro/`Option`, ecoando
  `WithAssetsDir`); o `PRAGMA` não precisa de nada disso, roda pela conexão
  que já existe. (2) é o número que o próprio SQLite considera "tamanho do
  banco" (o mesmo que `sqlite3_analyzer`/`.dbinfo` reportam), então bate com
  a intuição do dono ao comparar com ferramentas externas. **Decisão
  consciente de NÃO somar os arquivos `-wal`/`-shm` sidecar**: eles são
  artefatos transitórios do modo WAL (ADR-0010) — o `-wal` é truncado
  periodicamente (checkpoint automático do SQLite) e não é "dado do dono" no
  mesmo sentido do arquivo principal; somar um número que oscila de forma não
  intuitiva (cresce entre checkpoints, zera depois) confundiria mais do que
  ajudaria num snapshot simples. Se o dono precisar do tamanho real em disco
  de TODOS os artefatos, isso é uma pergunta de operação de baixo nível fora
  do escopo desta tela.
- **Uso de disco por fonte via `SUM(media_file.size)`, não
  `filepath.WalkDir`.** O scanner (`internal/scanner/scan.go`) já popula
  `media_file.size` a partir de `info.Size()` no momento do scan
  (`f.size = info.Size()`, usado em `CriarMediaFile`/`AtualizarMediaFile*`
  via `match.go`/`serie.go`). Re-varrer o filesystem inteiro de cada fonte a
  cada abertura da tela de saúde seria caro (I/O síncrono sobre,
  potencialmente, HDs externos ou montagens de rede — o mesmo tipo de custo
  que o [0053](0053-job-saude-biblioteca.md) já trata como "coisa que roda de
  madrugada", não sob demanda numa tela) e redundante: o dado já existe,
  atualizado no último scan daquela fonte. Trade-off aceito: o número
  reflete o estado no ÚLTIMO SCAN, não o disco agora-agora — mesma
  defasagem que toda leitura de `media_file` já tem no resto do sistema
  (revisão/órfãs, [0053] existe justamente para isso). Documentado na UI
  como "mídia rastreada" (não "uso real do disco agora"), para não prometer
  mais precisão do que a fonte de dado garante.
- **Espaço livre/total do filesystem via `golang.org/x/sys/unix.Statfs`, não
  `syscall.Statfs` da stdlib diretamente.** `syscall.Statfs` existe na
  stdlib mas seus campos (`Bsize`, `Blocks`, `Bavail`) têm tipos que variam
  por arquitetura/SO (ex. `int32` vs `int64` conforme a plataforma), exigindo
  conversões defensivas espalhadas; `golang.org/x/sys/unix` normaliza isso
  numa API mais estável entre arquiteturas (relevante para o alvo de deploy
  cross-compile amd64/arm64/armv7 do projeto — `docs/deploy.md`). Já é
  dependência INDIRETA do módulo (via `modernc.org/sqlite`/goose, presente no
  `go.sum` em `v0.45.0`) — usar `unix.Statfs` só promove a mesma versão já
  resolvida para import direto, sem baixar nada novo nem arriscar
  incompatibilidade de versão. Restrito a build Linux/Unix (`unix.Statfs` não
  compila em Windows) — aceitável: o alvo de deploy do projeto é sempre
  Linux (container/Pi, `docs/deploy.md`); se um dia houver build Windows,
  isso precisa de um arquivo `_windows.go` com stub, fora do escopo aqui.
- **"Telemetria" definida como contagens + uptime + memória do processo, sem
  qualquer coleta de uso do DONO (nenhum analytics, nenhuma telemetria
  enviada para fora).** O termo no título do briefing é sobre o sistema se
  auto-relatar PARA o próprio dono, localmente — não confundir com telemetria
  de produto/analytics enviada a terceiros (o projeto não faz isso, ponto
  fixado em `AGENTS.md`/`docs/03-arquitetura.md` sobre self-hosted sem
  telemetria externa).
- **`runtime.MemStats`/`NumGoroutine()` incluídos, sem GC stats detalhado
  nem profiling.** Serve como termômetro grosso ("o processo está usando
  quanta memória, quantas goroutines estão de pé") para detectar algo
  claramente anormal (ex. vazamento de goroutine); não é uma ferramenta de
  profiling — para isso o projeto já tem `net/http/pprof` disponível
  separadamente se algum dia for exposto (não faz parte deste briefing).
- **Cálculo de `storage.sources` no HANDLER (`internal/server`), não dentro
  de `internal/telemetry`.** `internal/telemetry` não conhece
  `library.Source` (evita acoplar o pacote novo a `internal/library` só
  para iterar fontes); o handler já tem `s.library.Listar(ctx)` disponível e
  monta a lista chamando `telemetry.UsoDisco`/`EstatisticasFilesystem` por
  fonte — mesmo espírito de composição enxuta já usado em
  `libraryhealth.Handler` (interfaces pequenas, composição no chamador, não
  no pacote de baixo nível).
- **Erro em UMA fonte não aborta as demais nem o endpoint inteiro.** Mesma
  filosofia "best-effort entre fontes" já estabelecida no
  [0053](0053-job-saude-biblioteca.md) para o job de re-scan: uma fonte
  offline vira uma entrada com `offline:true`/`error`, as outras continuam
  normais.

## 7. Fora deste briefing / follow-ups

- **Alertas/notificações** quando disco ultrapassa um limiar, banco cresce
  rápido demais, etc. — cogitável como job futuro (poderia reaproveitar o
  sistema de jobs do 0052/0053), sem caso de uso concreto ainda.
- **Histórico de telemetria** (guardar snapshots ao longo do tempo para
  gráfico de crescimento) — exigiria tabela nova + política de retenção;
  fora do escopo de "snapshot simples" deste briefing.
- **Botão de "recalcular agora" ignorando o cache** — se o TTL de 60s
  incomodar na prática, é uma mudança pequena e isolada (bypass de cache via
  query param `?fresh=1`, por exemplo) para um briefing futuro se houver
  demanda real.
- **Uso de disco "real" via `filepath.WalkDir`** como alternativa/complemento
  ao `SUM(media_file.size)` (ex. para detectar arquivos no disco que o
  scanner ainda não viu) — o [0053](0053-job-saude-biblioteca.md) já cobre
  a lacuna de "disco mudou e ninguém rodou scan" via re-scan agendado; não é
  redundante aqui, só um follow-up possível.
- **Windows/`unix.Statfs`** — stub de outra implementação para builds não-Unix,
  só se o projeto algum dia precisar rodar fora de Linux (não é o caso hoje).
