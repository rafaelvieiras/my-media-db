# 0008 — Datasources: cadastro e API `/api/v1/library`

> Apontar uma pasta do servidor como fonte da biblioteca: cadastrar, validar,
> listar e remover — a superfície de API que a UI de bibliotecas vai consumir.
> Ainda **sem** disparar scan.

| | |
|---|---|
| **ID** | 0008 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 2 — UI de biblioteca |
| **Docs relacionadas** | [05-api](../docs/05-api.md), [06-biblioteca-e-scan](../docs/06-biblioteca-e-scan.md) |
| **Status** | done |
| **Depende de** | [0007](0007-schema-nucleo-minimo.md) |

---

## 1. Objetivo

Antes de escanear qualquer coisa, o dono precisa **dizer ao sistema quais pastas
observar**. Este briefing entrega o CRUD de `library_source` sob `/api/v1/library`,
com validação de caminho (existe? é legível? é diretório?) e o sinal `online/offline`.
É a base de dados-de-entrada do scanner ([0009](0009-scanner-fswalk-ffprobe-matching.md))
e a primeira rota nativa `/api/v1` de verdade — define o padrão de request/response,
erros e o gerador OpenAPI que a Fase 4 formaliza.

## 2. Jornada do usuário

1. O dono abre a tela de Bibliotecas (front no [0011](0011-frontend-grid-lista-detalhe.md))
   e clica em "Adicionar datasource".
2. Informa um **caminho** e um **tipo** (`movie`/`tv`). O sistema valida: existe,
   é diretório, é legível.
3. Caminho inválido → erro claro (404/422) explicando o quê; nada é criado.
4. Válido → cria o `library_source` (`state=online`, `last_scan` nulo) e retorna o registro.
5. A lista mostra os datasources com seu estado; um HD desmontado aparece **offline**.
6. O dono pode **remover** um datasource (sem purgar entities neste briefing — ver §6).

## 3. Escopo

**Dentro:**
- **`POST /api/v1/library`** — cria datasource. Body `{ path, type }`. Valida
  existência/legibilidade/diretório; normaliza o path (absoluto, limpo). Conflito
  de `path` duplicado → 409.
- **`GET /api/v1/library`** — lista datasources com `id, path, type, state, last_scan`.
  Reavalia `state` (online/offline) na leitura, de forma barata (stat do path).
- **`GET /api/v1/library/{id}`** — um datasource.
- **`DELETE /api/v1/library/{id}`** — remove o registro do datasource.
- Validação e erros consistentes (shape de erro padrão do `/api/v1`).
- Testes de handler cobrindo caminho válido, inexistente, arquivo-não-diretório,
  sem permissão, duplicado, remoção.

**Fora (explicitamente):**
- **Disparo/execução de scan** e status de progresso → [0009](0009-scanner-fswalk-ffprobe-matching.md).
- Listagem de entities/itens detectados → [0010](0010-api-leitura-resolver-minimo.md).
- Edição de datasource (mudar path/tipo) — só create/list/delete por ora.
- Purga em cascata ao remover datasource (decisão em §6).
- Autenticação (Fase 4) — por ora o servidor é local, dono-é-autoridade.

## 4. Contrato técnico

### Dados / modelo
- Só `library_source` (criado no [0007](0007-schema-nucleo-minimo.md)). `state`
  reavaliado por stat; `last_scan` permanece como o scanner deixar.

### API (`/api/v1/library`)
- **POST** `{ "path": "/mídia/filmes", "type": "movie" }` → `201` com o registro;
  `422` (path inválido/ilegível), `409` (duplicado).
- **GET** → `200 { "items": [ { id, path, type, state, last_scan } ] }`.
- **GET /{id}** → `200` | `404`.
- **DELETE /{id}** → `204` | `404`.
- Shape de erro padrão: `{ "error": { "code": "...", "message": "..." } }`.

### Backend (Go)
- `internal/library` (serviço): validação de path (usa `os.Stat`/leitura de dir),
  normalização, CRUD via sqlc; avaliação de `state` (stat barato).
- Handlers chi em `/api/v1/library`; **escritas pela conexão de escrita** ([ADR-0010](../docs/adr/0010-concorrencia-sqlite-wal-escritor-unico.md)),
  leituras pelo pool de leitura.

### Frontend (Vite/React)
- Nada neste briefing (a tela é o [0011](0011-frontend-grid-lista-detalhe.md)); mas o
  contrato definido aqui é o que ela consome.

### Arquivos afetados
- Criar: `internal/library/*.go`, handlers em `internal/server/` (ou `internal/api/v1/`).
- Alterar: montagem de rotas do servidor; queries sqlc de `library_source`; testes.

## 5. Critérios de aceite

- [ ] `POST` com diretório válido cria o datasource e retorna `201` + registro.
- [ ] `POST` com path inexistente, arquivo-não-diretório ou sem permissão → `422` com mensagem clara; nada criado.
- [ ] `POST` com `path` já cadastrado → `409`.
- [ ] `GET` lista os datasources com `state` reavaliado (um path removido do disco aparece `offline`).
- [ ] `DELETE` remove o registro (`204`); id inexistente → `404`.
- [ ] Escritas passam pela conexão única; testes de handler passam; catraca verde.

## 6. Notas de implementação / decisões

- **Normalização de path:** guardar absoluto e limpo (`filepath.Clean`/`Abs`) para
  o UNIQUE do [0007](0007-schema-nucleo-minimo.md) casar e evitar duplicata mascarada.
- **`state` na leitura vs. persistido:** avaliar por stat **na leitura** é barato e
  sempre atual; persistir `offline` só faria sentido para o scanner suspender — deixar
  a persistência do estado offline para o [0009](0009-scanner-fswalk-ffprobe-matching.md),
  que é quem age sobre isso.
- **DELETE não purga entities** aqui: purga é operação destrutiva explícita ([04 —
  ciclo de vida](../docs/04-modelo-de-dados.md)); remover o datasource só tira a
  fonte. O destino das entities órfãs daí é follow-up (fila de limpeza).
- **Padrão de erro e rota** definidos aqui viram a **referência** para todo `/api/v1`;
  registrar o shape para o gerador OpenAPI do [0010](0010-api-leitura-resolver-minimo.md).

## 7. Fora deste briefing / follow-ups

- Editar datasource (path/tipo) e re-scan agendado.
- Purga em cascata / fila de limpeza ao remover uma fonte.
- Auth e permissões por datasource (Fase 4).
