# 0045 — Metadados de série: paridade com filme (slogan, gênero, classificação, tags, situação, relacionados)

> A série passa a trazer os mesmos metadados ricos que o filme já tem: slogan
> (tagline), gêneros, classificação indicativa, tags/keywords, **situação**
> (em andamento / encerrada / renovada / cancelada) e **relacionados** (similares).
> Hoje o filme tem enriquecimento grande e a série está pobre.

| | |
|---|---|
| **ID** | 0045 |
| **Tipo** | feature |
| **Status** | done |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override (vertical séries) |
| **Docs relacionadas** | [0014](0014-provider-tmdb-busca-local-first.md), [0022](0022-detalhe-externo-rico-persistente.md), [0024](0024-paridade-detalhe-entity-rico.md), [0031](0031-series-fundacao-modelo-enriquecimento.md) |
| **Depende de** | 0044, 0031 |

---

## 1. Objetivo

O detalhe do **filme** já é rico (slogan, gêneros, classificação, tags, elenco via
caminho rico do TMDB — `enriquecerCanonicoRico`/`tmdb_rico.go`). O detalhe da
**série** está pobre: faltam campos que o TMDB fornece e que já temos para filme:

- **Slogan / tagline** da série.
- **Gêneros** da série.
- **Classificação indicativa** (content rating por país/idioma).
- **Tags / keywords** da série.
- **Situação (status)** da série: em andamento (*Returning Series*), encerrada
  (*Ended*), cancelada (*Canceled*), em produção, planejada. É informação-chave de
  série (o filme não tem equivalente tão saliente) e o TMDB a fornece.
- **Relacionados / similares** da série (o filme já tem; a série não).

Isso deixa a série em **paridade** com o filme no detalhe rico.

## 2. Jornada do usuário

1. O dono abre o detalhe de uma série enriquecida. Vê **slogan**, **gêneros**,
   **classificação indicativa**, **tags** e um selo de **situação** ("Em exibição",
   "Encerrada", "Cancelada", "Renovada").
2. Uma seção de **relacionados/similares** aparece (como no filme), navegável e
   priorizando o que já está na biblioteca (interno-first, 0026/0027).
3. Ao re-importar (0043), esses campos atualizam junto com o resto da árvore
   (situação muda de "Em exibição" para "Encerrada", p.ex.), preservando override.

## 2.1. Realidade descoberta (decisão de arquitetura)

**A série hoje é importada EXCLUSIVAMENTE do TVDB.** O TMDB **não** é `SerieProvider`
(não existe `tmdb_serie.go`/`BuscarSerieArvore`); só TVDB e o fake implementam a
árvore. Consequência: o caminho rico do filme (`enriquecerCanonicoRico` em
`tmdb_rico.go`, que só roda no `MapearDetalhe` do TMDB) **nunca** roda para série.
E o `MapearDetalhe` do TVDB (`tvdb_detalhe.go:158`/`enriquecerNeutrosSerie`)
**descarta** o `status {name}` e os `genres[]` que o `/series/{id}/extended` traz, e
o TVDB **não fornece** tagline, keywords nem similar. Logo, hoje toda série tem
Tagline nil, Keywords `[]`, Certification nil, Similar `[]`, sem gêneros e sem
situação. Os campos e blocos de UI **já existem** (o `RichDetailView` e o
`ResolvedEntity` são agnósticos de tipo — `resolver.go:103-117`,
`rich-detail.ts:26-46`); falta **alimentá-los** para série. Só a **situação** é
campo novo em toda a stack.

**Rota escolhida — (C) enriquecimento híbrido do nó série pelo TMDB:** manter o
**TVDB como provider da árvore** (episódios/temporadas/still/numbering — não mexer),
e **enriquecer o NÓ SÉRIE** com um lookup no TMDB para os campos ricos (tagline,
gêneros, classificação, keywords, situação, similares). O casamento TVDB→TMDB usa o
`/find/{tvdb_id}?external_source=tvdb_id` do TMDB (mapear o id do TVDB para o id de
série do TMDB) e então `/tv/{id}` rico (reusar a maquinaria de `tmdb_rico.go`,
adaptando para série: `content_ratings`, `keywords`, `translations`, `similar`,
`status`). Fallback gracioso: sem TMDB (ou sem match), usar o que o TVDB der
(mapear ao menos `status` e `genres` do `/extended`, que hoje são descartados) e
deixar o resto vazio — nunca erro.

- **Descartadas:** (A) tornar o TMDB o `SerieProvider` principal e resolver a série
  pela fonte TMDB — reescreveria a fundação de séries (ADR-0021, toda a árvore/still/
  numbering ancorada no TVDB); risco alto, fora de proporção. (B) só reimplementar
  campo a campo no TVDB — o TVDB não tem tagline/keywords/similar; entregaria menos
  do que o usuário pediu ("temos no TMDB"). (C) híbrido entrega o pedido reusando o
  caminho rico do TMDB sem tocar a árvore.

## 3. Escopo

**Dentro:**

- **Lookup TMDB do nó série (rota C):** dado o id TVDB da série (em `external_ids`),
  resolver o id TMDB via `/find` e buscar o `/tv/{id}` rico. Novo caminho no provider
  TMDB para série-detalhe (não árvore) — p.ex. `tmdb_serie_detalhe.go` — reusando
  `enriquecerCanonicoRico`/helpers de `tmdb_rico.go` adaptados ao payload de TV.
- **Enriquecimento (campos ricos):** trazer para o canônico da série **tagline,
  genres (via `tags_by_lang`), content rating (`content_ratings`), keywords** pelo
  caminho rico do TMDB. Persistir no nó série no import/re-import (0043 refresh via
  `AtualizarExtensionData`/campos canônicos — garantir cobertura).
- **Fanart/backdrop de série (resolve o buraco do 0044):** o `/tv/{id}` do TMDB traz
  `backdrop_path` → `backdrop_url` no canônico. Mesclar isso no canônico do nó série
  **antes** de `persistir`/`cachearArtes` (`enrich.go`) para que a fanart da série
  seja **baixada** como asset `fanart` (hoje o TVDB não dá backdrop, então série não
  tem fanart). Isso também faz a **herança de fanart de temporada** (0044) passar a
  ter o que herdar. É o elo que fecha "séries/temporadas cabem fanart".
- **Situação (status):** mapear o `status` do TMDB de série
  (`Returning Series`→"Em exibição", `Ended`→"Encerrada", `Canceled`→"Cancelada",
  `In Production`→"Em produção", `Planned`→"Planejada"). Campo NOVO em toda a stack:
  canônico (`core.tv@1.status` ou `out["airing_status"]`), `ResolvedEntity`
  (`resolver.go`), `ExternalDetail`, `RichDetail` + ambos adapters, `<Badge>` na
  linha do herói (`rich-detail-view.tsx:520`). Enum neutro no dado; rótulo pt-BR na
  UI. Fallback: `status`/`genres` do TVDB `/extended` (hoje descartados) quando não
  houver TMDB.
- **Relacionados/similares:** `similar` do TMDB no nó série + interno-first (reusar
  `resolver.go:421`/`external_detail.go:160`). **Ampliar o interno-first** para casar
  também série (hoje `interno.go:26` só resolve `source=="tmdb"`; o similar de série
  vem com id TMDB, então casa — confirmar).
- **Exposição + UI:** reusar os blocos do `RichDetailView` (já renderizam para série
  assim que os dados chegam); só a situação é bloco novo (selo).
- **Tradução:** tagline traduzível per-idioma (TMDB `translations`, como o filme);
  gêneros/tags via `tags_by_lang` (0020). Situação é enum neutro, rótulo traduzido só
  na UI.

**Fora (explicitamente):**

- **Tornar o TMDB o provider da árvore de série** (rota A) — descartado; a árvore
  segue no TVDB.
- **Elenco/equipe/produtoras de série** (person/credit/company) — é o MD6 (0029).
- **Série sem match no TMDB** — degrada para os campos do TVDB `/extended`
  (status/genres) + vazio no resto; nunca erro.
- **Filtros/álbum por gênero/situação na home** — só exibição no detalhe aqui.
- **`next_episode_to_air` / data de retorno** — follow-up (§7).

## 4. Contrato técnico

### Dados / modelo
- Sem migration esperada. Campos ricos vão para o canônico/`extension_data` como o
  filme já faz; situação em `core.tv@1.status` (ou campo rico), string enum.
- Tagline traduzível em `media_external.payload_json[lang]` (como título/overview).

### Backend (Go)
- **Rota C (híbrido):** novo caminho de série-detalhe no provider TMDB (ex.
  `internal/provider/tmdb/tmdb_serie_detalhe.go`): `/find/{tvdb_id}` → id TMDB →
  `/tv/{id}` (append `content_ratings,keywords,translations,similar`), reusando os
  helpers de `tmdb_rico.go` (`taglinesPorIdioma`, `keywordsPorIdioma`,
  `certificacoesPorRegiao` já lê `content_ratings`, `similaresCanonicos`,
  `tagsPorIdioma`) e mapeando o `status` de TV. Ligar esse enriquecimento ao nó
  série em `internal/enrich/serie.go` (após montar a árvore do TVDB), sem tocar a
  árvore.
- `internal/provider/tvdb/tvdb_detalhe.go`: **fallback** — desserializar e emitir o
  `status {name}` e `genres[]` do `/series/{id}/extended` (hoje descartados em
  `serieData`/`enriquecerNeutrosSerie`), usados quando não há match no TMDB.
- **Situação**: campo novo — canônico (`out["airing_status"]` ou
  `core.tv@1.status`), `ResolvedEntity`/`Entidade` (`resolver.go`), `ExternalDetail`.
- `internal/resolver/resolver.go` + `internal/server`: expor os campos no
  `ResolvedEntity`/detalhe (muitos já existem para filme — reusar). Situação e
  relacionados de série expostos como o filme.
- `internal/enrich/serie.go`: persistir os novos campos no import/re-import
  (0043 refresh cobre atualização; garantir que os campos entram no mesmo caminho).

### Frontend (Vite/React)
- `RichDetailView` (0024) já renderiza gêneros/tags/tagline/classificação/
  relacionados para filme — garantir que aparecem para série (o modelo normalizado
  `rich-detail.ts` deve mapear os campos da série). Selo de **situação** novo
  (rótulos pt-BR: "Em exibição", "Encerrada", "Cancelada", "Em produção",
  "Planejada").
- Relacionados de série reusa o carrossel/lista de similares do filme, interno-first.

### OpenAPI
- Adicionar os campos que faltarem ao `ResolvedEntity` (status/situação de série;
  garantir tagline/genres/content_rating/keywords/similar presentes para série).
  `gen:types`; gerado formatado; `tsc -b` zero.

### Arquivos afetados (previsão)
- Go: `internal/provider/tmdb/tmdb_rico.go`, `internal/provider/tmdb/tmdb.go`,
  `internal/provider/tvdb/*`, `internal/enrich/serie.go`,
  `internal/resolver/resolver.go`, `internal/server/*` (detalhe/similares),
  `openapi.yaml` + tipos, testes Go.
- Web: `web/src/lib/rich-detail.ts`, `RichDetailView` e blocos (situação),
  relacionados, testes web.

## 5. Critérios de aceite

- [x] Detalhe de série enriquecida (TMDB) mostra: slogan, gêneros, classificação
      indicativa, tags e selo de situação.
- [x] Seção de relacionados/similares aparece na série, interno-first (`anotarInternos`
      roda p/ tv; `ObterEntityPorTmdb` casa por id independente de tipo).
- [x] Re-import atualiza situação e demais campos: `airing_status` é campo de base
      read-only (não editável pelo dono), o refresh reescreve a base e o resolver
      preserva override dos campos traduzíveis (mecanismo 0043).
- [x] Série só-TVDB não quebra: fallback `enriquecerFallbackRico` emite
      status/genres do `/extended`; tagline/keywords/similar ausentes sem erro.
- [x] Paridade visual com o filme (mesmos blocos do `RichDetailView`, agnósticos de
      tipo; só o selo de situação é novo).
- [x] Catraca completa verde (Go 75.2%, E2E 46.3%, web 90.71%, tsc 0, jscpd 0.12%;
      gocyclo drift 6 vs 3 não-bloqueante).
- [x] Smoke real: re-import da série Simpsons → slogan "A comédia que nunca
      envelhece.", gêneros Família/Animação/Comédia, classificação 12, keywords,
      `airing_status` "Returning Series" (TMDB sobrepõe "Continuing" do TVDB),
      20 similares e **fanart baixada** (asset `fanart external`).

## 6. Notas de implementação / decisões

- **Reuso máximo do caminho rico do filme** (`enriquecerCanonicoRico`,
  `RichDetailView`, similares do 0027): a série deve pendurar nos mesmos blocos, não
  criar UI/campo paralelos — paridade barata e consistente.
- **Situação é o metadado mais característico de série** e o mais pedido: mapear o
  enum do TMDB com fidelidade; rótulos traduzidos só na UI (enum neutro no dado).
- **Interno-first nos relacionados** (0026/0027): o resolver segue puro; o server
  pós-processa marcando `internal_id`.
- **Ausência graciosa** para série só-TVDB: campo vazio, nunca erro.

## 7. Fora deste briefing / follow-ups

- **MD6 de série** (elenco/equipe/produtoras) — briefing 0029.
- **Filtros/coleções por gênero/situação** na home.
- **Próximo episódio a exibir / data de retorno** (next_episode_to_air do TMDB) —
  extensão natural da situação, follow-up.
