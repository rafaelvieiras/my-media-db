# 0015 — Enriquecimento (import explícito) + proxy/cache de imagem

> O **import explícito**: puxar o detalhe de um resultado externo e persisti-lo como
> camada `media_external` (base editorial), gravar a identidade em `external_ids`, e
> **cachear as imagens em disco** — sem tocar no override.

| | |
|---|---|
| **ID** | 0015 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Docs relacionadas** | [07-enriquecimento-providers](../docs/07-enriquecimento-providers.md), [08-artwork-e-assets](../docs/08-artwork-e-assets.md), [ADR-0011](../docs/adr/0011-external-ids-autoritativo-media-external-cache.md), [ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md) |
| **Status** | done |
| **Depende de** | [0014](0014-provider-tmdb-busca-local-first.md) |

---

## 1. Objetivo

A busca ([0014](0014-provider-tmdb-busca-local-first.md)) acha; falta **puxar** e
**persistir** — o passo que enche a camada externa que o resolver ([0013](0013-resolver-tres-camadas.md))
compõe. Segue **pull > sync** e **import explícito** ([07](../docs/07-enriquecimento-providers.md)):
nada sincroniza sozinho; o dono decide o que salvar. É aqui que um `draft`
não-identificado ou um item pobre ganha título, sinopse e poster.

## 2. Jornada do usuário

1. Na busca, o dono escolhe um resultado do TMDB e clica **Importar**.
2. O sistema puxa o detalhe, grava a base externa e o item passa a exibir os dados
   ricos (via resolver) — poster incluso, servido do **cache local**.
3. Re-enriquecer depois atualiza só a base externa; o que o dono editou
   ([0016](0016-editor-override-ui.md)) permanece intacto (não-destrutivo).

## 3. Escopo

**Dentro:**
- **`POST /api/v1/enrich`** (ou `/entities/{id}/enrich`) — puxa o detalhe do provider
  e **grava `media_external`** (payload cru), atualizando `entity.external_ids`
  (identidade autoritativa, MD4/[ADR-0011](../docs/adr/0011-external-ids-autoritativo-media-external-cache.md)).
- **Mapeamento** do payload TMDB → campos da `Entity`/base externa (título, sinopse,
  data, gêneros…); **sem** elenco/equipe (MD6 adiado).
- **Proxy/cache de imagem** ([08](../docs/08-artwork-e-assets.md)): baixar poster/fanart
  sob demanda, gravar **binário em disco** e um `asset` (`origin=external`) apontando
  para ele; servir do cache depois (não depender da fonte).
- **Não-destrutivo** ([ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md)):
  re-enriquecer atualiza `media_external`; nunca mexe em `media_override` nem em
  artes `origin=user`.
- Testes: import grava external + external_ids; re-import não apaga override; cache
  de imagem grava em disco e serve o binário.

**Fora (explicitamente):**
- **Elenco/equipe** (person/credit, MD6) — briefing próprio.
- **Upload** de arte custom (`origin=user`) e a escolha de arte preferida — [0017](0017-arte-custom-upload-ui.md).
- **Modo `promote_draft`/`hidden_cache`** do passthrough `/api/tmdb` — Fase 4 ([ADR-0015](../docs/adr/0015-api-tmdb-passthrough.md)).

> **Implementação em duas passadas** (uma unidade de briefing, dois PRs): **(A)**
> backend — enrich/import + cache de imagem + fiação da camada externa no resolver
> (validável por curl); **(B)** UI de busca/import (validável por Playwright).

## 4. Contrato técnico

### API (`/api/v1`)
- **POST `/entities/{id}/enrich`** — enriquece uma entity **existente**. Body opcional
  `{ "source":"tmdb", "source_id":"..." }`; se omitido, usa `entity.external_ids[source]`
  (default `tmdb`); sem id disponível → `422 invalid_body`. `200` com a entity resolvida.
- **POST `/import`** — body `{ "source":"tmdb", "source_id":"...", "type":"movie"|"tv" }`.
  **Find-or-create por id externo** (dedup, MD4): se já existe entity com aquele
  `external_id_tmdb`, reusa; senão cria (`status=cataloged`, título inicial do detalhe).
  Depois enriquece pelo mesmo caminho. `201` se criou / `200` se já existia, com a
  entity resolvida. Erros no envelope padrão.

### Dados / modelo
- **`media_external.payload_json` guarda o RAW** do detalhe da fonte (ADR-0011: cache
  cru, para re-compor/re-puxar). Upsert por `(entity_id, source)` ([0012](0012-schema-fase3-camadas-externo-override.md)).
  O **mapeamento raw→canônico é do provider** (o resolver não conhece o TMDB) e roda
  na leitura (fiação abaixo).
- Atualiza `external_ids_json` (e as colunas geradas `external_id_tmdb/imdb`) —
  identidade autoritativa (MD4).
- **`asset` `origin=external`** por imagem cacheada (**poster** no MVP; fanart/backdrop
  = follow-up). Binário em disco sob `MYMEDIADB_ASSETS_DIR`
  (default `dirname(DB)/assets`), layout `{assetsDir}/{entity_id}/poster.<ext>`.

### Backend (Go)
- **Provider `+= BuscarDetalhe(ctx, sourceID, tipo) (json.RawMessage, error)`** e
  **`MapearDetalhe(raw) (map[string]any, error)`** (canônico: título, sinopse, data,
  `tags`/gêneros, `poster_url`; **sem** elenco/equipe — MD6 adiado). TMDB implementa.
- **Fiação da camada externa no resolver (a adiada do [0013](0013-resolver-tres-camadas.md)):**
  `getEntity` carrega as linhas de `media_external`, mapeia cada uma `raw→canônico` via
  o provider (por `source`, ordenadas pela prioridade `[tmdb]`) e passa `[]CamadaExterna`
  ao resolver. A composição externa fica **viva de ponta a ponta**.
- **Cache de imagem:** baixa o poster (timeout), grava o binário em disco e cria/renova
  o `asset` `origin=external`; **dedup** (não re-baixa se já cacheado o mesmo). Falha de
  imagem degrada (item sem poster, import não quebra).
- **Serviço de pôster** (`getEntityPoster`, [0010](0010-api-leitura-resolver-minimo.md)):
  estender a guarda de path para servir também de **`MYMEDIADB_ASSETS_DIR`** (dir
  confiável nosso), além dos datasources.
- **Não-destrutivo** ([ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md)):
  enrich faz upsert de `media_external`; **nunca** toca `media_override` nem `asset`
  `origin=user`. Escrita pelo pool de escrita ([ADR-0010](../docs/adr/0010-concorrencia-sqlite-wal-escritor-unico.md)).
- Config `MYMEDIADB_ASSETS_DIR` lida no `main` e injetada no server (option, como o
  provider do 0014).

### Frontend (Vite/React) — passada (B)
- **Tela de busca** (rota nova): input com debounce → `GET /search`; lista **locais**
  (cards) e **externos** ("não salvos"); `external_available:false` → aviso "busca
  externa indisponível" (distinto de "zero resultados").
- **Importar** no resultado externo → `POST /import`; sucesso invalida queries e leva
  ao detalhe; estados loading/erro. O grid/detalhe passam a mostrar o poster cacheado.

### Arquivos afetados
- Criar (A): serviço de enrich + cache de imagem (`internal/enrich` ou similar),
  handlers `/enrich` e `/import`, extensão do provider, testes.
- Alterar (A): `internal/resolver` (nada — só o handler que monta `Camadas.Externo`),
  `internal/server/*` (rotas, guarda do pôster, option de assets dir), `main.go`,
  `openapi.yaml` (+ regen tipos).
- Criar/alterar (B): rota/tela de busca em `web/src/`, `AppHeader`/router, cliente de
  API, testes vitest.

## 5. Critérios de aceite

- [x] Importar um resultado grava `media_external` e atualiza `external_ids`; o resolver passa a servir os dados ricos.
- [x] Poster/fanart são baixados **uma vez**, cacheados em disco como `asset` `origin=external`, e servidos do cache depois.
- [x] Re-enriquecer atualiza a base externa e **não** apaga `media_override` nem artes `origin=user`.
- [x] Falha da fonte/imagem degrada (import parcial claro), sem quebrar. Catraca verde.

## 6. Notas de implementação / decisões

- **Import escreve external, nunca override** — a separação de camadas é o que
  garante A3/B3/G2 ([04](../docs/04-modelo-de-dados.md)): o dono não perde edição ao
  re-scrapear.
- **Identidade vence** (MD4): o import grava/corrige `external_ids`; `media_external`
  é cache subordinado. Divergência → re-busca sob o id correto.
- **Layout do cache de assets** é decisão aberta da [08](../docs/08-artwork-e-assets.md) — fechar aqui o mínimo (diretório + nomeação estável); thumbnails/derivadas ficam fora.
- **`media_external` guarda RAW, mapeia na leitura** (decidido): fiel ao ADR-0011 (cache
  cru) e mantém o resolver genérico — quem sabe o shape do TMDB é o **provider**, que
  faz `raw→canônico` no `getEntity`. Alternativa (gravar canônico) descartada por
  perder o cru e furar o ADR. Custo: um `MapearDetalhe` por leitura (volume baixo, ok).
- **Dois endpoints** (`/entities/{id}/enrich` para existente, `/import` find-or-create):
  o import da busca cria/reusa por id externo (dedup MD4); o enrich cobre o item já
  casado pelo scanner (id no nome). Mesmo caminho interno de enriquecimento.
- **Poster servido de `MYMEDIADB_ASSETS_DIR`**: a guarda do [0010](0010-api-leitura-resolver-minimo.md)
  valida path sob datasource; arte cacheada mora no nosso dir de assets (confiável) —
  a guarda passa a aceitar os dois. Sem isso o pôster externo não seria servível.
- **Grid ainda mostra o título do arquivo** para entities já escaneadas e depois
  enriquecidas (o card não compõe camadas — follow-up do [0013](0013-resolver-tres-camadas.md));
  o detalhe já mostra o composto. Item importado do zero tem título inicial do detalhe,
  então o grid fica correto para ele.

## 7. Fora deste briefing / follow-ups

- Elenco/equipe (MD6); geração de thumbnails/derivadas; fanart/backdrop; universal
  scraper; o passthrough `/api/tmdb` com `promote_draft`/`hidden_cache` (Fase 4).
- **Composição do card na lista** (título enriquecido no grid) — junto do follow-up
  de batch do [0013](0013-resolver-tres-camadas.md).
