# 0092 — Fundação do histórico de consumo: schema genérico + API mínima

> Cria a base de todo o épico de tracking (ADR-0022): uma tabela de eventos
> genérica (`historico_evento`), agnóstica de vertical desde o schema, com API
> para registrar e consultar eventos manualmente. Sem webhook e sem UI ainda —
> isso são os próximos briefings do mesmo épico (0093/0094).

| | |
|---|---|
| **ID** | 0092 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 5+ — Verticais e plugins |
| **Status** | ready |
| **Docs relacionadas** | [04-modelo-de-dados](../docs/04-modelo-de-dados.md), [ADR-0022](../docs/adr/0022-historico-de-consumo-eventos-genericos-e-trackerprovider.md) |
| **Depende de** | — |

---

## 1. Objetivo

O dono pediu um sistema de tracking/marcos agnóstico a tipo de mídia e a
cliente de origem (inspirado em Trakt/Last.fm/ListenBrainz/RetroAchievements e
afins — pesquisa registrada no ADR-0022). Hoje o único precedente é passivo e
pobre: seis colunas soltas em `entity` (migration 0019), preenchidas só por
releitura pontual do `.nfo`, sem histórico real (1 valor, nunca N eventos),
sem porta de entrada em tempo real e sem API de escrita.

Este briefing entrega a fundação: uma tabela de eventos genérica que qualquer
vertical (filme, série, e no futuro música/livro/jogo) usa sem migration nova,
mais os endpoints mínimos para registrar um evento e consultar o histórico e o
estado atual (derivado) de uma entity. É a base sobre a qual o webhook de
player (0093) e a UI (0094) são construídos.

## 2. Jornada do usuário

1. O dono (ou, depois do 0093, um webhook automático) registra que começou a
   assistir um filme: `POST /entities/{id}/historico` com
   `{"kind":"assistir","status":"started","progress_percent":0}`.
2. Conforme avança, novos eventos são registrados (`status: paused` ao
   pausar, `status: stopped` com `progress_percent` atualizado ao parar).
3. A qualquer momento, `GET /entities/{id}/historico` mostra a lista de
   eventos em ordem cronológica reversa; `GET /entities/{id}/historico/estado`
   mostra o resumo derivado (ex.: `{"assistir":{"status":"completed",
   "progress_percent":100,"occurred_at":"..."}}`), sem o consumidor precisar
   calcular "qual foi o último evento" na mão.
4. Se o dono registrar um evento errado (fat-finger, teste), pode apagá-lo
   (`DELETE /entities/{id}/historico/{evento_id}`) — correção é sempre
   possível, autoridade do dono (princípio 1).
5. Nada disso tem UI ainda neste briefing — é fundação de API/schema; a
   jornada visual chega no 0094.

## 3. Escopo

**Dentro:**
- Migration nova: tabela `historico_evento`, filha de `entity`
  (`ON DELETE CASCADE`), append-only, campos `kind`/`status`/`source` como
  TEXT livre (sem CHECK fechado).
- Queries sqlc: criar evento, listar histórico por entity (com filtro
  opcional por `kind`), obter o último evento por `(entity_id, kind)` (base
  da projeção de "estado atual"), apagar evento por id.
- Endpoints REST:
  - `POST /api/v1/entities/{id}/historico` — cria evento.
  - `GET /api/v1/entities/{id}/historico` — lista eventos (filtro `?kind=`
    opcional), ordem cronológica reversa.
  - `GET /api/v1/entities/{id}/historico/estado` — resumo derivado: último
    evento por `kind` presente no histórico daquela entity.
  - `DELETE /api/v1/entities/{id}/historico/{eventoId}` — remove um evento.
- Contrato OpenAPI (`openapi.yaml`) + geração de tipos do frontend
  (`gen:types`), mesmo sem UI consumindo ainda (mantém o contrato como fonte
  única, convenção já usada em outros briefings de fundação).
- Validação de entrada: `kind`/`status` não-vazios; `progress_percent` (se
  presente) em `[0, 100]`; `occurred_at` (se ausente no corpo, default =
  timestamp do servidor no momento da chamada).
- Testes cobrindo: criação, listagem, filtro por `kind`, derivação do estado
  (último evento por kind vence, inclusive quando há eventos de kinds
  diferentes intercalados), remoção, 404 para entity inexistente,
  `ON DELETE CASCADE` ao apagar a entity.

**Fora (explicitamente):**
- Qualquer webhook de player (Jellyfin/Plex/Emby) — briefing 0093.
- Qualquer UI (badge de assistido, timeline de histórico, botão manual) —
  briefing 0094.
- Capacidade `TrackerProvider` (import/export com Trakt ou similar) — fica
  desenhada no ADR-0022, sem implementação aqui.
- Qualquer vertical além de filme/série sendo exercitada de ponta a ponta —
  o schema É agnóstico, mas esta entrega só é validada/testada com `entity`
  de filme/série (as que já existem na base hoje).
- Unificar com a migration 0019 (`watched`/`play_count`/`resume_*`) — os dois
  mecanismos convivem sem sincronização, por decisão do ADR-0022.
- "Marco"/achievement (catálogo de conquistas navegável, ao estilo
  RetroAchievements/Steam) — modelagem correta é `entity`+`entity_relation`+
  `asset` para a DEFINIÇÃO (fora deste briefing) e `historico_evento` só para
  o EVENTO de desbloqueio (`entity_id` apontando para a conquista, já
  suportado pelo schema deste briefing sem alteração nenhuma). Ver ADR-0022
  (revisão H5) — nenhuma migration extra necessária quando essa vertical
  entrar, mas o import de catálogo (`MarcoProvider`) e a UI ficam para outro
  briefing.
- Paginação de `GET /historico` — biblioteca pessoal, volume baixo; lista
  completa por ora (paginação vira follow-up se necessário na prática).

## 4. Contrato técnico

### Dados / modelo

Nova migration `internal/db/migrations/0020_historico_evento.sql`:

```sql
-- +goose Up
-- +goose StatementBegin
CREATE TABLE historico_evento (
    id                TEXT PRIMARY KEY,
    entity_id         TEXT NOT NULL REFERENCES entity (id) ON DELETE CASCADE,
    kind              TEXT NOT NULL,
    status            TEXT NOT NULL,
    progress_percent  REAL,
    position_seconds  REAL,
    occurred_at       TEXT NOT NULL,
    source            TEXT NOT NULL DEFAULT 'manual',
    payload_json      TEXT,
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_historico_evento_entity_kind
    ON historico_evento (entity_id, kind, occurred_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE historico_evento;
-- +goose StatementEnd
```

Nenhum campo novo em `entity`. `kind`/`status`/`source` sem `CHECK` (livre,
princípio 3 do `AGENTS.md`) — validação de vocabulário (se necessária) fica na
camada de aplicação, não no schema, para não travar vertical futura.

### API

Namespace `/api/v1`, seguindo o padrão REST já usado por `asset`/`credit`.

- **`POST /entities/{id}/historico`**
  - Request: `{"kind": string, "status": string, "progress_percent"?: number, "position_seconds"?: number, "occurred_at"?: string (RFC3339), "source"?: string, "payload"?: object}`.
  - `source` ausente → `"manual"`. `occurred_at` ausente → timestamp do
    servidor no momento da chamada.
  - 201 com o evento criado (mesmo shape do `GET`). 404 se a entity não
    existir. 422 se `kind`/`status` vazios ou `progress_percent` fora de
    `[0,100]`.
- **`GET /entities/{id}/historico?kind=`**
  - Response: `{"eventos": [{"id","kind","status","progress_percent",
    "position_seconds","occurred_at","source","payload","created_at"}, ...]}`,
    ordenado por `occurred_at DESC`. `kind` filtra quando presente. 404 se a
    entity não existir.
- **`GET /entities/{id}/historico/estado`**
  - Response: `{"estado": {"<kind>": {"status","progress_percent",
    "position_seconds","occurred_at","source"}, ...}}` — um objeto por `kind`
    distinto já visto no histórico daquela entity, com o evento mais recente
    daquele `kind`. Objeto vazio (`{}`) se não há histórico. 404 se a entity
    não existir.
- **`DELETE /entities/{id}/historico/{eventoId}`**
  - 204 em sucesso. 404 se entity ou evento não existirem (ou evento não
    pertence à entity).

### Backend (Go)

- `internal/db/queries/historico_evento.sql` (novo arquivo, **ASCII puro** —
  sqlc v1.31 corrompe acento em query `.sql`): queries `CriarHistoricoEvento`,
  `ListarHistoricoEvento` (com `sqlc.narg('kind')` opcional), `UltimoEventoPorKind`
  (uma linha por `kind` mais recente — via `MAX(occurred_at)`/window function
  ou N chamadas agrupadas em Go, o que for mais simples de manter), `ApagarHistoricoEvento`,
  `BuscarHistoricoEventoPorID`.
- Novo pacote/arquivo de handler `internal/server/historico.go` (ou dentro de
  `entities.go`, a critério de quem implementar — mas preferir arquivo próprio
  dado que o épico vai crescer com 0093/0094 no mesmo domínio): os 4
  endpoints acima, reaproveitando o padrão de validação/404 já usado em
  `internal/server/asset.go`/`credit.go`.
- Derivação do "estado" (`GET .../estado`): agrupar os eventos retornados por
  `kind`, pegar o de `occurred_at` mais recente por grupo — implementar em Go
  sobre o resultado de uma query simples é aceitável (volume baixo por
  entity); não força SQL avançado se não compensar.
- `openapi.yaml`: novos paths/schemas para os 4 endpoints; rodar `gen:types`
  no fim.

### Frontend (Vite/React)

- Nenhuma tela nova. Só os tipos gerados (`gen:types`) para os novos
  endpoints ficarem disponíveis para o briefing 0094 consumir depois — não
  há componente/rota tocada aqui.

### Arquivos afetados

- `internal/db/migrations/0020_historico_evento.sql` (novo).
- `internal/db/queries/historico_evento.sql` (novo).
- `internal/db/sqlcgen/*` (gerado).
- `internal/server/historico.go` (novo) + roteamento em
  `internal/server/router.go` (ou equivalente).
- `internal/server/historico_test.go` (novo).
- `openapi.yaml` + `web/src/api/*` gerado (`gen:types`).
- `docs/04-modelo-de-dados.md`: acrescentar `historico_evento` ao esboço de
  tabelas e ao diagrama ER (mermaid).

## 5. Critérios de aceite

- [ ] Migration `0020_historico_evento.sql` aplica e reverte limpo
      (`goose up`/`down`); `schema_nucleo_test.go` (ou equivalente) atualizado
      para a nova versão de schema.
- [ ] `POST /entities/{id}/historico` cria um evento e retorna 201 com o
      shape completo; `source` default `"manual"` quando omitido;
      `occurred_at` default = agora quando omitido.
- [ ] `POST` com `progress_percent` fora de `[0,100]` retorna 422 sem criar
      linha.
- [ ] `GET /entities/{id}/historico` lista todos os eventos da entity em
      ordem `occurred_at DESC`; `?kind=X` filtra corretamente.
- [ ] `GET /entities/{id}/historico/estado` devolve, para cada `kind` já
      visto, exatamente o evento de `occurred_at` mais recente daquele
      `kind` — testado com eventos intercalados de kinds diferentes fora de
      ordem de inserção.
- [ ] `DELETE /entities/{id}/historico/{eventoId}` remove o evento; um
      segundo `DELETE` do mesmo id retorna 404.
- [ ] Apagar a `entity` remove em cascata todo o `historico_evento` dela
      (`ON DELETE CASCADE`, testado).
- [ ] Todos os 4 endpoints retornam 404 para `entity_id` inexistente.
- [ ] `go build ./... && go vet ./... && go test ./... -count=1` verde.
- [ ] `tsc -b` verde após `gen:types` (mesmo sem UI nova consumindo).
- [ ] `make gate-local` passa.

## 6. Notas de implementação / decisões

- **Por que tabela nova em vez de estender a migration 0019?** A 0019 é 1
  valor por campo (mutável, sobrescrito), não histórico; e é lida
  passivamente do `.nfo`. Um sistema de tracking de verdade precisa de N
  eventos por entity ao longo do tempo — são modelos de dado
  fundamentalmente diferentes. Ver ADR-0022 para o detalhamento e a decisão
  explícita de não unificar agora.
- **Por que `kind`/`status`/`source` sem `CHECK`?** Fechar o vocabulário
  hoje (ex. `CHECK (kind IN ('assistir','ouvir',...))`) reintroduziria
  exatamente o acoplamento por-vertical que o princípio 3 do `AGENTS.md`
  proíbe — uma vertical nova (música, livro) precisaria de migration só para
  adicionar um `kind` novo. Vocabulário sugerido documentado aqui e no
  ADR-0022, não imposto pelo schema.
- **Por que `estado` é derivado em vez de mantido como coluna?** Manter um
  "estado atual" como coluna redundante exigiria escrevê-lo toda vez que um
  evento é inserido/apagado, com risco real de divergência (exatamente o
  problema que motivou tratar `watched`/`play_count` como legado a não
  estender). Derivar do histórico é mais lento por request (aceitável no
  volume de uma biblioteca pessoal) mas nunca mente.
- **Por que `DELETE` de evento é permitido, se o princípio 5 é
  "não-destrutivo"?** O princípio veda apagar dado do dono
  *automaticamente*; remoção explícita e pontual de um evento errado
  (fat-finger, teste) é o dono corrigindo o próprio histórico — mesma lógica
  já aplicada a `asset`/`credit` custom, que também podem ser removidos pelo
  dono. Não há purga em massa aqui.
- **`entity_id` de um evento não precisa ser um item "de mídia".** O schema
  não distingue: um `historico_evento` pode apontar tanto para um filme/
  série quanto para qualquer outra `entity`, inclusive uma futura entity
  `type='conquista'` (o desbloqueio de um "marco" — ver ADR-0022, revisão
  H5). Não é preciso nenhuma alteração aqui para esse caso funcionar; é só
  não presumir, na implementação, que `entity_id` sempre resolve para um
  item catalogável de filme/série.

## 7. Fora deste briefing / follow-ups

- Webhook receptor de Jellyfin (e depois Plex/Emby) normalizando payload de
  reprodução para `historico_evento` — briefing 0093.
- UI de estado/histórico no detalhe do item (badge de assistido, timeline,
  ação manual de "marcar como assistido") — briefing 0094.
- Capacidade `TrackerProvider` concreta (import/export com Trakt ou
  ListenBrainz) — desenhada no ADR-0022, sem briefing aberto ainda.
- Verticais de música/livro/jogo exercitando `historico_evento` de ponta a
  ponta — reaproveita o mesmo schema, sem migration nova, quando a vertical
  correspondente existir no projeto.
- "Marco"/achievement (catálogo navegável ao estilo RetroAchievements/Steam):
  entities filhas `type='conquista'` (definição) + `historico_evento` de
  desbloqueio (evento) + nova capacidade `MarcoProvider` (import) + UI de
  navegação — modelo fechado no ADR-0022 (revisão H5), sem briefing de
  implementação aberto ainda.
- Identidade de vertical baseada em hash de arquivo (ex. ROM), reaproveitando
  `media_file.content_hash` já existente, incluindo o fluxo de correção
  manual para hash não reconhecido e override de condição de desbloqueio por
  hash (fan-translation/hack) — desenhado no ADR-0022, sem briefing aberto.
