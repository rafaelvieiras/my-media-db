# 0014 — Provider TMDB + busca local-first (`/api/v1/search`)

> A abstração de **provider** e o primeiro dela (TMDB), mais a busca **local-first**:
> consulta o catálogo (FTS5) primeiro; só então a fonte externa, cujos resultados
> aparecem marcados como **"não salvos"** até import explícito.

| | |
|---|---|
| **ID** | 0014 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Docs relacionadas** | [07-enriquecimento-providers](../docs/07-enriquecimento-providers.md), [05-api](../docs/05-api.md), [04-modelo-de-dados](../docs/04-modelo-de-dados.md) |
| **Status** | done |
| **Depende de** | [0012](0012-schema-fase3-camadas-externo-override.md), [0013](0013-resolver-tres-camadas.md) |

---

## 1. Objetivo

Para enriquecer, o dono precisa **achar** o item na fonte externa — mas o princípio
é **local-first** ([07](../docs/07-enriquecimento-providers.md)): buscar consulta o
banco primeiro e só então a fonte, sem sujar o catálogo com resultados de pesquisa.
Este briefing entrega a **abstração de provider** (para a visão multi-vertical) e o
**TMDB** como primeiro provider, além da rota de busca que a UI de enriquecimento
([0015](0015-enriquecimento-import-proxy-imagem.md)) consome.

## 2. Jornada do usuário

1. O dono abre a busca e digita um título.
2. Aparecem primeiro os itens **do catálogo** (local, via FTS5 `entity_fts`).
3. Abaixo, resultados **do TMDB** marcados **"não salvos"** — nada é persistido.
4. O dono escolhe um resultado externo para **importar** (fluxo do [0015](0015-enriquecimento-import-proxy-imagem.md)).

## 3. Escopo

**Dentro:**
- **Abstração `Provider`** (busca, fetch de detalhe, mapeamento p/ `Entity`),
  plugável por vertical ([07](../docs/07-enriquecimento-providers.md)).
- **Provider TMDB** (filmes e séries — MVP): cliente HTTP, **chave de API via
  config/env — nunca hardcoded** (regra do projeto; alerta da [07](../docs/07-enriquecimento-providers.md)).
- **Rate-limit e cache** por provider (evita bater a fonte à toa).
- **`GET /api/v1/search`** — **local-first**: FTS5 no catálogo + passthrough ao
  provider; resultados externos marcados **"não salvos"** (não criam entidade).
- Testes com o cliente TMDB **injetável/mockado** (sem rede em teste).

**Fora (explicitamente):**
- **Import** (persistir `media_external`) e **proxy de imagem** — [0015](0015-enriquecimento-import-proxy-imagem.md).
- **`/api/tmdb/*`** (camada compat p/ Jellyfin/Kodi) — **Fase 4** ([05](../docs/05-api.md)); aqui é só o `/api/v1` nativo.
- Outros providers (TVDB, MusicBrainz, IMVDB…) — visão multi-vertical, pós-MVP.

## 4. Contrato técnico

### API (`/api/v1`)
- **GET `/api/v1/search?q=&type=`** (`type` opcional: `movie`|`tv`) →
  `200 { "query": "...", "local": [ <card> ], "external": [ <ext> ], "external_available": bool }`.
  - `<card>` = o mesmo card da lista do [0010](0010-api-leitura-resolver-minimo.md)
    (`id, type, title, status, owned, file_count, match_confidence, poster_url?`).
  - `<ext>` = `{ source:"tmdb", source_id, type, title, year?, overview?, poster_url?, saved:false }`.
    `saved:false` **sempre** (resultado externo nunca é entidade até import).
  - `external_available:false` quando não há credencial ou a fonte falhou → o front
    sabe explicar "busca externa indisponível" sem confundir com "zero resultados".
  - `q` vazio → `400 invalid_body`. Erro no envelope padrão do [0008](0008-datasources-cadastro-api-library.md).

### Backend (Go)
- **`internal/provider`**: interface `Provider` (`Nome() string`;
  `Buscar(ctx, query, tipo) ([]ResultadoExterno, error)`) + `ResultadoExterno`
  canônico. **`internal/provider/tmdb`**: cliente HTTP implementando a interface.
- **Auth (decidido):** credencial lida no boot (main), **preferindo**
  `MYMEDIADB_TMDB_TOKEN` (Read Access Token v4 → header `Authorization: Bearer`);
  se vazio, `MYMEDIADB_TMDB_API_KEY` (v3 → query `api_key`). Nenhuma das duas →
  provider **desligado** (não é erro): busca local funciona, `external_available:false`.
  **Nada de segredo no código/repo.** Injetado no `server.New` via option (assinatura
  atual `New(version, database)` preservada; provider é opcional).
- **Endpoints TMDB:** base `https://api.themoviedb.org/3`; busca via `/search/multi`
  filtrando `media_type ∈ {movie, tv}` (mapeado para o nosso `type`). `poster_url`
  externo = URL direta do TMDB (`https://image.tmdb.org/t/p/w342` + `poster_path`);
  o **proxy/cache** de imagem é do [0015](0015-enriquecimento-import-proxy-imagem.md).
- **Local-first:** busca no `entity_fts` (MATCH **sanitizado** — input do usuário não
  pode quebrar a sintaxe FTS5), junta aos cards; roda **sempre**, independente do
  externo. Cache TTL curto em memória do resultado externo por `(query,tipo)` para
  poupar a fonte; volume é baixo (self-hosted), sem limiter elaborado.
- Timeout no cliente HTTP; falha/timeout do TMDB → `external:[]`,
  `external_available:false`, **nunca 5xx** por causa do externo.

### Frontend (Vite/React)
- Nada aqui além dos tipos gerados; a **UI de busca** vive com o import no [0015](0015-enriquecimento-import-proxy-imagem.md).

### Arquivos afetados
- Criar: `internal/provider/provider.go` (interface + tipos), `internal/provider/tmdb/*`,
  handler `/search` + query FTS local (sqlc, ASCII), testes.
- Alterar: `internal/server/server.go` (rota `/search` + option de provider), `main.go`
  (ler credencial do env e injetar), `openapi.yaml` (+ regen tipos), **`compose.dev.yaml`**
  (adicionar `env_file: [.env]` ao serviço `api` para o container enxergar a credencial —
  seguro: `.env` é gitignored).

## 5. Critérios de aceite

- [ ] `Provider` é uma interface; TMDB a implementa; trocar/adicionar provider não toca o handler de busca.
- [ ] A chave do TMDB vem de config/env; **não** há segredo no código nem no repo.
- [ ] `GET /search` devolve locais (FTS5) e externos marcados **"não salvos"**; busca **não** cria entidade.
- [ ] Falha/ausência da fonte externa degrada (só locais) sem quebrar a rota; rate-limit respeitado.
- [ ] Testes rodam sem rede (cliente injetado). Catraca verde.

## 6. Notas de implementação / decisões

- **Local-first é regra, não otimização** ([07](../docs/07-enriquecimento-providers.md)):
  a busca externa nunca persiste — só o import do [0015](0015-enriquecimento-import-proxy-imagem.md) escreve.
- **`/api/tmdb` é outra coisa**: aquele é passthrough compat (Fase 4, [ADR-0015](../docs/adr/0015-api-tmdb-passthrough.md));
  este é a busca nativa do dono. Não confundir os namespaces.
- Gestão de chave/rate-limit por provider é decisão aberta da [07](../docs/07-enriquecimento-providers.md) — fechar o mínimo aqui (env + backoff simples).
- **Token v4 preferido à key v3** (decidido): o Read Access Token é o que o TMDB
  destaca hoje e cobre todos os endpoints de leitura que usamos; a key v3 fica de
  alternativa para quem só a tem. O código aceita as duas — o dono preenche a que tiver.
- **`/search/multi` filtrado** (não `/search/movie`+`/search/tv` separados): uma
  chamada, mapeando `media_type`; descarta `person` e outros. Simples e suficiente p/ MVP.
- **`external_available` explícito** no shape: separar "fonte indisponível" de "zero
  resultados" é o que deixa a UI do [0015](0015-enriquecimento-import-proxy-imagem.md) honesta.
- **Config base hardcoded** (base da API e de imagem do TMDB são estáveis) — evita uma
  chamada a `/configuration` no MVP; se mudar, vira config depois.

## 7. Fora deste briefing / follow-ups

- Providers adicionais por vertical; universal scraper vs escolha explícita da
  fonte; `/api/tmdb/*` (Fase 4).
