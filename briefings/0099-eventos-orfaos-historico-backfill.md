# 0099 — Eventos de histórico órfãos + backfill retroativo

> Quando um evento de webhook (Jellyfin, e futuros players) não casa com
> nenhuma entity local, guarda o evento como "órfão" em vez de descartar — e
> vincula automaticamente, com efeito retroativo, quando a entity ganhar
> aquele ID externo no futuro.

| | |
|---|---|
| **ID** | 0099 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 5+ — Histórico de consumo |
| **Status** | done |
| **Docs relacionadas** | [briefing 0092](0092-fundacao-historico-de-consumo-schema-e-api.md), [briefing 0093](0093-webhook-jellyfin-historico-consumo.md), [briefing 0094](0094-ui-historico-e-estado-assistido.md), [ADR-0022](../docs/adr/0022-historico-de-consumo-eventos-genericos-e-trackerprovider.md), [briefing 0098](0098-guard-esvaziar-external-ids.md) |
| **Depende de** | 0092, 0093 (feitos) |

---

## 1. Objetivo

Hoje, quando o webhook de entrada do Jellyfin (0093) recebe um evento cujos
IDs externos (tmdb/imdb/tvdb) não casam com nenhuma entity local, o evento é
**descartado silenciosamente** (`internal/server/webhook_jellyfin.go`, log
"nenhuma entity casou ... -- descartado", resposta 204 sem persistir nada).
Isso foi observado em produção: um filme já catalogado, mas cuja identidade
(`external_ids`) tinha sido zerada por engano (ver briefing 0098), gerou um
evento de reprodução que simplesmente sumiu — sem registro, sem sinal pro
dono de que algo precisava de atenção.

Esse descarte contraria o princípio 2 do projeto ("o banco é maior que a
biblioteca... cataloga o que se tem *e* o que ainda não se tem"): um evento
de consumo real aconteceu, e o sistema tem informação parcial sobre ele
(pelo menos um ID externo, geralmente um título/ano) — descartar é perder
dado que o dono não pode mais recuperar depois (o Jellyfin não reenvia
webhooks passados). Isso vale tanto para o caso de identidade zerada por
acidente (0098) quanto para o caso genérico e mais comum: o dono assistiu
algo que **ainda não importou/catalogou** no MyMediaDB.

Este briefing muda o comportamento: eventos sem match viram um registro
**órfão** (sem `entity_id`, com os IDs brutos do evento), visíveis no
histórico como "sem item relacionado", e — quando a entity certa aparecer ou
ganhar aquele ID externo — os eventos órfãos correspondentes são
**vinculados retroativamente** (backfill), sem ação manual do dono.

## 2. Jornada do usuário

1. O Jellyfin reporta reprodução de um filme que o MyMediaDB não reconhece
   (não catalogado, ou catalogado mas sem o ID externo daquele evento).
2. Hoje: nada acontece visivelmente. **Depois deste briefing**: o evento é
   gravado como órfão (guarda `kind`, `status`, `occurred_at`, `source` como
   hoje, mais os IDs brutos vindos do payload — tmdb/imdb/tvdb — e, se o
   payload trouxer, título/ano pra exibição).
3. Uma nova seção **"Eventos sem item relacionado"** (tela a definir — ver
   §4) lista esses eventos órfãos: título/ano (se disponível) ou os IDs
   crus, quando ocorreram, de qual fonte vieram (`source='jellyfin'`).
4. Em algum momento, o dono importa/cataloga o filme (fluxo normal de
   import), ou corrige/confirma o match de uma entity já catalogada
   (`/correct`, `/auto-match`, `/confirm-candidate`, ou `PUT
   /external-ids`) de forma que ela passa a ter aquele mesmo ID externo.
5. **Automaticamente**, sem o dono precisar fazer nada a mais: os eventos
   órfãos cujos IDs brutos batem com o novo `external_ids` da entity são
   atualizados (`entity_id` preenchido) e passam a aparecer no histórico
   *daquela* entity (`GET /entities/{id}/historico`) — e somem da lista de
   órfãos.
6. Se dois eventos órfãos diferentes (fontes/IDs diferentes) acabarem
   batendo com a mesma entity ao longo do tempo, ambos são vinculados —
   backfill não é "primeiro a chegar, único a ganhar".

## 3. Escopo

**Dentro:**
- Migration tornando `historico_evento.entity_id` nullable + colunas novas
  para os IDs brutos do evento (ver §4 Dados/modelo).
- `webhook_jellyfin.go`: em vez de descartar quando `casarEntityJellyfin`
  não encontra match, grava o evento com `entity_id = NULL` e os IDs brutos
  disponíveis no payload.
- Função de backfill (`internal/store` ou pacote compartilhado entre
  `server` e `enrich` — ver §6) chamada nos pontos onde `external_ids_json`
  de uma entity é gravado com sucesso E o resultado inclui pelo menos um ID
  novo em relação ao anterior: `external_ids.go` (`updateExternalIds`),
  `enrich.go` (`mesclarExternalIds`, `mesclarExternalIdSeAusente`).
- Endpoint novo para listar órfãos: `GET /api/v1/historico/orfaos`
  (paginado, mesmo padrão de outros GETs de lista do projeto).
- UI mínima pra exibir a lista de órfãos (ver §4 Frontend — escopo
  propositalmente enxuto: só listar, sem ação manual de vincular).
- Testes de integração cobrindo: evento sem match vira órfão; correção de
  entity dispara backfill; evento órfão aparece na lista global e some dela
  após backfill; `GET /entities/{id}/historico` passa a incluir o evento
  vinculado.

**Fora (explicitamente):**
- **Vínculo manual** de um evento órfão a uma entity específica pelo dono
  (ex.: um botão "associar a este item" na lista de órfãos) — o backfill
  automático via match/correção já cobre o caminho principal; vínculo manual
  direto fica para um follow-up se o dono sentir falta.
- Expiração/limpeza automática de órfãos antigos nunca vinculados — não é
  destrutivo por padrão (princípio 5); se algum dia for necessário, é purga
  explícita, outro briefing.
- Suporte a outros players de entrada (Plex/Emby) — este briefing cobre o
  mecanismo genérico (schema + backfill), mas só o Jellyfin (0093) grava
  eventos hoje; qualquer player futuro que grave em `historico_evento`
  ganha órfãos "de graça" pelo mesmo mecanismo, sem trabalho extra.
- Notificação proativa ("você tem N eventos órfãos") — a lista fica
  disponível sob consulta, sem push/badge.

## 4. Contrato técnico

### Dados / modelo

Nova migration `internal/db/migrations/0021_historico_evento_orfao.sql`:

```sql
-- entity_id passa a aceitar NULL (evento sem match); guarda os IDs brutos
-- do evento pra permitir backfill retroativo quando uma entity ganhar
-- aquele ID. SQLite não tem ALTER COLUMN pra remover NOT NULL: recriar a
-- tabela (rebuild) preservando os dados existentes, padrão já usado em
-- migrations anteriores deste projeto (ver migrations 00XX que fizeram
-- rebuild de tabela — conferir um exemplo real no repo antes de implementar
-- pra seguir o mesmo idioma).

ALTER TABLE historico_evento RENAME TO historico_evento_old;

CREATE TABLE historico_evento (
    id TEXT PRIMARY KEY,
    entity_id TEXT REFERENCES entity(id) ON DELETE CASCADE, -- nullable agora
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    progress_percent REAL,
    position_seconds REAL,
    occurred_at TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT 'manual',
    payload_json TEXT,
    raw_tmdb_id TEXT,   -- IDs brutos do evento, usados só quando entity_id
    raw_imdb_id TEXT,   -- é NULL (órfão) -- pra localizar candidatos a
    raw_tvdb_id TEXT,   -- backfill. Ficam preenchidos mesmo após o backfill
                        -- (histórico de onde o evento veio), não são
                        -- limpos ao vincular.
    raw_title TEXT,     -- exibição da lista de órfãos quando disponível
    raw_year TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

INSERT INTO historico_evento SELECT id, entity_id, kind, status,
    progress_percent, position_seconds, occurred_at, source, payload_json,
    NULL, NULL, NULL, NULL, NULL, created_at FROM historico_evento_old;

DROP TABLE historico_evento_old;

CREATE INDEX idx_historico_evento_entity ON historico_evento(entity_id);
CREATE INDEX idx_historico_evento_orfaos
    ON historico_evento(raw_tmdb_id, raw_imdb_id, raw_tvdb_id)
    WHERE entity_id IS NULL;
```

> Conferir na implementação se `historico_evento` já tem os índices
> `idx_historico_evento_entity` de outra migration (0020) — se sim, não
> recriar, só ajustar o que muda de fato.

### API

- `GET /api/v1/historico/orfaos?limit=&offset=` → lista paginada de eventos
  com `entity_id IS NULL`, ordenada por `occurred_at desc`. Shape do item:
  `{id, kind, status, occurred_at, source, raw_title, raw_year,
  raw_external_ids: {tmdb?, imdb?, tvdb?}}`.
- `GET /entities/{id}/historico` (já existe, 0092/0093) — sem mudança de
  contrato; passa a incluir naturalmente eventos que foram vinculados via
  backfill (mesma query, já filtra por `entity_id`).

### Backend (Go)

- `internal/server/webhook_jellyfin.go`: no branch `if !ok` de
  `casarEntityJellyfin` (hoje ~linha 166-172), trocar o descarte por
  `queries.CriarHistoricoEventoOrfao(ctx, ...)` com os IDs brutos extraídos
  do payload do Jellyfin (os mesmos `tmdb`/`imdb`/`tvdb` que já são
  parseados hoje pra tentar o match) e `raw_title`/`raw_year` se o payload
  do Jellyfin trouxer (conferir na implementação quais campos o payload
  expõe — provavelmente `Name`/`Year` ou similar).
- Função de backfill nova, ex. `internal/store/historico_backfill.go` (pacote
  a decidir na implementação — precisa ser importável tanto por `server`
  quanto por `enrich` sem criar ciclo; se `enrich` já importa `store`/`db`,
  colocar lá é o caminho mais simples):
  ```go
  // VincularEventosOrfaos vincula ao entityID todo evento órfão cujos IDs
  // brutos batem com externalIDs (mapa fonte->id, ex. o mesmo shape de
  // entity.external_ids). Roda um UPDATE por fonte presente no mapa.
  func VincularEventosOrfaos(ctx context.Context, q *sqlcgen.Queries, entityID string, externalIDs map[string]string) error
  ```
  Chamada nos 3 pontos que gravam `external_ids_json` (ver Escopo) **depois**
  que a gravação foi bem-sucedida, só com as fontes que de fato mudaram
  (evitar rodar a query de backfill sem necessidade quando nada novo foi
  adicionado — ex. `mesclarExternalIdSeAusente` só preenche ausentes, então
  sempre há algo novo para verificar quando o merge muda algo).

### Frontend (Vite/React)

- Uma seção/tela nova pra listar órfãos — reusar o padrão de lista simples
  já usado em outras telas do dashboard (ex. `settings.tsx`/`webhook.tsx`),
  **sem** reusar o componente rico de histórico por-entity do 0094 (que
  assume uma entity concreta pra renderizar poster/link). Decisão de
  localização (rota nova `/dashboard/historico-orfaos`? seção dentro de uma
  tela de histórico geral que ainda não existe?) fica para a implementação
  avaliar contra o estado atual do 0094 — se o 0094 já criou uma tela de
  histórico geral, a lista de órfãos é uma aba/seção dela; se não, cria
  `/dashboard/historico-orfaos` como tela própria, seguindo o padrão de rota
  do 0097.
- `web/src/lib/api.ts`/`queries.ts`: wrapper + `queryOptions` pra `GET
  /historico/orfaos`.

### Arquivos afetados

- `internal/db/migrations/0021_historico_evento_orfao.sql` (novo)
- `internal/db/queries/historico.sql` (ou arquivo equivalente do 0092/0093 —
  novas queries: criar órfão, listar órfãos, vincular por IDs brutos)
- `internal/server/webhook_jellyfin.go`
- `internal/enrich/enrich.go`
- `internal/server/external_ids.go`
- `internal/server/server.go` (rota nova)
- `openapi.yaml` (endpoint novo + shape de resposta)
- Frontend: tela/rota nova + `api.ts`/`queries.ts` (arquivos exatos a
  decidir na implementação, dependendo do que o 0094 já tiver construído)

## 5. Critérios de aceite

- [x] Evento de webhook sem match vira uma linha em `historico_evento` com
      `entity_id = NULL` e os IDs brutos preenchidos, em vez de ser
      descartado.
- [x] `GET /api/v1/historico/orfaos` lista esses eventos, paginado.
- [x] Corrigir/confirmar o match de uma entity (`/correct`, `/auto-match`,
      `/confirm-candidate` ou `PUT /external-ids`) para um ID que bate com
      algum evento órfão vincula automaticamente esse evento (`entity_id`
      preenchido) — sem ação manual adicional. (Os 4 caminhos convergem em
      `enrich.Enriquecer`→`mesclarExternalIds`/`mesclarExternalIdSeAusente`
      ou em `updateExternalIds`, todos chamando
      `internal/historico.VincularOrfaos`.)
- [x] Evento vinculado por backfill passa a aparecer em `GET
      /entities/{id}/historico` e some da lista de órfãos.
- [x] Dois eventos órfãos com fontes de ID diferentes, ambos batendo com a
      mesma entity ao longo do tempo, são ambos vinculados (não é
      "primeiro ganha"). (`TestVincularOrfaosDuasFontesMesmaEntity`.)
- [x] Nenhuma regressão no fluxo existente de histórico por-entity (0092/
      0093/0094) — eventos que já casam hoje continuam se comportando
      igual.
- [x] `go build ./...`; `go vet ./...`; `go test ./... -count=1`; `tsc -b`;
      `npm run lint`; `npm run test:coverage` sem regressão; `make
      gate-local` limpo antes do commit final.

## 6. Notas de implementação / decisões

- **Por que não reusar `entity_relation`/`extension_data`**: ambos exigem
  uma entity dona (relation liga duas entities existentes; extension_data é
  namespaced por entity) — um evento órfão por definição não tem entity
  ainda. Ficar dentro da própria tabela `historico_evento` com FK nullable é
  mais simples e não introduz um conceito novo no core (a tabela já existe,
  só ganha capacidade de representar "sem dono ainda" — coerente com o
  princípio 2 do projeto).
- **Backfill em 3 call sites, não um hook central**: hoje não existe uma
  função única que todo fluxo de atualização de `external_ids_json` passa
  por cima — `external_ids.go` e `enrich.go` são pacotes diferentes sem uma
  camada compartilhada de "pós-gravação de identidade". Introduzir esse hook
  central seria um refactor maior, fora do escopo deste briefing; a
  duplicação de 3 chamadas explícitas é aceitável aqui (função pequena,
  comportamento idêntico nas 3).
- **IDs brutos não são limpos após o backfill** — ficam como registro de
  proveniência do evento (de onde ele veio originalmente), mesmo já
  vinculado. `entity_id` é a fonte de verdade de "está vinculado ou não";
  os `raw_*` nunca mudam depois de gravados.
- **Por que não vínculo manual nesta entrega**: o caminho automático
  (corrigir/confirmar o match da entity) já é a ação natural que o dono faria
  de qualquer forma para catalogar o item; adicionar um segundo caminho
  manual (escolher a entity direto na lista de órfãos) é redundante para o
  MVP e pode ser um follow-up se o backfill automático não cobrir algum caso
  na prática.

## 7. Fora deste briefing / follow-ups

- Vínculo manual de evento órfão a uma entity (se o automático não for
  suficiente na prática).
- Purga/expiração de órfãos antigos.
- Extensão do mesmo mecanismo para outros players de entrada quando
  existirem (Plex/Emby) — nenhum trabalho adicional esperado além do que
  esses briefings futuros já precisariam fazer para gravar em
  `historico_evento`.
