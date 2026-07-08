# 07 — Enriquecimento e providers

> **Status desta doc:** stub escopado. Detalhamento em briefings da Fase 3.

## Princípios

- **Pull > Sync.** Nada sincroniza sozinho. O dono **puxa** da fonte externa
  quando quer ([cenário F](01-visao-e-casos-de-uso.md)).
- **Local-first.** Buscar consulta primeiro o banco; só então a fonte externa,
  cujos resultados aparecem marcados como **"não salvos"** até import explícito.
- **Import explícito.** O dono decide o que persistir. O que é puxado vira
  camada `media_external` (base), **sem tocar** no override.
- **Cache local.** Payloads e imagens puxados são cacheados localmente (ver
  [08](08-artwork-e-assets.md)) para não depender da fonte depois.

## Abstração de provider

Um **provider** encapsula uma fonte externa: busca, fetch de detalhe, mapeamento
para a `Entity`/`extension_data`. Providers são plugáveis por vertical.

- **MVP:** TMDB (filmes e séries).
- **Visão (multi-vertical):** TVDB, IMVDB (music videos), MusicBrainz (música),
  ScreenScraper (games), e outros — cada um como provider de uma vertical. Ordem
  e prioridade a definir ([11](11-decisoes-abertas.md)).

## Decisões abertas (ver [11](11-decisoes-abertas.md))

- Gestão de **chaves de API** (nunca hardcoded — ver alerta em [11](11-decisoes-abertas.md)).
- Estratégia de rate-limit/cache por provider.
- Universal scraper (combinar fontes) vs escolha explícita da fonte.
