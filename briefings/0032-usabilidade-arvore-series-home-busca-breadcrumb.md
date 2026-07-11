# 0032 — Usabilidade da árvore de séries: home/busca só de topo + breadcrumb

> Tirar temporadas/episódios da home e da busca (mostrar só obras de topo) e dar
> navegação ascendente (breadcrumb Série › Temporada › Episódio).

| | |
|---|---|
| **ID** | 0032 |
| **Tipo** | fix/feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override (vertical séries) |
| **Status** | ready |
| **Docs relacionadas** | [ADR-0021](../docs/adr/0021-modelo-de-series-hierarquia-ordenacoes-vinculo-arquivo.md), [0031](0031-series-fundacao-modelo-enriquecimento.md) |
| **Depende de** | 0031 |

---

## 1. Objetivo

No 0031, temporada e episódio viraram **entities de primeira classe**. Efeito
colateral de usabilidade: eles vazaram para lugares onde só deveriam aparecer
obras de topo, e os sub-nós ficaram sem contexto de navegação. Três problemas do
dono:

1. **Home/biblioteca** carrega tudo misturado (filmes, séries **e** episódios/
   temporadas). Deveria mostrar só as **obras de topo**.
2. **Busca** de "Simpsons" traz a série **e** os episódios/temporadas (o FTS indexa
   toda entity) — confuso.
3. **Sem navegação ascendente:** dentro de uma temporada não há como voltar à série
   (nem diz a qual série pertence); dentro de um episódio, idem para série+temporada.

## 2. Jornada do usuário

1. O dono abre a **home** e vê filmes, séries e coleções — **não** episódios nem
   temporadas soltos.
2. Ele **busca** "Simpsons" e recebe a série (e filmes), sem a enxurrada de
   episódios/temporadas.
3. Ele entra na série → **Temporadas**; abre a **Temporada 1** e vê no topo um
   **breadcrumb** "Os Simpsons › Temporada 1", com "Os Simpsons" clicável.
4. Abre um **episódio** e vê "Os Simpsons › Temporada 1 › Episódio 1", cada nível
   ancestral clicável — voltando pela árvore.

## 3. Escopo

**Dentro:**
- **Tipos de topo** = `movie`, `tv`, `collection`. `season`/`episode` são **nós de
  navegação**, alcançados só pela árvore do pai.
- Home/biblioteca (`ListarEntityCards`/`ListarEntityCardsDesc`/`ContarEntityCards`)
  e **busca local** (FTS em `search.go`) excluem `season`/`episode`.
- **Breadcrumb** ascendente em temporada/episódio: DTO da entity ganha os
  **ancestrais** (via `ListarRelacoesPais`, subindo episódio→temporada→série); UI
  renderiza como trilha clicável (reusa o padrão do `ColecaoBand`).

**Fora (explicitamente):**
- Seções da home tipo "episódios recém-adicionados/atualizados" — **futuro** (abre
  margem para plugins). Só a exclusão dos sub-nós agora.
- Busca **externa** dos provedores — já retorna série/filme, não episódio; sem
  mudança.
- Seletor de ordenação, artwork de episódio — Fase 3 do 0031.

## 4. Contrato técnico

### Dados / API
- Queries de card (`entity.sql`) e a query FTS (`search.go`) ganham
  `AND e.type NOT IN ('season','episode')` (além do filtro de tipo opcional já
  existente). Regenerar sqlc (ASCII).
- DTO de `GET /entities/{id}` ganha `ancestors` (ou `breadcrumb`): array
  **raiz→pai** de `{id, title, type}`, montado no server (pós-resolução, como
  `anotarInternos`) subindo por `ListarRelacoesPais` até a raiz. Vazio para
  `movie`/`tv`/`collection`. Adicionar ao `openapi.yaml` + `gen:types`.

### Backend (Go)
- Filtros nas queries + handler. Ancestrais em `entities.go` (walk-up com
  `ListarRelacoesPais`; máx. 2 níveis: episódio→temporada→série). Robusto a
  ausência de pai (item solto) e a ciclos (defensivo).

### Frontend (Vite/React)
- Componente de breadcrumb no detalhe de `season`/`episode` (a partir de
  `ancestors`). Reusa o padrão do `ColecaoBand`. Home/busca não mudam (o backend
  já filtra).

### Arquivos afetados (previsão)
- `internal/db/queries/entity.sql`, `internal/server/search.go`,
  `internal/server/entities.go`, `openapi.yaml`; `web/src/routes/entity-detail.tsx`
  e/ou `web/src/components/rich-detail-view.tsx`, `web/src/lib/api-types.ts` (gen).

## 5. Critérios de aceite

- [ ] Home/biblioteca não lista `season`/`episode` (só `movie`/`tv`/`collection`).
- [ ] Busca local de "Simpsons" não traz episódios/temporadas.
- [ ] `GET /entities/{id}` de um episódio traz `ancestors` = [série, temporada]; de
      uma temporada = [série]; de filme/série/coleção = [].
- [ ] Página de temporada mostra breadcrumb com a série clicável; página de episódio
      mostra série + temporada clicáveis.
- [ ] Catraca completa verde; smoke ao vivo (Simpsons) navegando série→temporada→
      episódio e voltando pelo breadcrumb, sem erro de console.

## 6. Notas de implementação / decisões

- **Coleção fica na home** (é obra de topo, briefing 0025) — "só de topo" exclui os
  sub-nós de série, não a coleção.
- Ancestrais no server (não no resolver puro) — mesma fronteira do 0031/0026.

## 7. Fora deste briefing / follow-ups

- Seções de "recém-adicionados/atualizados" na home (episódios), extensíveis via
  plugin.
- Breadcrumb poderia exibir o **número** do episódio na ordenação corrente quando o
  seletor de ordenação (Fase 3) existir.
