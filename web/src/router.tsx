import {
  Outlet,
  createRootRouteWithContext,
  createRoute,
  createRouter,
} from '@tanstack/react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AppHeader } from '@/components/app-header'
import { LanguageProvider } from '@/lib/language-context'
import { LibraryItemsScreen } from '@/routes/index'
import { LibraryScreen } from '@/routes/library'
import { SearchScreen } from '@/routes/search'
import { EntityDetailScreen } from '@/routes/entity-detail'
import { SettingsScreen } from '@/routes/settings'
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

const searchRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/search',
  component: SearchScreen,
})

const entityDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/entities/$id',
  component: EntityDetailScreen,
})

const settingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/settings',
  component: SettingsScreen,
})

const healthRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/health',
  component: HealthScreen,
})

const routeTree = rootRoute.addChildren([
  indexRoute,
  libraryRoute,
  searchRoute,
  entityDetailRoute,
  settingsRoute,
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
