import {
  Outlet,
  createRootRouteWithContext,
  createRoute,
  createRouter,
} from '@tanstack/react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { z } from 'zod'
import { AppHeader } from '@/components/app-header'
import { LanguageProvider } from '@/lib/language-context'
import { LibraryItemsScreen } from '@/routes/index'
import { LibraryScreen } from '@/routes/library'
import { LibraryNewScreen } from '@/routes/library-new'
import { LibraryEditScreen } from '@/routes/library-edit'
import { SearchScreen } from '@/routes/search'
import { ReviewScreen } from '@/routes/review'
import { OrphansScreen } from '@/routes/orphans'
import { EntityDetailScreen } from '@/routes/entity-detail'
import { EntityEditScreen } from '@/routes/entity-edit'
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

const libraryRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/library',
  component: LibraryScreen,
})

// Rotas dedicadas de datasource (briefing 0071): criar caminho/tipo e reapontar
// caminho/remover saem do formulário fixo + edição inline da lista para páginas
// próprias — o mesmo padrão de `entityEditRoute`/`itemNewRoute` abaixo.
const libraryNewRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/library/new',
  component: LibraryNewScreen,
})

const libraryEditRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/library/$id/edit',
  component: LibraryEditScreen,
})

const searchRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/search',
  component: SearchScreen,
})

const reviewRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/review',
  component: ReviewScreen,
})

const orphansRoute = createRoute({
  getParentRoute: () => rootRoute,
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

const entityEditRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/entities/$id/edit',
  component: EntityEditScreen,
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

const settingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/settings',
  component: SettingsScreen,
})

// Administração dos jobs internos (0052) — agendador embutido + histórico.
// Agrupada com `/settings` no `AppHeader` ("Administração"), não solta como
// Revisão/Órfãs.
const jobsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/jobs',
  component: JobsScreen,
})

const healthRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/health',
  component: HealthScreen,
})

const routeTree = rootRoute.addChildren([
  indexRoute,
  libraryRoute,
  libraryNewRoute,
  libraryEditRoute,
  searchRoute,
  reviewRoute,
  orphansRoute,
  itemNewRoute,
  collectionNewRoute,
  entityDetailRoute,
  entityEditRoute,
  externalDetailRoute,
  collectionDetailRoute,
  personDetailRoute,
  settingsRoute,
  jobsRoute,
  healthRoute,
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
