# 05 — API

> **Status desta doc:** stub escopado. Decisões travadas listadas; detalhamento
> (rotas, payloads, auth) a fechar em briefings da Fase 4.

## Princípios

- **Dois namespaces, separados de propósito:**
  - **`/api/tmdb/*`** — camada **compatível com o TMDB**. Objetivo: apontar um
    cliente que já fala TMDB (Jellyfin/Kodi) para o servidor trocando só a URL
    base. Não deve ser poluída por features fora do escopo do TMDB.
  - **`/api/v1/*`** — API **nativa**, dona de tudo que o TMDB não modela
    (override, biblioteca, mediainfo, assets custom, usuários).
- **Serve sempre do resolver** (ver [03](03-arquitetura.md)/[04](04-modelo-de-dados.md)):
  as respostas refletem o catálogo do dono **com overrides aplicados**, nunca o
  dado externo cru. **Exceção explícita:** o passthrough do `/api/tmdb` (abaixo),
  que serve dado externo quando o item não é local.
- **Versionada** para estabilidade de contrato (`v1`, `v2`…).
- **Contrato de tipos via OpenAPI gerado.** A API se descreve em **OpenAPI**, que é
  a **fonte de verdade** dos tipos TS do front (gerados, não mantidos à mão) — front
  e back não saem de sincronia. Ver [ADR-0014](adr/0014-stack-de-implementacao.md).

## `/api/tmdb`: passthrough com cache (nunca 404)

Quando um cliente compatível (Jellyfin/Kodi) consulta o `/api/tmdb` e o item **não
existe local**, a camada **nunca responde 404 por indisponibilidade** — ela busca no
TMDB real, cacheia e serve. Comportamento em três regras:

1. **Local-first.** Se o item existe no catálogo, serve do **resolver** (com
   override), como qualquer rota. Passthrough só entra quando não há item local.
2. **Miss → passthrough + cache.** Sem item local, busca no TMDB real e **cacheia o
   payload**. O que acontece com esse resultado depende do **tipo de chamada** e da
   configuração:
   - **Busca (`search`)** → só passthrough + cache; **não** cria entrada (evita
     poluir o banco com todo resultado de pesquisa).
   - **Detalhe (fetch de um id específico)** → conforme o modo configurado:
     - **`promote_draft`** — cria automaticamente uma entrada `draft` no banco e a
       põe na **fila de revisão** (item auto-criado, sem investimento do dono → é
       candidato a **órfã** se nunca for curado; ver [MD1](00-agenda-de-decisoes.md)
       e [04 — Ciclo de vida](04-modelo-de-dados.md)). A partir daí o item passa a
       fluir pelo resolver como qualquer entidade.
     - **`hidden_cache`** — mantém **só em cache oculto** (não vira entidade),
       **revalidado a cada X** (TTL). Serve à compatibilidade sem sujar o catálogo.
3. **Falha da API externa → serve cache (stale), nunca 404.** Se o TMDB estiver
   inacessível, responde com o **cache existente**, mesmo vencido, em vez de erro. O
   404 fica reservado para o item que o próprio TMDB diz **não existir** — nunca para
   indisponibilidade.

> Config: `tmdb_passthrough` com `enabled` (default **on**), `on_detail`
> (`promote_draft` | `hidden_cache`) e o TTL de revalidação. Decisão em
> [ADR-0015](adr/0015-api-tmdb-passthrough.md).

## Superfície nativa prevista (a detalhar)

- `/api/v1/library` — datasources, scan, status, itens faltando/duplicados.
- `/api/v1/entities/{id}` — entidade resolvida (com override).
- `/api/v1/entities/{id}/override` — editar campos (o override do dono).
- `/api/v1/entities/{id}/files` — arquivos + mediainfo.
- `/api/v1/entities/{id}/assets` — artes (upload/servir custom).
- `/api/v1/search` — busca local-first.
- `/api/v1/enrich` — disparar enriquecimento on-demand.
- `/api/v1/users`, `/api/v1/tokens` — multiusuário (Fase 4).

## Shape de erro padrão do `/api/v1`

Definido no [briefing 0008](../briefings/0008-datasources-cadastro-api-library.md) e
**referência para toda a superfície `/api/v1`** (base para o gerador OpenAPI do
[0010](../briefings/0010-api-leitura-resolver-minimo.md)). Toda resposta de erro
usa o mesmo envelope:

```json
{ "error": { "code": "<slug estável>", "message": "<texto humano em pt-BR>" } }
```

- `code` é um **slug estável** (não localizado) para o cliente ramificar; `message`
  é o texto legível. Content-Type `application/json`.
- Sucesso serializa o recurso direto (sem envelope); listas usam `{ "items": [...] }`.
- Codes já em uso: `invalid_body` (400), `invalid_type`, `invalid_path`,
  `not_a_directory`, `path_unreadable` (422), `duplicate_path` (409), `not_found`
  (404), `internal_error` (500). O 404 genérico de `/api` (rota inexistente) também
  responde neste shape (`not_found`).

## `/api/v1/library`: datasources (implementado no 0008)

- **POST `/api/v1/library`** — body `{ "path": "...", "type": "movie"|"tv" }`.
  Normaliza o path (absoluto + limpo), valida existência/diretório/legibilidade e
  cria a fonte (`state=online`, `last_scan` nulo). `201` com o registro;
  `422` (path inválido/ilegível/não-diretório ou tipo inválido); `409` (path
  duplicado após normalização); `400` (corpo inválido).
- **GET `/api/v1/library`** — `200 { "items": [ { id, path, type, state, last_scan } ] }`.
  O `state` é **reavaliado na leitura** por um stat barato (não persistido aqui).
- **GET `/api/v1/library/{id}`** — `200` (com `state` reavaliado) | `404`.
- **DELETE `/api/v1/library/{id}`** — remove só o registro da fonte → `204` | `404`
  (não purga entities — ver §6 do briefing 0008).

## Decisões abertas (ver [11](11-decisoes-abertas.md))

- Escopo exato das rotas TMDB a espelhar (movie, tv, search, images…).
- Cabeçalho de auth / esquema de token (Fase 4; ver [10](10-usuarios-e-permissoes.md)).

> **Fechadas:** contrato **OpenAPI gerado** e política de `/api/tmdb`
> (**passthrough + cache, nunca 404**) — ver acima e ADR-0014/0015.
