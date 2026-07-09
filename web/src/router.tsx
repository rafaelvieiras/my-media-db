import {
  Outlet,
  createRootRouteWithContext,
  createRoute,
  createRouter,
} from '@tanstack/react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { HealthScreen } from '@/routes/index'

// Contexto do router: carrega o QueryClient compartilhado, de modo que loaders
// de rota (Fase 2) possam pré-carregar/hidratar o cache do TanStack Query.
export interface RouterContext {
  queryClient: QueryClient
}

// QueryClient único, compartilhado entre o contexto do router e o provider.
export const queryClient = new QueryClient()

// Roteamento CODE-BASED (uma rota só nesta fase; o routeTree.gen file-based
// entra na Fase 2, quando houver várias telas).
const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: () => <Outlet />,
})

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  component: HealthScreen,
})

const routeTree = rootRoute.addChildren([indexRoute])

export const router = createRouter({
  routeTree,
  context: { queryClient },
  // Wrap envolve toda a árvore do router com o provider do TanStack Query,
  // usando o mesmo client que está no contexto.
  Wrap: ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  ),
})

// Registro de tipos: dá type-safety global ao router (navegação/params tipados).
declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
