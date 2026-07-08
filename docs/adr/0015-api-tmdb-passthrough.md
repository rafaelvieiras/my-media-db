# ADR-0015 — `/api/tmdb`: passthrough com cache, nunca 404

- **Status:** Aceita
- **Data:** 2026-07-08
- **Agenda:** [doc 11, item 6](../11-decisoes-abertas.md) · **Spec:** [05](../05-api.md)

## Contexto

A camada compatível `/api/tmdb` serve clientes como Jellyfin/Kodi. Faltava definir
o que responder quando o item consultado **não existe no catálogo local**: `404`,
ou buscar no TMDB real. Um `404` por indisponibilidade quebraria o cliente.

## Decisão

**Passthrough + cache, nunca 404 por indisponibilidade.**

1. **Local-first:** havendo item local, serve do resolver (com override).
2. **Miss → passthrough + cache**, com o destino dependendo do tipo de chamada e da
   config:
   - **Busca (`search`)** → só cache; **não** cria entrada.
   - **Detalhe (id específico)** → modo configurável: **`promote_draft`** cria uma
     entrada `draft` na **fila de revisão** (item auto-criado, candidato a órfã por
     [ADR-0012](0012-ciclo-de-vida-nao-destrutivo.md)/[ADR-0008](0008-identidade-e-matching-kodi-jellyfin.md)),
     ou **`hidden_cache`** mantém só em cache oculto, revalidado por TTL.
3. **Falha da API externa → serve cache (stale)**, nunca erro. O `404` fica só para
   o item que o próprio TMDB diz **não existir**, jamais para indisponibilidade.

Config `tmdb_passthrough`: `enabled` (default **on**), `on_detail`
(`promote_draft` | `hidden_cache`), TTL de revalidação.

## Consequências

- Clientes existentes ganham catálogo "infinito" sem quebrar quando a fonte cai.
- É a **exceção explícita** ao princípio "servir sempre do resolver" (doc 05): o
  passthrough serve dado externo cru na forma TMDB.
- `promote_draft` conecta a compat à curadoria (draft → resolver → override); o dono
  controla se quer o banco povoado ou só cache.
