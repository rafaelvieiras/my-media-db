# 0033 — Busca: filtro de sub-nós (temporadas/episódios) default-off em seções separadas

> Deixar a busca **poder** trazer temporadas/episódios, mas por padrão DESLIGADO e,
> quando ligado, em **seções separadas** — nunca misturado com filmes/séries.

| | |
|---|---|
| **ID** | 0033 |
| **Tipo** | fix/feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override (vertical séries) |
| **Status** | done |
| **Docs relacionadas** | [ADR-0021](../docs/adr/0021-modelo-de-series-hierarquia-ordenacoes-vinculo-arquivo.md), [0031](0031-series-fundacao-modelo-enriquecimento.md), [0032](0032-usabilidade-arvore-series-home-busca-breadcrumb.md) |
| **Depende de** | 0032 |

---

## 1. Objetivo

O briefing 0032 EXCLUIU por hardcode `season`/`episode` da busca local (o
`AND e.type NOT IN ('season','episode')` fixo em `search.go`). Isso está errado: a
busca **deve poder** trazer temporadas/episódios. No geral o usuário busca filme/série;
se "Simpsons" trouxer dezenas de episódios/temporadas de várias séries antes da série
em si, a busca fica pobre. A correção: **default = só topo** (movie/tv/collection); o
usuário LIGA "Temporadas" e/ou "Episódios" via filtro, e aí os sub-nós aparecem numa
**seção separada** (não misturados). Este briefing **SUPERSEDE** a decisão de busca do
0032 (a exclusão dura vira filtro default-off). O filtro da HOME (`ListarEntityCards`)
**não** muda — segue excluindo os sub-nós.

## 2. Jornada do usuário

1. O usuário abre **Buscar** e digita "Simpsons".
2. Por padrão vê a seção **"Filmes e séries"** com a série (e filmes homônimos), sem a
   enxurrada de episódios/temporadas.
3. Se quiser os sub-nós, liga o chip **"Temporadas"** e/ou **"Episódios"**.
4. Uma nova seção aparece — **"Temporadas"** e/ou **"Episódios"** — com os sub-nós
   casados, em grid próprio, sem misturar com a seção de topo.
5. Desligar o chip esconde a seção de novo. A busca externa segue intacta abaixo.

## 3. Escopo

**Dentro:**
- Parametrizar a exclusão de sub-nós na **busca local** (FTS em `search.go`): default
  só-topo, inclusão opt-in por parâmetro.
- Novo parâmetro de query `include` (CSV: `season`, `episode`) no `GET /api/v1/search`.
- UI da busca: dois toggles default-off e **seções separadas** por categoria.

**Fora (explicitamente):**
- A HOME/biblioteca (`ListarEntityCards`) — continua só-topo, sem toggle (0032 vale).
- Busca **externa** — os providers já retornam série/filme, não episódio; sem mudança.
- Ordenação/numeração de episódios na busca (Fase 3, seletor de ordenação).

## 4. Contrato técnico

### Dados / API
- `GET /api/v1/search` ganha o parâmetro **`include`** (query, opcional): CSV que
  religa sub-nós nos resultados **locais**. Aceita `season` e/ou `episode`
  (ex.: `include=season,episode`); valores fora desse conjunto são **ignorados**;
  repetição é idempotente. Ausente ⇒ só-topo (comportamento default preservado).
- Os sub-nós religados vêm no MESMO `local[]` da resposta (mesmo shape de card),
  distinguíveis pelo campo `type` — a **UI** os separa em seções.

### Backend (Go)
- `internal/server/search.go`: a query const `buscarCardsFTSSQL` troca o
  `AND e.type NOT IN ('season','episode')` fixo por
  `AND (e.type NOT IN ('season','episode') OR e.type IN (?, ?))`, onde os dois
  placeholders são os sub-nós a INCLUIR (string vazia `''` não casa nenhum type ⇒
  só-topo). Parâmetros posicionais: `(match, incl1, incl2, tipo, tipo, limit)`.
- `parseInclude(q)` lê o CSV `include` e devolve os dois slots (`season`/`""`,
  `episode`/`""`). Query hand-written mantida como **const Go ASCII** (regra sqlc).
- Combinação `type=season|episode` + `include` correspondente passa a funcionar (antes
  o `NOT IN` dura matava o resultado); `type=season` sem `include=season` segue vazio.

### Frontend (Vite/React)
- `web/src/components/search-panel.tsx`: dois toggles (`Button` shadcn, `aria-pressed`)
  **"Temporadas"** e **"Episódios"**, off por padrão. Ligar envia `include=` e refetch.
- Resultados locais partidos por `type` em seções: **"Filmes e séries"**
  (movie/tv/collection, sempre visível), **"Temporadas"** e **"Episódios"** (só quando
  o respectivo toggle está ligado). Grid reusado via `CardGridSection`.
- `searchMedia`/`searchQueryOptions` ganham `include?: string[]` (ordenado na chave de
  cache). Idioma e demais estados preservados.

### Arquivos afetados
- `internal/server/search.go`, `internal/server/search_test.go`.
- `internal/e2e/journeys_test.go` (jornada de série exercita `include=`).
- `openapi.yaml` (+ `web/src/lib/api-types.ts` regenerado).
- `web/src/lib/api.ts`, `web/src/lib/queries.ts`,
  `web/src/components/search-panel.tsx`, `web/src/components/search-panel.test.tsx`.
- `briefings/0032-...md` (nota de supersede), `briefings/README.md` (índice).

## 5. Critérios de aceite

- [x] Busca default (`sem include`) não traz `season`/`episode` (só topo).
- [x] `include=season` traz temporadas; `include=episode` traz episódios; `include=season,episode` traz os dois.
- [x] Valor inválido no `include` é ignorado (comportamento = default).
- [x] `type=season&include=season` retorna a temporada; `type=season` sozinho = vazio.
- [x] UI: toggles off por padrão; ligar abre a seção correspondente e envia `include=`.
- [x] Sub-nós aparecem em seções SEPARADAS, nunca misturados com filme/série.
- [x] Busca externa intacta; idioma preservado.
- [x] `go build/vet` limpos; testes Go, web e e2e verdes; catraca completa passa.

## 6. Notas de implementação / decisões

- **Contrato `include=season,episode` (CSV)** em vez de flags separadas
  (`?season=1&episode=1`): um só parâmetro, extensível, RESTie, e whitelist estrita
  (só `season`/`episode`) evita injeção de type arbitrário na query.
- **Sub-nós no mesmo `local[]`** (não em campos novos da resposta): a separação é de
  APRESENTAÇÃO (UI parte por `type`), não de contrato — mantém o DTO estável e o
  `SearchResult` sem novos campos. O server segue puro; a UI pós-processa.
- **Seção "Filmes e séries" sempre visível**; "Temporadas"/"Episódios" só com o toggle
  ligado (evita seção vazia ruidosa quando o usuário não pediu sub-nós).
- **HOME inalterada**: a decisão do 0032 para `ListarEntityCards` continua — só a BUSCA
  ganhou o opt-in.

## 7. Fora deste briefing / follow-ups

- Numeração/ordenação do episódio na busca quando o seletor de ordenação (Fase 3)
  existir — hoje o card mostra só o título.
- Um eventual filtro multi-tipo unificado (substituindo o `type` single-select + os
  toggles) fica para quando houver mais tipos de sub-nó.
