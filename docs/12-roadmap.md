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
- **Multi-idioma** (L1–L3): config principal/fallback/permitidos, resolução por
  idioma com fallback + proveniência, tradução de campos e tags.
- **Editor de override** (título, campos, **por idioma**) + **upload de arte custom**.
- O **resolver** compondo `override ⟵ externo ⟵ arquivo` em toda leitura.

**Breakdown em briefings** (ver [`briefings/`](../briefings/README.md#fase-3--enriquecimento--override)):

| ID | Entrega | Fecha |
|---|---|---|
| [0012](../briefings/0012-schema-fase3-camadas-externo-override.md) | Materializar `media_external` + `media_override` | fundação (R4/MD4) |
| [0013](../briefings/0013-resolver-tres-camadas.md) | Resolver `override ⟵ externo ⟵ arquivo` | R1–R7 |
| [0014](../briefings/0014-provider-tmdb-busca-local-first.md) | Provider TMDB + busca `/api/v1/search` local-first | doc 07 |
| [0015](../briefings/0015-enriquecimento-import-proxy-imagem.md) | Import explícito + proxy/cache de imagem | doc 07/08 |
| [0019](../briefings/0019-fundacao-multi-idioma-settings-resolver.md) | Fundação multi-idioma: settings + resolução por idioma | L1/L2 |
| [0020](../briefings/0020-provider-multi-idioma-traducao-tags.md) | Provider multi-idioma + tradução de tags | L3 |
| [0021](../briefings/0021-ui-idiomas-admin-seletor.md) | UI de idiomas: admin + seletor de leitura | L1/L2 |
| [0016](../briefings/0016-editor-override-ui.md) | Editor de override + UI (por idioma) | R3/R6 |
| [0017](../briefings/0017-arte-custom-upload-ui.md) | Arte custom (upload) + UI | doc 08 |
| [0018](../briefings/0018-fila-revisao-orfas-correcao-match.md) | Fila de revisão + órfãs + correção de match | MD1/MD5 |

| [0022](../briefings/0022-detalhe-externo-rico-persistente.md) | Detalhe externo rico + cache persistente (provider) | doc 07 |
| [0023](../briefings/0023-tela-detalhe-externo-ui.md) | Tela rica de detalhe do item externo (UI) | doc 07/08 |

Os briefings **0022/0023** nasceram na **revisão da Fase 3**: uma tela rica de
detalhe do item externo (como o TMDB) **antes** de importar, persistindo o detalhe na
1ª consulta. A 1ª versão é **sem créditos** e **sem nota local** — ver abaixo.

**Adiado para o fim da fase / Fase 4:** elenco/equipe (MD6 — `person`/`credit`/
`company` e override de créditos), separado por ser bem mais complexo; e a
**avaliação/nota local** do dono (modelo novo), levantada na revisão da Fase 3.

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
- **Histórico de consumo (tracking/scrobbling)** agnóstico a vertical —
  tabela de eventos genérica + webhook de media server + capacidade
  `TrackerProvider` para sync externo (Trakt e afins). Fundado em pesquisa de
  mercado, decisão em [ADR-0022](adr/0022-historico-de-consumo-eventos-genericos-e-trackerprovider.md)
  (agenda [H1–H4](00-agenda-de-decisoes.md)), breakdown em
  [briefings 0092–0094](../briefings/README.md#fase-5--histórico-de-consumo-trackingscrobbling).

---

## Como cada fase vira briefings

1. Ao iniciar uma fase, quebrar seus itens em briefings (`briefings/NNNN-*.md`).
2. Cada briefing referencia esta fase e as `docs/` que o fundamentam.
3. O índice de briefings vive em [`briefings/README.md`](../briefings/README.md).
