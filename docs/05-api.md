# 05 — API

> **Status desta doc:** stub escopado. Decisões travadas listadas; detalhamento
> (rotas, payloads, auth) a fechar em briefings da Fase 3.

## Princípios

- **Dois namespaces, separados de propósito:**
  - **`/api/tmdb/*`** — camada **compatível com o TMDB**. Objetivo: apontar um
    cliente que já fala TMDB (Jellyfin/Kodi) para o servidor trocando só a URL
    base. Não deve ser poluída por features fora do escopo do TMDB.
  - **`/api/v1/*`** — API **nativa**, dona de tudo que o TMDB não modela
    (override, biblioteca, mediainfo, assets custom, usuários).
- **Serve sempre do resolver** (ver [03](03-arquitetura.md)/[04](04-modelo-de-dados.md)):
  as respostas refletem o catálogo do dono **com overrides aplicados**, nunca o
  dado externo cru.
- **Versionada** para estabilidade de contrato (`v1`, `v2`…).

## Superfície nativa prevista (a detalhar)

- `/api/v1/library` — datasources, scan, status, itens faltando/duplicados.
- `/api/v1/entities/{id}` — entidade resolvida (com override).
- `/api/v1/entities/{id}/override` — editar campos (o override do dono).
- `/api/v1/entities/{id}/files` — arquivos + mediainfo.
- `/api/v1/entities/{id}/assets` — artes (upload/servir custom).
- `/api/v1/search` — busca local-first.
- `/api/v1/enrich` — disparar enriquecimento on-demand.
- `/api/v1/users`, `/api/v1/tokens` — multiusuário (Fase 3).

## Decisões abertas (ver [11](11-decisoes-abertas.md))

- Escopo exato das rotas TMDB a espelhar (movie, tv, search, images…).
- Cabeçalho de auth / esquema de token.
- Contrato de tipos: **OpenAPI gerado** (fonte de verdade p/ tipos TS do front).
- Política de `/api/tmdb` quando o item não existe local: `404` vs
  passthrough+cache do TMDB real.
