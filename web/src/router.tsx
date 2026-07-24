import {
  Outlet,
  createRootRouteWithContext,
  createRoute,
  createRouter,
  redirect,
} from '@tanstack/react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { z } from 'zod'
import { AppHeader } from '@/components/app-header'
import { DashboardShell } from '@/components/dashboard-shell'
import { LanguageProvider } from '@/lib/language-context'
import { LibraryItemsScreen } from '@/routes/index'
import { LibraryScreen } from '@/routes/library'
import { LibraryNewScreen } from '@/routes/library-new'
import { LibraryEditScreen } from '@/routes/library-edit'
import { SearchScreen } from '@/routes/search'
import { ReviewScreen } from '@/routes/review'
import { OrphansScreen } from '@/routes/orphans'
import { EntityDetailScreen } from '@/routes/entity-detail'
import { EntityEditBasicoScreen } from '@/routes/entity-edit-basico'
import { EntityEditIdsScreen } from '@/routes/entity-edit-ids'
import { EntityEditCreditosScreen } from '@/routes/entity-edit-creditos'
import { ItemNewScreen } from '@/routes/item-new'
import { CollectionNewScreen } from '@/routes/collection-new'
import { ExternalDetailScreen } from '@/routes/external-detail'
import { CollectionDetailScreen } from '@/routes/collection-detail'
import { PersonDetailScreen } from '@/routes/person-detail'
import { SettingsScreen } from '@/routes/settings'
import { JobsScreen } from '@/routes/jobs'
import { HealthScreen } from '@/routes/health'

// Contexto do router: carrega o QueryClient compartilhado, de modo que loaders
// de rota possam pré-carregar/hidratar o cache do TanStack Query.
export interface RouterContext {
  queryClient: QueryClient
}

// QueryClient único, compartilhado entre o contexto do router e o provider.
export const queryClient = new QueryClient()

// Roteamento CODE-BASED. A raiz é o layout: cabeçalho de navegação + <Outlet>.
const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: () => (
    <div className="min-h-svh bg-background text-foreground">
      <AppHeader />
      <Outlet />
    </div>
  ),
})

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  component: LibraryItemsScreen,
})

// Área administrativa (briefing 0083): as 6 telas de operação/config viram
// subrotas de `/dashboard`, navegadas pela sidebar do `DashboardShell` — em vez
// de soltas no `AppHeader`. `dashboardIndexRoute` redireciona `/dashboard` para
// a primeira seção (Bibliotecas).
const dashboardRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/dashboard',
  component: DashboardShell,
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

// Rotas dedicadas de datasource (briefing 0071): criar caminho/tipo e reapontar
// caminho/remover saem do formulário fixo + edição inline da lista para páginas
// próprias — o mesmo padrão de `entityEditBasicoRoute`/`itemNewRoute` abaixo.
const dashboardLibraryNewRoute = createRoute({
  getParentRoute: () => dashboardRoute,
  path: '/library/new',
  component: LibraryNewScreen,
})

const dashboardLibraryEditRoute = createRoute({
  getParentRoute: () => dashboardRoute,
  path: '/library/$id/edit',
  component: LibraryEditScreen,
})

const searchRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/search',
  component: SearchScreen,
})

const dashboardReviewRoute = createRoute({
  getParentRoute: () => dashboardRoute,
  path: '/review',
  component: ReviewScreen,
})

const dashboardOrphansRoute = createRoute({
  getParentRoute: () => dashboardRoute,
  path: '/orphans',
  component: OrphansScreen,
})

// Rotas de CRUD dedicadas (briefing 0030): criar item/coleção e editar item saem
// dos formulários inline para páginas próprias (deep-link + testabilidade).
const itemNewRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/new/item',
  component: ItemNewScreen,
})

const collectionNewRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/new/collection',
  component: CollectionNewScreen,
})

// Search param `ordering` do detalhe da série (0040): a ordem de exibição de
// episódios escolhida no seletor vive na URL (compartilhável, efêmera — sem
// persistência). Opcional; ausente = ordem default da série (view de temporadas).
const entityDetailSearchSchema = z.object({
  ordering: z.string().optional(),
})

const entityDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/entities/$id',
  component: EntityDetailScreen,
  validateSearch: entityDetailSearchSchema,
})

// Edição de item (briefing 0030), dividida em subrotas no padrão sidebar do
// TMDB (briefing 0080): `/entities/$id/edit` continua sendo a URL default —
// agora a seção "Informações básicas" —, com IDs de terceiros e Elenco/equipe
// em subrotas próprias (deep-linkáveis), todas FLAT (mesmo padrão das demais
// rotas deste arquivo — não uma rota de layout aninhada).
const entityEditBasicoRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/entities/$id/edit',
  component: EntityEditBasicoScreen,
})

const entityEditIdsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/entities/$id/edit/ids',
  component: EntityEditIdsScreen,
})

const entityEditCreditosRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/entities/$id/edit/creditos',
  component: EntityEditCreditosScreen,
})

// Detalhe rico do item externo ANTES de importar (0023). Params: fonte, tipo
// (`movie|tv`) e id NA FONTE. Similares/coleção navegam recursivamente aqui.
const externalDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/external/$source/$type/$id',
  component: ExternalDetailScreen,
})

// Coleção externa como página própria (0024, follow-up do 0023). Params: fonte e id
// da coleção NA FONTE. Alcançada pela faixa de coleção do detalhe rico.
const collectionDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/collection/$source/$id',
  component: CollectionDetailScreen,
})

// Página MÍNIMA da pessoa (0029, fase D, interno-first). Alcançada pelo card de
// elenco/equipe da RichDetailView quando o crédito traz `person_id` (a pessoa já foi
// persistida por algum import).
const personDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/person/$personId',
  component: PersonDetailScreen,
})

const dashboardSettingsRoute = createRoute({
  getParentRoute: () => dashboardRoute,
  path: '/settings',
  component: SettingsScreen,
})

// Administração dos jobs internos (0052) — agendador embutido + histórico.
const dashboardJobsRoute = createRoute({
  getParentRoute: () => dashboardRoute,
  path: '/jobs',
  component: JobsScreen,
})

const dashboardHealthRoute = createRoute({
  getParentRoute: () => dashboardRoute,
  path: '/health',
  component: HealthScreen,
})

const routeTree = rootRoute.addChildren([
  indexRoute,
  searchRoute,
  itemNewRoute,
  collectionNewRoute,
  entityDetailRoute,
  entityEditBasicoRoute,
  entityEditIdsRoute,
  entityEditCreditosRoute,
  externalDetailRoute,
  collectionDetailRoute,
  personDetailRoute,
  dashboardRoute.addChildren([
    dashboardIndexRoute,
    dashboardLibraryRoute,
    dashboardLibraryNewRoute,
    dashboardLibraryEditRoute,
    dashboardReviewRoute,
    dashboardOrphansRoute,
    dashboardSettingsRoute,
    dashboardJobsRoute,
    dashboardHealthRoute,
  ]),
])

export const router = createRouter({
  routeTree,
  context: { queryClient },
  // Wrap envolve toda a árvore do router com o provider do TanStack Query,
  // usando o mesmo client que está no contexto.
  Wrap: ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={queryClient}>
      <LanguageProvider>{children}</LanguageProvider>
    </QueryClientProvider>
  ),
})

// Registro de tipos: dá type-safety global ao router (navegação/params tipados).
declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
