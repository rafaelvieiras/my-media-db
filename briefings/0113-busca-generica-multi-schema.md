# 0113 — Busca genérica multi-schema com filtros estruturados

> `/search` já não tem hardcode real de `movie|tv` no Go — o hardcode vive no
> **frontend** (`MediaTypeFilter`, `SearchPanel`). Este briefing generaliza a
> UI de busca pra qualquer schema (reaproveitando `GET /entities/types`, como
> a home/navbar já fazem) e estrutura os filtros.

| | |
|---|---|
| **ID** | 0113 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 5+ — Verticais e plugins |
| **Status** | done |
| **Docs relacionadas** | [ADR-0030](../docs/adr/0030-tipos-de-topo-biblioteca-e-busca-genericas.md) |
| **Depende de** | 0112 |

---

## 1. Objetivo

`GET /search` (`internal/server/search.go`) já aceita `type` livre (sem
validar contra `movie|tv`) — a limitação real está no **frontend**:
`MediaTypeFilterValue = 'all' | 'movie' | 'tv'` (`web/src/components/
media-type-filter.tsx`) trava o `<Select>` de filtro a três opções fixas, e
`SearchPanel` (`web/src/components/search-panel.tsx`) separa resultados em
três baldes hardcoded ("Filmes e séries", "Temporadas", "Episódios") — sem
bucket pra artista/álbum/faixa (caem todos dentro de "Filmes e séries" hoje).
Depois do 0112 (`album`/`faixa` já saem da busca-padrão), falta o frontend
deixar de travar o filtro a dois tipos e passar a listar **todos os tipos de
topo reais da biblioteca** (`artista` incluso).

## 2. Jornada do usuário

1. O dono abre `/search`, digita um termo. O filtro de tipo mostra **todas as
   opções de topo que existem na biblioteca** (hoje: Filmes, Séries,
   Artistas — dinâmico via `GET /entities/types`, igual à navbar), não mais
   um `<Select>` fixo de 2 opções.
2. Resultados de artista aparecem normalmente na lista, sem precisar de um
   balde especial "Filmes e séries" com nome errado pra conter tipos que não
   são filme nem série.
3. Filtro por posse (`owned`) continua funcionando igual, agora explícito
   como parte do "filtro estruturado" (não muda de comportamento, só de
   organização/typing).

## 3. Escopo

**Dentro:**
- `MediaTypeFilter` (`web/src/components/media-type-filter.tsx`): trocar o
  enum fixo `'all' | 'movie' | 'tv'` por `string` (o `type` real, "all" como
  sentinela de "sem filtro"), populando as opções via `entityTypesQueryOptions`
  (mesmo hook já usado por `RecentlyAddedSection`/`BibliotecaNavItem`),
  rotulando com `rotuloTipoPlural` (já existe, `web/src/lib/entity-badges.ts`).
  Mesmo componente serve `PosterGrid` (biblioteca, briefing 0114 aqui do lado)
  e `SearchPanel`.
- `SearchPanel`: substituir os 3 baldes hardcoded por agrupamento genérico —
  um grupo por `type` retornado nos resultados, título via
  `rotuloTipoPlural(type)`, mantendo season/episode agrupados sob a série-mãe
  quando `include` os trouxer (não perder a UX atual de navegação de série).
- Confirmar que o backend `/search` (já genérico, conforme achado do mapa
  técnico) não precisa de mudança de filtro adicional além do que 0112 já fez
  — só validar que filtros combinados (`type` + `q` + `owned`, se `owned` já
  for aceito por `/search`; caso não seja, adicionar, espelhando o filtro que
  `/entities` já tem) funcionam.
- `web/src/lib/api.ts`: atualizar o comentário/tipo que hoje documenta (só em
  comentário, não em código real) "`type` opcional filtra por movie|tv" — está
  desatualizado mesmo antes deste briefing.

**Fora (explicitamente):**
- Exclusão de sub-nó da busca — já resolvido no 0112.
- Mudança de import de música — 0115/ADR-0029.

## 4. Contrato técnico

### Dados / modelo
- Nenhuma migration.

### API
- `GET /search`: confirmar/adicionar suporte a `owned` (bool) como filtro
  estruturado adicional, se ainda não existir — mesmo shape de `/entities`.
  Sem mudança de shape de resposta.

### Backend (Go)
- `internal/server/search.go`: só se `owned` precisar ser adicionado como
  filtro (verificar durante a implementação se já existe); caso contrário,
  este briefing é 100% frontend.

### Frontend (Vite/React)
- `web/src/components/media-type-filter.tsx` (editar — tipo genérico, opções
  dinâmicas).
- `web/src/components/search-panel.tsx` (editar — agrupamento genérico por
  tipo retornado, em vez de 3 baldes fixos).
- `web/src/components/poster-grid.tsx` (consumidor de `MediaTypeFilter`,
  confirmar que continua funcionando com o tipo genérico — sem mudança de
  comportamento aqui, só de tipagem).
- `web/src/lib/api.ts` (comentário desatualizado).

### Arquivos afetados
- `web/src/components/media-type-filter.tsx`, `.test.tsx`
- `web/src/components/search-panel.tsx`, `.test.tsx`
- `internal/server/search.go`, `_test.go` (se `owned` precisar entrar)

## 5. Critérios de aceite

- [ ] Filtro de tipo em `/search` e na biblioteca lista dinamicamente todos os
      tipos de topo com dado catalogado (não mais preso a `movie|tv`) —
      confirmado com música: "Artistas" aparece como opção.
- [ ] Resultado de busca de um artista aparece agrupado corretamente (não
      dentro de um balde rotulado "Filmes e séries").
- [ ] Regressão: busca de filme/série continua idêntica ao comportamento
      atual (season/episode seguem agrupados sob a série via `include`).
- [ ] `tsc -b` zero-tolerância, `npm run lint`, `npm run test:coverage`.
- [ ] `go build ./...`/`go vet ./...`/`go test ./... -count=1` verdes (se
      backend tocado).

## 6. Notas de implementação / decisões

- Implementar **depois** do 0112 estar pronto (a lista de tipos de topo que
  este briefing consome já precisa excluir album/faixa corretamente).
- Pode rodar em paralelo com o 0114 (home/rota `/library`) — são consumidores
  irmãos do mesmo 0112, sem dependência um do outro.

### Implementado (frontend, escopo §3/§4 deste briefing)

- `MediaTypeFilter` (`media-type-filter.tsx`) virou um componente "esperto":
  passou a fazer sua PRÓPRIA `useQuery(entityTypesQueryOptions)` (mesma fonte
  de `RecentlyAddedSection`/`BibliotecaNavItem`) e renderizar `<option>` "Todos"
  + uma por tipo real, rotulada via `rotuloTipoPlural`. `MediaTypeFilterValue`
  virou `string` (era `'all'|'movie'|'tv'`); `'all'` continua o sentinela de
  "sem filtro". Decisão de onde colocar a query: DENTRO do componente (não
  como prop dos consumidores) — assim `SearchPanel` e qualquer consumidor
  futuro ganham o comportamento dinâmico automaticamente, sem replicar a
  `useQuery` em cada tela.
- `SearchPanel`/`search-panel.tsx`: os cards de TOPO (excluindo season/episode,
  que mantêm suas seções próprias e o comportamento de toggle inalterado) agora
  são agrupados por `agruparPorTipo` (novo helper) — uma seção por `type` real
  devolvido, título via `rotuloTipoPlural`. Ordem dos grupos = ordem de
  primeira aparição em `data.local` (ordem de relevância do FTS5), sem
  reordenar por nome. **Decisão não 100% especificada pelo briefing**: quando
  `gruposTopo` fica vazio (zero cards de topo no resultado local), em vez de
  não renderizar seção nenhuma (o que faria a tela pular direto pra "Resultados
  externos" sem indicar que o catálogo FOI consultado), mantive uma seção
  fixa "Catálogo" com o `emptyLabel` de sempre — evita regressão de UX frente
  ao comportamento anterior (que sempre mostrava "Filmes e séries", mesmo
  vazio).
- `poster-grid.tsx` **não foi tocado**: o briefing 0114 (rodando em paralelo,
  mesmo working tree) já havia removido o `MediaTypeFilter`/`useSearch` de
  dentro do `PosterGrid` antes desta implementação terminar — o tipo agora
  chega FIXO via prop `type` (resolvido do slug de `/library/:entitySlug`), e
  não há mais seletor de tipo dentro do grid. Confirmado por leitura do
  arquivo já modificado; nada do escopo §3/§4 sobrou pendente ali.
- `lib/api.ts`: comentário de `searchMedia` atualizado (não filtra mais só
  `movie|tv`, é `type` livre desde o backend do 0112).
- Testes: `media-type-filter.test.tsx` (novo — lista dinâmica, degrade em
  falha do fetch, `onChange` com o `type` real) e `search-panel.test.tsx`
  atualizado (rotas `fetchRouter` passaram a servir `GET /entities/types`;
  asserção "Filmes e séries" trocada por seção "Filmes" via `getByRole`
  `heading` — o texto "Filmes" deixou de ser único na tela porque também é
  `<option>` do Select dinâmico; teste novo cobrindo agrupamento de um tipo
  não-filme/série, artista, em seção própria).
- `owned` como filtro estruturado de `/search` (mencionado na §4 do briefing)
  **não foi tocado** por este agente: `internal/server/search.go` está sendo
  modificado por trabalho concorrente de outro briefing no mesmo working tree
  (fora do escopo de arquivos frontend delegado a esta implementação).

## 7. Fora deste briefing / follow-ups

- Filtros adicionais (ano, gênero) ficam para quando houver demanda real —
  não pedido explicitamente pelo dono nesta rodada.
