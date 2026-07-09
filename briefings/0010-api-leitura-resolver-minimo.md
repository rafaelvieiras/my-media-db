# 0010 — API de leitura + resolver mínimo (camada arquivo)

> As rotas `/api/v1/entities` que a UI consome: listar o catálogo detectado, ver o
> detalhe de um item e seus arquivos com mediainfo, servir o poster. Um resolver
> ainda de **uma camada** (arquivo) — override/externo entram na Fase 3.

| | |
|---|---|
| **ID** | 0010 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 2 — UI de biblioteca |
| **Docs relacionadas** | [05-api](../docs/05-api.md), [04-modelo-de-dados](../docs/04-modelo-de-dados.md), [ADR-0014](../docs/adr/0014-stack-de-implementacao.md) |
| **Status** | done |
| **Depende de** | [0007](0007-schema-nucleo-minimo.md), [0009](0009-scanner-fswalk-ffprobe-matching.md) |

---

## 1. Objetivo

O scanner ([0009](0009-scanner-fswalk-ffprobe-matching.md)) enche o banco; falta
**servir** isso para a UI. Este briefing entrega a superfície de leitura de
`/api/v1/entities`: lista (grid/lista), detalhe e arquivos+mediainfo, além de servir
o poster. Tudo passa por um **resolver**, mesmo que na Fase 2 ele componha só a
camada **arquivo** (não há override nem externo ainda) — assim a forma da resposta
já é a definitiva ([04](../docs/04-modelo-de-dados.md)/[03](../docs/03-arquitetura.md)),
e a Fase 3 só adiciona camadas por baixo. Também estabelece a **spec OpenAPI** dessas
rotas como fonte dos tipos TS ([ADR-0014](../docs/adr/0014-stack-de-implementacao.md)).

## 2. Jornada do usuário

1. A UI carrega o **grid**: `GET /api/v1/entities` devolve os itens detectados com
   `id, title, type, status, posse, match_confidence` e a **ref do poster**.
2. Filtra por posse ("na biblioteca") ou por tipo; pagina em bibliotecas grandes.
3. Abre um item: `GET /api/v1/entities/{id}` traz a entidade **resolvida**.
4. Vê os **arquivos**: `GET /api/v1/entities/{id}/files` lista cada `media_file` com
   os fatos técnicos do `mediainfo_json` (resolução, codecs, HDR, faixas, duração).
5. O **poster** carrega de `GET /api/v1/entities/{id}/assets/poster` (ou similar),
   servindo o arquivo local descoberto no scan; sem arte, a UI mostra placeholder.

## 3. Escopo

**Dentro:**
- **`GET /api/v1/entities`** — lista paginada; campos de card + **posse derivada**
  (EXISTS `media_file present`, [0007](0007-schema-nucleo-minimo.md)); filtros mínimos
  (`type`, posse) e ordenação por título. Inclui contagem de arquivos e o `match_confidence`
  (para o badge de revisão).
- **`GET /api/v1/entities/{id}`** — entidade **resolvida** pelo resolver mínimo.
- **`GET /api/v1/entities/{id}/files`** — `media_file`s com `mediainfo` (subconjunto
  útil e/ou bruto), `edition`, `part_no`, `state`, tamanho, path.
- **Servir poster** — endpoint que devolve o binário do `asset` local (com content-type
  correto); 404 → a UI cai no placeholder.
- **Resolver (camada única):** função que monta a visão da entity a partir do
  `media_file`/campos do núcleo. Interface preparada para receber `externo`/`override`
  na Fase 3 **sem** mudar a assinatura pública.
- **OpenAPI**: descrever essas rotas; **gerar os tipos TS** consumidos pelo [0011](0011-frontend-grid-lista-detalhe.md).
- Testes de handler (lista/detalhe/files/poster, paginação, filtros, 404).

**Fora (explicitamente):**
- `media_override`/`media_external` e a composição multi-camada — Fase 3
  (o resolver aqui é de uma camada, mas com a forma final).
- Edição de override (`PUT .../override`), upload de arte custom, busca (`/search`),
  enriquecimento (`/enrich`) — Fases 3/4.
- Proveniência por campo na API (a doc 04 diz que **não** se expõe por ora).
- Auth/tokens (Fase 4).

## 4. Contrato técnico

### API (`/api/v1/entities`)
- **GET** `?type=&owned=&limit=&offset=&sort=` → `200 { items:[…], total, limit, offset }`.
  Item: `{ id, type, title, status, owned, file_count, match_confidence, poster_url? }`.
- **GET /{id}** → `200` entidade resolvida | `404`.
- **GET /{id}/files** → `200 { items:[ { id, path, size, edition, part_no, state, mediainfo } ] }`.
- **GET /{id}/assets/poster** (nome a confirmar) → binário `image/*` | `404`.
- Erros no shape padrão definido no [0008](0008-datasources-cadastro-api-library.md).

### Backend (Go)
- `internal/resolver` (mínimo, camada arquivo) + handlers `/api/v1/entities`.
- **Leituras pelo pool de leitura**; nada escreve aqui.
- Servir poster: ler o `asset.path_or_uri` (local), stream com content-type; validar
  que o path pertence a um datasource (evitar path traversal).
- OpenAPI: spec + pipeline de geração de tipos TS (ferramenta a definir no briefing;
  saída consumida pelo front).

### Frontend (Vite/React)
- Nada aqui (é o [0011](0011-frontend-grid-lista-detalhe.md)); mas os **tipos gerados**
  deste briefing são o contrato que ele importa.

### Arquivos afetados
- Criar: `internal/resolver/*.go`, handlers de entities, spec OpenAPI, config de
  geração de tipos, testes.
- Alterar: montagem de rotas; queries sqlc de leitura (lista com posse/contagem, detalhe, files).

## 5. Critérios de aceite

- [ ] `GET /entities` pagina e filtra por `type`/posse; cada item traz posse derivada correta e `match_confidence`.
- [ ] `GET /entities/{id}` retorna a entidade resolvida (camada arquivo) ou `404`.
- [ ] `GET /entities/{id}/files` lista os `media_file` com `mediainfo` (codecs/resolução/HDR/faixas) e `edition`/`part_no`.
- [ ] O endpoint de poster serve a arte local com content-type correto e **bloqueia path traversal**; ausência → `404`.
- [ ] A **spec OpenAPI** cobre as rotas e **gera tipos TS** que compilam no front.
- [ ] Testes de handler passam; leituras não usam a conexão de escrita; catraca verde.

## 6. Notas de implementação / decisões

- **Resolver de uma camada, forma final:** a chave é a **assinatura** — a resposta já
  tem o shape que a Fase 3 vai preencher com override/externo, então a UI não muda
  quando o enriquecimento chegar. Compor só arquivo agora evita YAGNI sem pintar a UI
  num canto.
- **Posse na lista:** vem da query do [0007](0007-schema-nucleo-minimo.md) (EXISTS),
  não de coluna; cuidar do N+1 (join/agregação única).
- **Servir binário com segurança:** validar que o `asset.path_or_uri` está sob um
  datasource conhecido antes de abrir (nunca servir path arbitrário).
- **OpenAPI-first x código-first:** decidir na implementação se a spec é escrita à mão
  e gera tipos, ou derivada do código Go — o resultado obrigatório é **tipos TS
  gerados** ([ADR-0014](../docs/adr/0014-stack-de-implementacao.md)), não mantidos à mão.

## 7. Fora deste briefing / follow-ups

- Camadas `externo`/`override` no resolver + edição de override (Fase 3).
- Upload/serviço de **arte custom** e proxy de imagem externa (Fase 3).
- `/api/v1/search` (busca local-first via FTS5) e `/enrich` (Fase 3).
- Ordenações/filtros ricos (por ano, gênero) guiados pela UI real.
