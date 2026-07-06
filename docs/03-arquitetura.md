# 03 — Arquitetura

## Princípio de forma

O MyMediaDB é distribuído como **um binário / uma imagem de container**. O mesmo
processo Go serve os **assets estáticos do frontend** e responde a **API** no
mesmo *origin*. Sem Node em produção, sem processo separado obrigatório, memória
mínima.

Há uma tensão de fundo que a arquitetura precisa respeitar: o app de gestão só
existe quando aberto, mas a **API precisa estar sempre no ar** (o Jellyfin
consulta às 3h da manhã). Por isso o **serviço é o produto** e a UI é apenas um
cliente dele — inclusive podendo apontar para uma instância remota.

## Componentes

```
┌──────────────────────────────────────────────────────────┐
│  binário Go  (uma imagem, um processo)                    │
│                                                            │
│   router HTTP                                              │
│    ├── /              → SPA (dist do Vite, //go:embed)     │
│    ├── /api/tmdb/*    → camada compatível com TMDB         │
│    └── /api/v1/*      → API nativa                         │
│                                                            │
│   serviços                                                 │
│    ├── scanner      (varredura de datasources)            │
│    ├── mediainfo    (extração via ffprobe, subprocess)    │
│    ├── enrich       (providers externos, on-demand)       │
│    ├── artwork      (proxy/cache + upload custom)         │
│    └── resolver     (compõe override ⟵ externo ⟵ arquivo) │
│                                                            │
│   persistência                                            │
│    └── SQLite (modernc.org/sqlite, CGO-free)              │
│        + arquivos de mídia/arte em disco (path no banco)  │
└──────────────────────────────────────────────────────────┘
```

## Stack

| Camada | Escolha | Porquê |
|---|---|---|
| Backend | **Go** | Binário único, RAM baixa, sem runtime pesado parado; cross-compile fácil para o servidor (incl. ARM). |
| Frontend | **Vite + React** | React moderno, build estático leve. **Sem Next.js/SSR** — contradiz "memória mínima". |
| Banco | **SQLite** | Local-first, zero-config, um arquivo. Volume de requests **não é gargalo** para o uso self-hosted (decisão revista — ver [11](11-decisoes-abertas.md)). |
| Empacotamento | **Um container** | `//go:embed` do `dist/` do Vite; o Go serve tudo. |

Escolhas de biblioteca (router Go, driver SQLite, camada de query) ficam em
[11 — Decisões abertas](11-decisoes-abertas.md).

## Modos de execução e a flag de API

O frontend fala com a API por uma **base configurável**, `VITE_API_BASE`:

| `VITE_API_BASE` | Comportamento |
|---|---|
| **vazia** (produção padrão) | O front usa **caminhos relativos**; o binário Go serve os estáticos *e* responde `/api/*` no mesmo origin. Um container, tudo junto. |
| **preenchida** | O front aponta para **aquela URL** como base da API. Serve para dev (Vite na 5173 + API Go na 8080) e para o app apontar para uma **instância remota**. |

### Desenvolvimento

- `vite dev` (HMR) na 5173, com `VITE_API_BASE=http://localhost:8080`.
- Binário Go servindo só a API na 8080.

### Produção

- `vite build` → `dist/` embutido no binário via `//go:embed`.
- `VITE_API_BASE` vazio → mesmo origin, o Go faz o *fallback* de rota (qualquer
  caminho não-`/api` devolve o `index.html` para o roteamento client-side).

## O resolver (composição em camadas)

O coração da leitura é o **resolver**, que monta a visão final de cada entidade
compondo três camadas em ordem de precedência (detalhado em
[04 — Modelo de dados](04-modelo-de-dados.md)):

```
resultado  =  OVERRIDE do usuário   (vence sempre)
           ⟵  dados EXTERNOS         (TMDB/etc., cache local)
           ⟵  dados do ARQUIVO       (mediainfo, detectado)
```

Tanto `/api/v1` quanto `/api/tmdb` servem **a partir do resultado do resolver** —
nunca do dado externo cru. É isso que garante que o override do dono apareça em
todo lugar, inclusive para o Jellyfin.

## Preparação para verticais e plugins

O núcleo não conhece "filme" ou "série" de forma privilegiada: conhece uma
**entidade genérica** com **dados de extensão** *namespaced* (ver
[04](04-modelo-de-dados.md)). Isso é o que permite, no futuro, adicionar
verticais e **plugins de terceiros** sem alterar o schema do núcleo — mas o
*maquinário* de plugins (registry, ciclo de vida, sandbox) está **fora do MVP**
(ver [11](11-decisoes-abertas.md)).
