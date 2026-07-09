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
| **Status** | draft |
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
- **GET `/api/v1/search?q=&type=`** → `200 { local: [...], external: [ { source, source_id, title, year, poster_url, saved:false } ] }` (shape a fechar na implementação; envelope de erro padrão do [0008](0008-datasources-cadastro-api-library.md)).

### Backend (Go)
- `internal/provider` (interface + registro) e `internal/provider/tmdb` (cliente).
- **Chave** lida de config/env no boot; ausência → busca local funciona, externa
  degrada com aviso (nunca quebra a rota).
- Cache local do payload de busca (curto) para rate-limit; sem criar entidade.

### Frontend (Vite/React)
- Nada aqui além dos tipos gerados; a **UI de busca** vive com o import no [0015](0015-enriquecimento-import-proxy-imagem.md).

### Arquivos afetados
- Criar: `internal/provider/*`, handler de `/search`, config da chave, testes.
- Alterar: montagem de rotas; `openapi.yaml` (+ regen dos tipos TS).

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

## 7. Fora deste briefing / follow-ups

- Providers adicionais por vertical; universal scraper vs escolha explícita da
  fonte; `/api/tmdb/*` (Fase 4).
