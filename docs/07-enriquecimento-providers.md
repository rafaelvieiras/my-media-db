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
para a `Entity`/`extension_data`. Providers são plugáveis e declarados por
**capacidade**, não por nome fixo.

- **Capacidades** são interfaces **opcionais** descobertas por type-assert, sobre
  um core mínimo (`Nome/Buscar/BuscarDetalhe/MapearDetalhe`): `SerieProvider`
  (árvore de temporadas/episódios), `SerieRicoProvider` (enriquecimento cruzado
  do nó série), `ColecaoProvider`, e futuras (`ArteProvider` para fanart.tv,
  trailer, tema). Um provider que não implementa uma capacidade é degradado em
  silêncio.
- **Independência de provider** (princípio, ver [AGENTS.md](../AGENTS.md) §6): a
  mídia sobrevive a **qualquer** provider registrado. Só TMDB → pesquisa e
  importa filme **e** série pelo TMDB; só TVDB → idem pelo TVDB; ambos → cruzam e
  enriquecem. Campo que uma fonte não modela fica vazio, **nunca** bloqueia a
  vertical. Estado de implementação e lacunas: briefings **0046/0047/0048**.
- **Registry N-providers** com ordem = prioridade; múltiplos providers coexistem
  e se compõem no resolver (`override ⟵ externo ⟵ arquivo`, camadas externas
  ordenadas por prioridade).
- **Visão (multi-vertical):** TVDB, IMVDB (music videos), MusicBrainz (música),
  ScreenScraper (games), fanart.tv (arte) e outros — cada um como provider.

## Decisões abertas (ver [11](11-decisoes-abertas.md))

- Gestão de **chaves de API** (nunca hardcoded — ver alerta em [11](11-decisoes-abertas.md)).
- Estratégia de rate-limit/cache por provider.
- ~~Universal scraper (combinar fontes) vs escolha explícita da fonte~~ →
  **fechada**: fonte-base única por item + demais providers enriquecem por
  capacidade (independência de provider, briefings 0046-0048).
