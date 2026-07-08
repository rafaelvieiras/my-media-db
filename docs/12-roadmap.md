# 12 — Roadmap

O desenvolvimento é **UI-first** (validar o rumo cedo) e dirigido por
**briefings** ([`briefings/`](../briefings/README.md)): cada item abaixo vira um
ou mais briefings autocontidos quando entra em execução.

O modelo e a arquitetura já nascem preparados para a **visão universal**
([README](../README.md)); as fases entregam **filmes/séries** primeiro.

---

## Fase 0 — Documentação de fundação ✅ concluída

Objetivo: fechar modelo, arquitetura e decisões de fundação **antes** de codar.

- **R1–R7** (resolver), **MD1–MD6** (modelo de dados) e **ME1–ME4** (base da doc),
  registrados na [agenda](00-agenda-de-decisoes.md) e nos [ADRs](adr/).
- Stack de implementação e passthrough do `/api/tmdb` ratificados
  ([ADR-0014](adr/0014-stack-de-implementacao.md)/[ADR-0015](adr/0015-api-tmdb-passthrough.md)).

## Fase 1 — Esqueleto do container único

Objetivo: um binário que sobe, serve um SPA vazio e responde `/api/v1/health`.

- Scaffold **Vite + React** (+ shadcn/ui) e binário **Go** com `//go:embed` do `dist/`.
- Router: estáticos + `/api/v1/health`; fallback de rota para o SPA.
- Flag **`VITE_API_BASE`** funcionando (dev: Vite 5173 + API 8080; prod: mesmo origin).
- **Dockerfile multi-stage** (build Vite → build Go → imagem final enxuta).

## Fase 2 — UI de biblioteca (validar o rumo)

Objetivo: apontar pastas, escanear, ver a mídia detectada com seus fatos técnicos.

- Cadastro de **datasource** + **scan** em background (Go: fs walk + ffprobe).
- Modelo mínimo: `entity`, `media_file`, `library_source`.
- **Grid de posters**, lista de itens detectados, **tela de detalhe** com mediainfo.
- Ainda sem enriquecimento externo pesado.

## Fase 3 — Enriquecimento + override

Objetivo: complementar itens de fontes externas e **dar autoridade ao dono**.

- Busca **local-first → TMDB** on-demand (resultados "não salvos").
- **Import explícito** + proxy/cache de imagem.
- **Editor de override** (título, campos) + **upload de arte custom**.
- O **resolver** compondo `override ⟵ externo ⟵ arquivo` em toda leitura.

## Fase 4 — API TMDB-compat + multiusuário

Objetivo: outros apps consomem o catálogo; acesso controlado.

- **`/api/tmdb/*`** servindo do banco; teste apontando um cliente TMDB real.
- **Usuários, tokens, permissões** ([10](10-usuarios-e-permissoes.md)).
- **Export NFO** opcional ([09](09-nfo-e-export.md)).

## Fase 5+ — Verticais e plugins (visão)

Objetivo: cumprir o escopo universal.

- Novas **verticais internas** (music videos, música, games…) via `extension_data`.
- **Maquinário de plugins** de terceiros (manifest, registry, ciclo de vida,
  isolamento) — só depois da base estável ([11](11-decisoes-abertas.md)).

---

## Como cada fase vira briefings

1. Ao iniciar uma fase, quebrar seus itens em briefings (`briefings/NNNN-*.md`).
2. Cada briefing referencia esta fase e as `docs/` que o fundamentam.
3. O índice de briefings vive em [`briefings/README.md`](../briefings/README.md).
