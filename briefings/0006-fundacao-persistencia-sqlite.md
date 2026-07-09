# 0006 — Fundação de persistência SQLite

> A camada de banco: SQLite em WAL, pool de leitura + escritor único, migrations
> embutidas (goose) e wiring do sqlc. Ainda sem tabelas de domínio.

| | |
|---|---|
| **ID** | 0006 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 2 — UI de biblioteca |
| **Docs relacionadas** | [03-arquitetura](../docs/03-arquitetura.md), [ADR-0010](../docs/adr/0010-concorrencia-sqlite-wal-escritor-unico.md), [ADR-0014](../docs/adr/0014-stack-de-implementacao.md), [ADR-0017](../docs/adr/0017-migrations-goose-embedado.md) |
| **Status** | done |
| **Depende de** | [0001](0001-backend-esqueleto-servidor.md) |

---

## 1. Objetivo

A Fase 1 entregou o esqueleto HTTP **sem banco**. Nada da Fase 2 (datasource,
scan, entities) existe sem persistência. Este briefing entrega a **fundação de
dados**: abrir o SQLite com a receita de concorrência ratificada ([ADR-0010](../docs/adr/0010-concorrencia-sqlite-wal-escritor-unico.md)),
um mecanismo de migrations que roda no boot ([ADR-0017](../docs/adr/0017-migrations-goose-embedado.md))
e o wiring do sqlc — de modo que os próximos briefings só precisem **adicionar
tabelas e queries**, não reinventar o encanamento. É infraestrutura pura: não há
tabela de domínio nem endpoint novo além do já existente.

## 2. Jornada do usuário

A "jornada" é do **dev** e do **operador**:

1. Sobe o binário (ou `air` em dev). No boot, o banco é aberto e as **migrations
   pendentes são aplicadas automaticamente**; um log informa a versão de schema.
2. Se uma migration falha, o processo **não sobe** (fail-fast) e o erro é claro.
3. `GET /api/v1/health` passa a reportar também o **estado do banco** (ping OK +
   versão de schema), além do que já reportava.
4. O caminho do arquivo `.sqlite` é configurável por env/flag; em dev fica em
   `tmp/`, ignorado pelo git.

## 3. Escopo

**Dentro:**
- Abertura do SQLite via **`modernc.org/sqlite`** com pragmas de [ADR-0010](../docs/adr/0010-concorrencia-sqlite-wal-escritor-unico.md):
  `journal_mode=WAL`, `busy_timeout=5000`, `synchronous=NORMAL`, `foreign_keys=ON`.
- **Dois pools**: um `*sql.DB` de leitura (N conexões) e um `*sql.DB` de escrita
  com `SetMaxOpenConns(1)` (escritor único). Ambos sobre o mesmo arquivo.
- **Migrations com goose embedado** (`//go:embed` de `internal/db/migrations/*.sql`),
  aplicadas no startup pela conexão de **escrita**. Uma migration inicial mínima
  (ex.: só a tabela de controle do goose, ou um `schema_version` trivial) só para
  provar o mecanismo — o schema de domínio é o [0007](0007-schema-nucleo-minimo.md).
- **Wiring do sqlc**: `sqlc.yaml` configurado (engine sqlite, pacote de saída,
  emit de interfaces), gerando um pacote vazio/mínimo que compila.
- `/api/v1/health` estendido com ping ao banco e versão de schema.
- Configuração do caminho do DB (env `MYMEDIADB_DB_PATH` / flag), com default de dev.
- Testes: abertura aplica pragmas corretos; migrate up/down; leitura concorrente
  não bloqueia durante escrita; health reflete o banco.

**Fora (explicitamente):**
- Tabelas de domínio (`entity`, `media_file`, …) — é o [0007](0007-schema-nucleo-minimo.md).
- Qualquer endpoint de biblioteca/entities — [0008](0008-datasources-cadastro-api-library.md)+.
- Fila/goroutine escritora dedicada (a evolução citada na [ADR-0010](../docs/adr/0010-concorrencia-sqlite-wal-escritor-unico.md));
  aqui basta o pool de escrita de tamanho 1.

## 4. Contrato técnico

### Dados / modelo
- Nenhuma tabela de domínio. Só a infra de migrations (tabela de versão do goose)
  e a migration-semente.

### Backend (Go)
- Pacote novo `internal/db` (ou `internal/store`): `Open(cfg) (*DB, error)` que
  devolve os dois pools; `Migrate(ctx)` aplicando o goose sobre a conexão de escrita;
  `Close()`. Helper de pragmas aplicado por conexão (via DSN e/ou `connect hook`).
- `internal/db/migrations/` com o `embed.FS` e a migration-semente.
- `sqlc.yaml` na raiz de `internal/db` (ou raiz do repo), engine `sqlite`.
- Injeção dos pools no servidor ([0001](0001-backend-esqueleto-servidor.md)); o
  handler de health ganha uma checagem de DB.

### API
- `GET /api/v1/health` — resposta ganha um campo de banco, ex.:
  `{ "status": "ok", "version": "...", "db": { "ok": true, "schema": <n> } }`.
  Mantém compatibilidade com o contrato do [0001](0001-backend-esqueleto-servidor.md)
  (campos existentes preservados).

### Arquivos afetados
- Criar: `internal/db/db.go`, `internal/db/migrations/embed.go`,
  `internal/db/migrations/0001_*.sql`, `sqlc.yaml`, `internal/db/queries/` (vazio/mínimo).
- Alterar: `cmd/mymediadb/main.go` (abrir DB + migrate no boot), `internal/server/*`
  (injeção dos pools, health com DB), `go.mod` (goose, sqlc é ferramenta de dev),
  `.gitignore` (`*.sqlite*` já coberto; garantir `tmp/`).
- CI/Makefile: alvo `sqlc generate` (opcional, se versionarmos o gerado).

## 5. Critérios de aceite

- [ ] O binário abre o banco com **WAL** e os quatro pragmas de [ADR-0010](../docs/adr/0010-concorrencia-sqlite-wal-escritor-unico.md) (verificável por `PRAGMA` numa conexão de cada pool).
- [ ] As **migrations rodam no boot**; subir de novo é idempotente; falha de migration **aborta** o processo.
- [ ] O pool de escrita tem **no máximo 1 conexão**; leituras concorrem sem `database is locked` durante uma escrita.
- [ ] `sqlc generate` roda limpo e o pacote gerado **compila**.
- [ ] `GET /api/v1/health` reporta banco OK + versão de schema, sem quebrar os campos existentes.
- [ ] Testes cobrindo pragmas, migrate up/down e leitura-durante-escrita passam; a catraca ([0005](0005-quality-gate-catraca-unificada.md)) segue verde.

## 6. Notas de implementação / decisões

- **Escritor único via pool de tamanho 1** (não uma goroutine dedicada): é o que a
  [ADR-0010](../docs/adr/0010-concorrencia-sqlite-wal-escritor-unico.md) prescreve
  para o MVP; a fila escritora fica como evolução se a priorização precisar ser
  explícita.
- **Pragmas por conexão:** com `database/sql` e pool, os pragmas devem valer para
  **toda** conexão nova — aplicar via DSN (`?_pragma=...` do modernc) e/ou hook de
  conexão, não uma única vez.
- **goose como biblioteca** (não CLI): migrations embutidas rodam no próprio boot,
  sem binário externo — ver [ADR-0017](../docs/adr/0017-migrations-goose-embedado.md).
- **sqlc convive com goose:** goose versiona o DDL; o sqlc lê o mesmo SQL para
  gerar o acesso. Definir no [0007](0007-schema-nucleo-minimo.md) se o código
  gerado é versionado ou gerado no build.

## 7. Fora deste briefing / follow-ups

- Schema de domínio da Fase 2 → [0007](0007-schema-nucleo-minimo.md).
- Fila/goroutine escritora com backpressure, se necessária.
- Backup/checkpoint explícito do WAL, telemetria de tamanho do banco.
