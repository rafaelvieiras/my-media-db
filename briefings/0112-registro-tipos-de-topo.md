# 0112 — Registro de tipos "de topo" (fundação)

> Fonte única de verdade Go pra "este `entity.type` é navegável de topo (home/
> navbar/busca/`/library`) ou é sub-nó (só via drill-down do pai)" — substitui
> duas exclusões hardcoded divergentes que hoje só cobrem `season`/`episode`.

| | |
|---|---|
| **ID** | 0112 |
| **Tipo** | chore/fundação |
| **Fase (roadmap)** | Fase 5+ — Verticais e plugins |
| **Status** | ready |
| **Docs relacionadas** | [ADR-0030](../docs/adr/0030-tipos-de-topo-biblioteca-e-busca-genericas.md) |
| **Depende de** | — |

---

## 1. Objetivo

Hoje existem **dois** pontos no backend que excluem sub-nós de listagens
"gerais" de tipo, cada um reimplementando a própria lista, e nenhum cobrindo
`album`/`faixa`:

- `internal/db/queries/entity.sql` — query `ListarTiposComContagem` (usada por
  `GET /entities/types`, que alimenta a home e o dropdown "Biblioteca" da
  navbar): `WHERE type NOT IN ('season', 'episode')`.
- `internal/server/search.go` — SQL `buscarCardsFTSSQL` (usada por
  `GET /search`): mesma exclusão, hardcoded separadamente, religável via
  `?include=season,episode`.

Consequência real observada em produção: com música catalogada, `album` e
`faixa` aparecem soltos na busca e ganham carrossel/entrada própria na home e
na navbar — não fazem sentido fora do contexto do artista/álbum pai. Este
briefing cria o registro único que os briefings 0113 (busca) e 0114
(home/rota `/library`) vão consumir, e que qualquer vertical futura (ex.
videoclipe) só precisa referenciar, sem tocar SQL espalhado.

## 2. Jornada do usuário

Não abre UI nova — é fundação consumida pelos próximos dois briefings. Efeito
observável só depois de 0113/0114: álbum/faixa somem da home, da navbar e da
busca-padrão, continuando acessíveis normalmente via drill-down (tela do
artista → álbuns → tela do álbum → faixas).

## 3. Escopo

**Dentro:**
- Constante/lista Go em `internal/server` (pacote onde `kindEOrdemMembros` já
  vive, `internal/server/entities.go`) definindo os tipos de **sub-nó**:
  `season`, `episode`, `album`, `faixa` — nome sugerido
  `tiposSubNo = []string{tipoEntTemporada, tipoEntEpisodio, tipoEntAlbum, "faixa"}`
  (reaproveitar as constantes `tipoEnt*` já existentes no arquivo). Decisão de
  design: **lista de exclusão** (sub-nó), não de inclusão (topo) — schema novo
  sem entrada na lista já nasce "de topo" por padrão, que é o caso mais comum
  (filme, série, artista foram todos assim); só sub-nós explícitos precisam
  entrar na lista. Se o crescimento futuro do catálogo tornar sub-nó o caso
  mais comum, inverter para lista de inclusão é um `git blame` de distância.
- Query sqlc `ListarTiposComContagem` passa a receber a exclusão via
  parâmetro (`sqlc.slice` ou array bind) em vez de `NOT IN ('season',
  'episode')` fixo no `.sql` — o handler Go (`listEntityTypes`,
  `entities.go:284`) passa `tiposSubNo` na chamada.
- `buscarCardsFTSSQL`/`search.go` passa a excluir os mesmos `tiposSubNo` por
  padrão (substituindo `type NOT IN ('season', 'episode')` pela lista
  centralizada), preservando o mecanismo existente de `?include=season,
  episode` — generalizar `include` para aceitar qualquer sub-tipo da lista
  (não só season/episode), já que `faixa`/`album` também podem fazer sentido
  incluídos numa busca textual futura (ex. buscar pelo nome de uma faixa
  específica).
- Testes: `ListarTiposComContagem` exclui album/faixa mesmo com dado
  catalogado; `/search` sem `include` não retorna album/faixa; `/search
  ?include=album,faixa` retorna.

**Fora (explicitamente):**
- Qualquer mudança de UI (home, navbar, filtro) — briefings 0113/0114.
- Mudança de comportamento do import de música (briefing 0115/ADR-0029) — este
  briefing só toca navegação/listagem, não o que é criado no banco.

## 4. Contrato técnico

### Dados / modelo
- Nenhuma migration. Query sqlc muda de `NOT IN (literal)` para `NOT IN
  (sqlc.slice('subnos'))` (ou equivalente do dialeto suportado pelo driver
  `modernc.org/sqlite` + sqlc — confirmar sintaxe suportada durante a
  implementação; se `sqlc.slice` não for suportado pelo dialeto configurado,
  alternativa aceitável é expandir `?` dinamicamente em Go antes de chamar a
  query, mesmo padrão eventualmente já usado em outro filtro dinâmico do
  projeto).

### API
- `GET /entities/types` — mesmo contrato de resposta (`{type, count}[]`),
  só muda o que é excluído (`album`/`faixa` somem, `season`/`episode` como já
  era).
- `GET /search?include=` — mesmo parâmetro, generalizado para aceitar
  qualquer valor de `tiposSubNo`, não só `season`/`episode`.

### Backend (Go)
- `internal/server/entities.go`: nova constante `tiposSubNo` (ou nome melhor
  encontrado durante a implementação — manter coerência com `kindEOrdemMembros`
  já existente no mesmo arquivo).
- `internal/db/queries/entity.sql` + `internal/db/sqlcgen/*` (editar/gerado).
- `internal/server/search.go`: consumir `tiposSubNo` em vez da lista própria;
  generalizar `parseInclude`.

### Frontend (Vite/React)
- Nenhuma mudança neste briefing.

### Arquivos afetados
- `internal/server/entities.go` (editar)
- `internal/db/queries/entity.sql`, `internal/db/sqlcgen/*` (editar/gerado)
- `internal/server/search.go` (editar)
- `internal/server/entities_test.go`, `internal/server/search_test.go` (editar/novo)

## 5. Critérios de aceite

- [ ] `GET /entities/types` nunca retorna `album`/`faixa`, mesmo com dado
      catalogado (teste cobre isso explicitamente — regressão do achado em
      produção).
- [ ] `GET /search?q=...` sem `include` nunca retorna `album`/`faixa`/`season`/
      `episode`; com `include=album,faixa` retorna.
- [ ] `go build ./...`, `go vet ./...`, `go test ./... -count=1` verdes.
- [ ] `make gate-fast` limpo durante a iteração; `make gate-local` reservado
      pro agente final (junto dos briefings 0113-0115).

## 6. Notas de implementação / decisões

- Este briefing é a base dos dois seguintes (0113, 0114) — implementar
  primeiro e sozinho, mesmo padrão de "contrato antes de back+front" já usado
  no projeto (ver [memória de sessões anteriores]: 1 agente fecha a fundação,
  os consumidores vêm depois em paralelo).

## 7. Fora deste briefing / follow-ups

- **`/search` genérico com filtros estruturados** — briefing 0113.
- **Home top-level-only + rota `/library/:entitySlug`** — briefing 0114.
