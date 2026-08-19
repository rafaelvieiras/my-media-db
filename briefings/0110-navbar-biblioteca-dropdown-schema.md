# 0110 — Navbar: "Biblioteca" com dropdown por schema

> O item "Biblioteca" da navbar principal ganha um dropdown para ir direto
> pra biblioteca filtrada por um schema (filme, série, álbum, artista...).

| | |
|---|---|
| **ID** | 0110 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 5+ — Verticais e plugins (música) |
| **Status** | done |
| **Docs relacionadas** | — |
| **Depende de** | 0107 |

---

## 1. Objetivo

Hoje o item "Biblioteca" da navbar (`web/src/components/app-header.tsx`,
`NavLink to="/" exact`) é um link simples pra home/grid. O filtro de tipo
existente (`MediaTypeFilter`) só cobre `movie`/`tv`/`all`, vive como estado
local do `PosterGrid` (não linkável, não sobrevive a refresh) e não inclui
os tipos de música já catalogados desde 0105-0107. Este briefing dá acesso
direto, via dropdown na navbar, à biblioteca já filtrada por schema —
navegação mais rápida que abrir a home e depois trocar o filtro manualmente.

## 2. Jornada do usuário

1. O dono passa o mouse/clica em "Biblioteca" na navbar.
2. Abre um dropdown listando os schemas disponíveis (com dado catalogado) —
   ex.: Filmes, Séries, Álbuns, Artistas.
3. Clicar em um item navega para a biblioteca **já filtrada** por aquele
   tipo (URL com o filtro refletido — linkável e sobrevive a refresh,
   corrigindo a limitação atual de estado local não persistido na URL).
4. "Biblioteca" continua funcionando como link direto (clique no texto
   principal, sem abrir o dropdown) para a visão sem filtro — comportamento
   atual preservado.
5. No mobile (menu `Sheet` já existente), o dropdown se comporta de forma
   utilizável em toque (não depende de hover).

## 3. Escopo

**Dentro:**
- Navbar: trocar o `NavLink` simples de "Biblioteca" por um componente que
  combina link direto + `DropdownMenu` (shadcn, já instalado e em uso —
  `web/src/components/entity-manage-menu.tsx` é a referência de uso
  existente no projeto).
- Itens do dropdown vêm da mesma fonte de "schemas com dado" que 0109 usa
  (endpoint de tipos, se criado lá) — **não duplicar** essa descoberta,
  reaproveitar o hook/endpoint de 0109. Se 0109 ainda não estiver
  implementado quando este briefing rodar, implementar aqui e 0109
  reaproveita (registrar quem implementou primeiro nas notas).
- Filtro por tipo passa a refletir na URL da rota de biblioteca (query
  param `?type=X`), corrigindo a limitação atual de estado 100% local do
  `PosterGrid` (`web/src/components/poster-grid.tsx:39`).
- `MediaTypeFilter` (o filtro já existente na própria tela) e o novo
  dropdown da navbar ficam **sincronizados** com a mesma URL — mudar um
  reflete no outro.

**Fora (explicitamente):**
- Redesenho geral da navbar — só o item "Biblioteca" muda.
- Multi-seleção de tipos (dropdown seleciona um schema por vez, igual ao
  filtro atual).

## 4. Contrato técnico

### Dados / modelo
- Nenhuma mudança.

### API
- Reaproveita `GET /api/v1/entities?type=X` já existente. Reaproveita
  endpoint de tipos-com-dado de 0109 (ou implementa aqui se este briefing
  rodar primeiro).

### Backend (Go)
- Nenhuma mudança prevista além do que 0109 já cobre (endpoint de tipos).

### Frontend (Vite/React)
- `web/src/components/app-header.tsx`: novo item de navbar com
  `DropdownMenu` (trigger = texto "Biblioteca" + chevron), itens = schemas
  disponíveis.
- `web/src/routes/index.tsx` (ou onde a rota de biblioteca resolver morar):
  ler/escrever `?type=` na URL via search params do TanStack Router
  (`Route.useSearch`/`navigate`), substituindo o `useState` local por
  estado derivado da URL.
- `web/src/components/media-type-filter.tsx`: passa a ler/escrever o mesmo
  search param, mantendo sincronia com o dropdown da navbar.

### Arquivos afetados
- `web/src/components/app-header.tsx`
- `web/src/routes/index.tsx`
- `web/src/components/poster-grid.tsx`
- `web/src/components/media-type-filter.tsx`

## 5. Critérios de aceite

- [ ] Clicar num item do dropdown navega pra biblioteca filtrada e a URL
      reflete o filtro (`?type=album`, por exemplo).
- [ ] Dar refresh na página com `?type=album` na URL mantém o filtro
      aplicado (hoje isso se perde).
- [ ] Trocar o filtro pelo `MediaTypeFilter` da própria tela atualiza a URL
      e, se a navbar for reaberta, reflete a seleção atual.
- [ ] Mobile: dropdown funciona por toque dentro do `Sheet` existente.
- [ ] `tsc -b`, `npm run lint`, `npm run test:coverage` verdes.
- [ ] `make gate-local` limpo antes do commit final.

## 6. Notas de implementação / decisões

- Coordenar com 0109 pra não duplicar o endpoint/hook de "schemas com
  dado" — implementar uma vez, os dois consomem.

## 7. Fora deste briefing / follow-ups

- Contagem de itens por schema exibida no próprio dropdown (nice-to-have,
  avaliar se o endpoint de tipos já trouxer `count` de graça) — o endpoint
  (`GET /entities/types`, de 0109) já traz `count`, não usado no dropdown
  ainda.
- Validação em produção adiada para a rodada de deploy conjunta com
  0108/0109/0111.
