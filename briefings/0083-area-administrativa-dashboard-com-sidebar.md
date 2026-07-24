# 0083 — Área administrativa unificada em `/dashboard`, com sidebar

> A navegação hoje espalha 6 telas de operação/config (Bibliotecas, Revisão,
> Órfãs, Jobs, Configurações, Saúde) soltas no `AppHeader`, com um agrupamento
> visual frágil ("Administração" por borda+rótulo). O dono quer essas 6 telas
> viverem sob `/dashboard`, com um menu lateral fixo — no mesmo padrão de
> sidebar já usado na edição de item (`EntityEditShell`, briefing 0080).

| | |
|---|---|
| **ID** | 0083 |
| **Tipo** | feature (refactor de rota) |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | ready |
| **Docs relacionadas** | [03-arquitetura](../docs/03-arquitetura.md) |
| **Depende de** | 0071, 0075, 0080 |

---

## 1. Objetivo

Consolidar as telas de **operação/administração** — hoje soltas como rotas
flat (`/library`, `/review`, `/orphans`, `/jobs`, `/settings`, `/health`) — sob
um único namespace `/dashboard`, navegável por um **menu lateral** (sidebar),
no lugar de ficarem misturadas com a navegação principal (Biblioteca/Buscar) no
`AppHeader`. Isso separa claramente "usar o catálogo" (Biblioteca, Buscar) de
"administrar o catálogo" (as 6 telas abaixo), e dá lugar para crescer o menu
administrativo sem inchar o header.

## 2. Jornada do usuário

1. **Hoje**: o dono clica em "Bibliotecas", "Revisão", "Órfãs", "Jobs",
   "Configurações" ou "Saúde" — todos soltos lado a lado no `AppHeader`
   (Jobs/Configurações agrupados só por uma borda com rótulo
   "Administração").
2. **Depois deste briefing**: o `AppHeader` mostra um único link
   "Dashboard" (ícone `LayoutDashboard`). Clicar nele leva a `/dashboard`,
   que redireciona para `/dashboard/library` (primeira seção).
3. Dentro de `/dashboard`, uma sidebar à esquerda lista as 6 seções
   (Bibliotecas, Revisão, Órfãs, Jobs, Configurações, Saúde); a área
   principal mostra só a tela da seção ativa. Trocar de seção troca a URL
   (`/dashboard/review`, `/dashboard/orphans`, etc.) — deep-linkável,
   compartilhável, sobrevive a refresh.
4. Em telas estreitas (mobile), a sidebar colapsa no mesmo padrão responsivo
   que o `AppHeader` já usa (0075): vira um `Sheet` acionado por botão, sem
   nunca comprimir os links a ponto de ficarem ilegíveis.
5. Nenhuma URL antiga (`/library`, `/review`, `/orphans`, `/jobs`,
   `/settings`, `/health`) precisa continuar funcionando — é app
   local-first, uso pessoal do dono, sem terceiros com bookmarks a
   preservar. A mudança é um MOVE, não uma cópia com redirect.

## 3. Escopo

**Dentro:**
- Nova rota-layout `/dashboard` (código, `createRoute` com componente próprio
  + `<Outlet/>`), com uma rota-índice que redireciona para
  `/dashboard/library`.
- Mover as 6 telas para subrotas de `/dashboard`:
  - `/library` → `/dashboard/library`
  - `/library/new` → `/dashboard/library/new`
  - `/library/$id/edit` → `/dashboard/library/$id/edit`
  - `/review` → `/dashboard/review`
  - `/orphans` → `/dashboard/orphans`
  - `/jobs` → `/dashboard/jobs`
  - `/settings` → `/dashboard/settings`
  - `/health` → `/dashboard/health`
- Componente `DashboardShell` (novo, `web/src/components/dashboard-shell.tsx`),
  inspirado no `EntityEditShell` (0080): sidebar com os 6 itens (ícone +
  rótulo), item ativo destacado (`aria-current="page"`), responsivo
  (`Sheet` abaixo do breakpoint `md`, mesmo padrão do `AppHeader` 0075).
- Atualizar o `AppHeader`: remover os 6 links soltos + o agrupamento
  "Administração"; adicionar 1 link "Dashboard" apontando para
  `/dashboard`.
- Atualizar TODOS os `<Link to="...">`/`navigate({ to: ... })`/
  `useParams({ from: ... })` que hoje apontam para as 6 rotas antigas (grep
  por `/library`, `/review`, `/orphans`, `/jobs`, `/settings`, `/health` em
  `web/src`), incluindo testes.
- Atualizar a numeração/nomes dos arquivos de rota-fonte só se necessário
  para clareza (os arquivos de tela em si — `routes/library.tsx`,
  `routes/review.tsx` etc. — **não precisam mudar de nome de arquivo**, só
  o `path` registrado no `router.tsx` muda).

**Fora (explicitamente):**
- Redirects de compatibilidade das URLs antigas — não há terceiros/bookmarks
  a preservar (uso pessoal, local-first).
- Mudar o CONTEÚDO de qualquer uma das 6 telas (Bibliotecas, Revisão, Órfãs,
  Jobs, Configurações, Saúde) — só a rota/navegação até elas.
- Permissões/autenticação por seção — fora de escopo deste projeto hoje.
- Mexer nas rotas de "uso do catálogo" (`/`, `/search`, `/entities/$id`,
  `/new/item`, `/new/collection`, `/external/...`, `/collection/...`,
  `/person/...`) — continuam soltas no `AppHeader` como estão.

---

## 4. Contrato técnico

### 4.1 Rotas (`web/src/router.tsx`)

Nova rota-layout, pai das 6 subrotas:

```ts
const dashboardRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/dashboard',
  component: DashboardShell, // sidebar + <Outlet/>
})

const dashboardIndexRoute = createRoute({
  getParentRoute: () => dashboardRoute,
  path: '/',
  beforeLoad: () => {
    throw redirect({ to: '/dashboard/library' })
  },
})

const dashboardLibraryRoute = createRoute({
  getParentRoute: () => dashboardRoute,
  path: '/library',
  component: LibraryScreen,
})
// ...idem library/new, library/$id/edit, review, orphans, jobs, settings, health
```

Todas as 6 telas trocam `getParentRoute: () => rootRoute` por
`getParentRoute: () => dashboardRoute`, e o `path` perde o prefixo (`/library`
vira `/library` relativo — o TanStack Router concatena com o pai
`/dashboard`). `routeTree` ganha `dashboardRoute.addChildren([...])` no lugar
das 8 entradas soltas que saem da lista de `rootRoute.addChildren([...])`.

`useParams({ from: '/library/$id/edit' })` em `library-edit.tsx` passa a
`useParams({ from: '/dashboard/library/$id/edit' })`.

### 4.2 `DashboardShell` (novo componente)

Sidebar com os 6 itens, cada um com `to` + ícone (reaproveitar os já
importados no `AppHeader` hoje: `Library`/`ListChecks`/`Trash2`/`Briefcase`/
`Settings`; escolher um ícone novo para Saúde, ex. `Activity` ou
`HeartPulse`, do `lucide-react`). Estrutura análoga ao `EntityEditShell`
(0080): `<nav aria-label="...">` com os `Link`, `aria-current="page"` no
ativo via `activeProps`/rota atual, `<Outlet/>` para a tela da seção.
Responsivo: usa o MESMO padrão de `Sheet` que o `AppHeader` (0075) — sidebar
vira painel deslizante abaixo de `md`, com botão de abrir/fechar.

### 4.3 `AppHeader` (`web/src/components/app-header.tsx`)

Remove `LinksNavegacao`'s 6 entradas relacionadas
(`/library`→Bibliotecas, `/review`, `/orphans`, o `<div role="group"
aria-label="Administração">` com `/settings`+`/jobs`, e `/health`).
Adiciona um único `NavLink` novo: `to="/dashboard"` com ícone
`LayoutDashboard`, rótulo "Dashboard". Mantém `/` (Biblioteca) e `/search`
(Buscar) como estão.

### 4.4 Links a atualizar (grep, não exaustivo — validar com busca real)

| Arquivo | Link antigo | Link novo |
|---|---|---|
| `components/poster-grid.tsx` | `to="/library"` | `to="/dashboard/library"` |
| `components/datasource-list.tsx` | `to="/library/new"`, `to="/library/$id/edit"` | `to="/dashboard/library/new"`, `to="/dashboard/library/$id/edit"` |
| `components/library-form-header.tsx` | `to="/library"` | `to="/dashboard/library"` |
| `components/entity-action-buttons.tsx` | `to="/review"` | `to="/dashboard/review"` |
| `components/poster-card.tsx` | `to="/review"` (×2) | `to="/dashboard/review"` |
| `routes/library-new.tsx` | `navigate({ to: '/library' })` | `navigate({ to: '/dashboard/library' })` |
| `routes/library-edit.tsx` | `useParams({ from: '/library/$id/edit' })`, `navigate({ to: '/library' })` (×2) | equivalentes com prefixo `/dashboard` |
| `routes/entity-detail.tsx` | `to="/library"` | `to="/dashboard/library"` |

Todos os testes que montam o router de teste (`test/router-mock.tsx`) ou
navegam para essas rotas via `screen`/`userEvent` precisam do mesmo ajuste de
path — rodar `tsc -b` (zero-tolerância) pega qualquer `to=` com path
inexistente, já que as rotas são type-safe.

## 5. Critérios de aceite

- [ ] `/dashboard` existe, redireciona para `/dashboard/library`.
- [ ] As 6 telas respondem em `/dashboard/library`, `/dashboard/review`,
      `/dashboard/orphans`, `/dashboard/jobs`, `/dashboard/settings`,
      `/dashboard/health` (e `/dashboard/library/new`,
      `/dashboard/library/$id/edit`), cada uma renderizando dentro do
      `DashboardShell` com a sidebar visível.
- [ ] Sidebar destaca a seção ativa; navegar entre seções não perde o
      layout (só troca a área principal).
- [ ] Em viewport mobile, a sidebar colapsa para um `Sheet`/painel,
      igual ao padrão do `AppHeader`.
- [ ] `AppHeader` não tem mais os 6 links soltos nem o grupo
      "Administração" — só o novo link "Dashboard".
- [ ] Nenhum link/navegação interno aponta mais para as rotas antigas
      (`/library`, `/review`, `/orphans`, `/jobs`, `/settings`, `/health`
      soltas, fora do prefixo `/dashboard`).
- [ ] `tsc -b`, `npm run lint`, `npm run test:coverage` e `make gate-local`
      passam.

## 6. Decisões tomadas neste briefing

- **Sem redirects de compatibilidade**: app local-first de uso pessoal, sem
  terceiros com bookmarks — mover é mais simples que mover+redirecionar.
- **Sidebar reaproveita o padrão visual do `EntityEditShell`** (0080) em vez
  de inventar um componente novo do zero — consistência visual entre as
  duas sidebars que o app passa a ter.
