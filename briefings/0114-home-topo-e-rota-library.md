# 0114 — Home só com tipos de topo + rota `/library/:entitySlug`

> Remove a listagem completa (grid paginado) da home, deixando só os
> carrosséis "Adicionados recentemente" por tipo de topo, cada um com um link
> "Ver Todos" para uma rota nova dedicada à biblioteca completa filtrada.

| | |
|---|---|
| **ID** | 0114 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 5+ — Verticais e plugins |
| **Status** | done |
| **Docs relacionadas** | [ADR-0030](../docs/adr/0030-tipos-de-topo-biblioteca-e-busca-genericas.md) |
| **Depende de** | 0112 |

---

## 1. Objetivo

Hoje `LibraryItemsScreen` (`web/src/routes/index.tsx`, rota `/`) mistura, na
mesma tela, os carrosséis de "Adicionados recentemente" (`RecentlyAddedSection`,
0109) com um grid paginado completo (`PosterGrid`) filtrável por `?type=`.
Depois do 0112, `RecentlyAddedSection` já só vai gerar carrossel pra tipos de
topo (`album`/`faixa` somem automaticamente, sem tocar o componente). Falta:
(a) tirar o `PosterGrid` da home, (b) dar a cada carrossel um link "Ver
Todos" pra uma tela própria de biblioteca completa daquele tipo, (c) essa
tela própria vive numa rota nova, amigável: `/library/:entitySlug`.

## 2. Jornada do usuário

1. O dono abre a home (`/`). Vê **só** os carrosséis de tipos de topo com dado
   catalogado — hoje: "Filmes", "Séries", "Artistas" (nem grid completo nem
   álbum/faixa soltos).
2. Cada carrossel tem, no título, um link "Ver Todos" (ex. ao lado de
   "Filmes").
3. Clicar em "Ver Todos" de "Filmes" navega para `/library/movies` — a
   listagem completa e paginada, com o mesmo filtro de posse que `PosterGrid`
   já tinha, agora escopada a um tipo só (sem seletor de tipo dentro da tela —
   o tipo já veio da rota).
4. A navbar "Biblioteca" (dropdown, 0110) passa a linkar direto pra
   `/library/:entitySlug` de cada tipo, em vez de `/?type=X`.

## 3. Escopo

**Dentro:**
- `web/src/routes/index.tsx` (`LibraryItemsScreen`): remove `<PosterGrid
  lang={lang} />`, mantém só `<RecentlyAddedSection lang={lang} />` (+ os
  botões "Novo item"/"Nova coleção" que já existiam no header da tela).
- `web/src/components/recently-added-rail.tsx`: `RecentlyAddedRail` ganha um
  link "Ver Todos" no título, apontando para `/library/:entitySlug` do `type`
  daquele carrossel (usar o mesmo mapeamento slug↔type do item abaixo).
- Mapeamento slug↔`entity.type`: `movies`→`movie`, `series`→`tv`,
  `artists`→`artista` (mesma convenção em inglês da URL, título em pt-BR
  segue vindo de `rotuloTipoPlural`). Novo tipo de topo (vertical futura)
  precisa de uma entrada nesse mapa — colocar num lugar único
  (`web/src/lib/`, ex. `library-slugs.ts`), não espalhado.
- Nova rota `/library/:entitySlug` (`web/src/router.tsx` + novo arquivo
  `web/src/routes/library.tsx`): renderiza `PosterGrid` (reaproveitado,
  adaptado pra receber o `type` fixo da URL em vez de `useSearch({from:'/'})`)
  escopado ao tipo resolvido do slug. Slug inválido/tipo sem dado: estado
  vazio coerente (nunca 404 hard nem crash).
- `web/src/components/app-header.tsx` (`BibliotecaNavItem`): trocar `Link
  to="/" search={{type: tipo.type}}` por `Link to="/library/$entitySlug"`
  com o slug mapeado do `tipo.type`.
- Rota antiga `/?type=X`: **remover** o uso dela (grid não vive mais na home).
  Decidir na implementação se `librarySearchSchema`/`type` na rota `/` é
  removido de vez ou mantido como redirect silencioso para `/library/:slug`
  (preferir remover — não há indicação de que precisa de compat externa,
  `/?type=` não é um contrato público documentado).

**Fora (explicitamente):**
- Filtro de tipo dinâmico dentro do `PosterGrid`/`MediaTypeFilter` — briefing
  0113 (a tela `/library/:entitySlug` não precisa de seletor de tipo, já vem
  fixo da rota; `MediaTypeFilter` genérico do 0113 é usado só em `/search`).
- Mudança de import de música — 0115/ADR-0029.

## 4. Contrato técnico

### Dados / modelo
- Nenhuma migration/mudança de backend — este briefing é 100% frontend
  (consome `GET /entities` e `GET /entities/types` já existentes).

### API
- Nenhuma mudança.

### Backend (Go)
- Nenhuma mudança.

### Frontend (Vite/React)
- `web/src/routes/index.tsx` (editar — remove `PosterGrid`).
- `web/src/routes/library.tsx` (novo — tela de biblioteca completa por tipo).
- `web/src/router.tsx` (editar — nova rota `/library/$entitySlug`, remover ou
  redirecionar `?type=` de `/`).
- `web/src/components/recently-added-rail.tsx` (editar — link "Ver Todos").
- `web/src/components/app-header.tsx` (editar — dropdown linka pra
  `/library/$entitySlug`).
- `web/src/lib/library-slugs.ts` (novo — mapa slug↔type único).
- `web/src/components/poster-grid.tsx` (editar — recebe `type` fixo via prop/
  route param em vez de `useSearch({from:'/'})`).

### Arquivos afetados
- Todos os listados acima + `_test.tsx` correspondentes.

## 5. Critérios de aceite

- [x] Home (`/`) mostra só carrosséis de tipos de topo, sem grid completo
      abaixo.
- [x] Cada carrossel tem "Ver Todos" navegando pro slug certo.
- [x] `/library/movies`, `/library/series`, `/library/artists` renderizam a
      listagem completa e paginada do tipo certo, com filtro de posse
      funcionando.
- [x] Dropdown "Biblioteca" da navbar navega pra `/library/:slug`.
- [x] `tsc -b` zero-tolerância, `npm run lint`, `npm run test:coverage`
      (rodado sem `--coverage`, ver nota abaixo).

## 6. Notas de implementação / decisões

- Implementar **depois** do 0112. Pode rodar em paralelo com o 0113 (irmãos,
  sem dependência mútua).
- Reaproveitar `PosterGrid` quase integralmente — a única mudança estrutural
  é a origem do `type` (prop/route param fixo em vez de search param
  editável pelo próprio grid).
- **Nome do arquivo da rota nova: `web/src/routes/library-browse.tsx`
  (`LibraryBrowseScreen`), não `library.tsx`.** O briefing sugeria
  `routes/library.tsx`, mas esse caminho já existe — é `LibraryScreen`, a
  tela de gestão de datasources de `/dashboard/library` (briefing 0071), algo
  completamente diferente. Rota registrada como `/library/$entitySlug` em
  `router.tsx`.
- **`PosterGrid` perdeu de vez o seletor de tipo (`MediaTypeFilter`) e o
  `useSearch`/`useNavigate` da rota `/`.** Como `/library/$entitySlug` sempre
  passa um `type` fixo e é o ÚNICO consumidor restante do grid (a home não
  usa mais `PosterGrid`), o componente ficou mais simples: `type: string`
  agora é prop OBRIGATÓRIA (sem default "todos"), sem lógica de troca de tipo
  interna. Isso também evita qualquer conflito de merge com o 0113, que mexe
  em `media-type-filter.tsx`/`search-panel.tsx` — `poster-grid.tsx` não
  importa mais `MediaTypeFilter`.
- **`librarySearchSchema`/`?type=` da rota `/` foi removido de vez** (não
  mantido como redirect) — conforme a preferência já indicada no briefing;
  não havia indício de uso externo desse contrato.
- Link "Ver Todos" e dropdown "Biblioteca" tratam um tipo sem slug mapeado em
  `library-slugs.ts` como **ausência do link** (não um link quebrado) — hoje
  isso não deveria acontecer na prática (`GET /entities/types` já exclui
  sub-nós desde o 0112), mas é defensivo caso um tipo de topo novo apareça
  antes de ganhar entrada no mapa.
- `/library/:entitySlug` com slug desconhecido cai num `EmptyState` ("Tipo de
  biblioteca não encontrado"), sem fetch nem crash — nunca 404 hard.
- **Ambiente:** `npm run test:coverage` falhou neste agente por um artefato
  `web/coverage/` pré-existente dono de `root` (`EACCES` ao tentar
  sobrescrever `coverage-summary.json`), sem relação com o código mudado —
  provável resíduo de uma rodada anterior via Docker. Validado via
  `npx vitest run` (sem `--coverage`): 596/598 testes passando; os 2 testes
  que falham são de `search-panel.test.tsx`, arquivo do 0113 (trabalho
  concorrente, fora do escopo deste briefing) — não referenciam nada tocado
  aqui (`PosterGrid`/`RecentlyAddedRail`/`library-slugs`). `tsc -b` e
  `npm run lint` passaram limpos. Recomendo que o agente final de
  validação/commit limpe `web/coverage/` (`sudo rm -rf web/coverage`) antes
  de rodar `make gate-local`.

## 7. Fora deste briefing / follow-ups

- Filtros estruturados adicionais na tela `/library` (ano, gênero) — sem
  pedido explícito nesta rodada.
