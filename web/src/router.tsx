import {
  Outlet,
  createRootRouteWithContext,
  createRoute,
  createRouter,
  redirect,
} from '@tanstack/react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { I18nProvider } from '@/lib/i18n'
import { z } from 'zod'
import { AppHeader } from '@/components/app-header'
import { DashboardShell } from '@/components/dashboard-shell'
import { LanguageProvider } from '@/lib/language-context'
import { LibraryItemsScreen } from '@/routes/index'
import { LibraryScreen } from '@/routes/library'
import { LibraryBrowseScreen } from '@/routes/library-browse'
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
import { IntegrationsListScreen } from '@/routes/integrations'
import { IntegrationDetailScreen } from '@/routes/integration-detail'
import { WebhookScreen } from '@/routes/webhook'
import { HistoricoOrfaosScreen } from '@/routes/historico-orfaos'
import { JobsScreen } from '@/routes/jobs'
import { QueueScreen } from '@/routes/queue'
import { HealthScreen } from '@/routes/health'

// Router context: it carries the shared QueryClient, so that route
// loaders can preload/hydrate the TanStack Query cache.
export interface RouterContext {
  queryClient: QueryClient
}

// Single QueryClient, shared between the router context and the provider.
export const queryClient = new QueryClient()

// CODE-BASED routing. The root is the layout: navigation header + <Outlet>.
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

// Route `/library/$entitySlug` (ADR-0030): the complete
// paginated library of one top-level type at a time, reached through the "Ver Todos" of each
// carousel of the home and through the "Biblioteca" dropdown of the navbar. It replaces the
// search param `?type=` of the `/` route (removed — it was not a
// documented public contract) with a friendly, dedicated URL; the slug is
// resolved to `entity.type` by `lib/library-slugs.ts`.
const libraryBrowseRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/library/$entitySlug',
  component: LibraryBrowseScreen,
})

// Administrative area: the 6 operation/config screens become
// subroutes of `/dashboard`, navigated through the sidebar of the `DashboardShell` — instead
// of loose in the `AppHeader`. `dashboardIndexRoute` redirects `/dashboard` to
// the first section (Bibliotecas).
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

// Dedicated datasource routes: creating path/type and re-pointing
// path/removing move out of the fixed form + inline editing of the list into pages
// of their own — the same pattern as `entityEditBasicoRoute`/`itemNewRoute` below.
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

// Dedicated CRUD routes: creating item/collection and editing an item move out
// of the inline forms into pages of their own (deep-link + testability).
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

// Search param `ordering` of the series detail: the display order of
// episodes chosen in the selector lives in the URL (shareable, ephemeral — without
// persistence). Optional; absent = default order of the series (seasons view).
const entityDetailSearchSchema = z.object({
  ordering: z.string().optional(),
})

const entityDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/entities/$id',
  component: EntityDetailScreen,
  validateSearch: entityDetailSearchSchema,
})

// Item edit, split into subroutes in the TMDB sidebar pattern:
// `/entities/$id/edit` is still the default URL —
// now the "Informações básicas" section —, with third-party IDs and Cast/crew
// in subroutes of their own (deep-linkable), all FLAT (same pattern as the other
// routes of this file — not a nested layout route).
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

// Rich detail of the external item BEFORE importing. Params: source, type
// (`movie|tv`) and id IN THE SOURCE. Similars/collection navigate here recursively.
const externalDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/external/$source/$type/$id',
  component: ExternalDetailScreen,
})

// External collection as a page of its own. Params: source and id
// of the collection IN THE SOURCE. Reached through the collection band of the rich detail.
const collectionDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/collection/$source/$id',
  component: CollectionDetailScreen,
})

// MINIMAL person page (phase D, internal-first). Reached through the
// cast/crew card of the RichDetailView when the credit carries a `person_id` (the person has already been
// persisted by some import).
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

// Integrations configurable through the UI/database (ADR-0023): list and
// form per schema for provider credentials (TMDB first).
const dashboardIntegrationsRoute = createRoute({
  getParentRoute: () => dashboardRoute,
  path: '/integrations',
  component: IntegrationsListScreen,
})

const dashboardIntegrationDetailRoute = createRoute({
  getParentRoute: () => dashboardRoute,
  path: '/integrations/$slug',
  component: IntegrationDetailScreen,
})

// Jellyfin inbound webhook: generate/copy the webhook token.
const dashboardWebhookRoute = createRoute({
  getParentRoute: () => dashboardRoute,
  path: '/webhook',
  component: WebhookScreen,
})

// GLOBAL list of history events without a related item:
// read-only, complementary to the per-entity timeline.
const dashboardHistoricoOrfaosRoute = createRoute({
  getParentRoute: () => dashboardRoute,
  path: '/historico-orfaos',
  component: HistoricoOrfaosScreen,
})

// Administration of the internal jobs — embedded scheduler + history.
const dashboardJobsRoute = createRoute({
  getParentRoute: () => dashboardRoute,
  path: '/jobs',
  component: JobsScreen,
})

// The queue (ADR-0040) sits next to the jobs because it shows the SAME executions
// that the jobs screen fires — plus the scans, which have no screen of their own.
const dashboardQueueRoute = createRoute({
  getParentRoute: () => dashboardRoute,
  path: '/queue',
  component: QueueScreen,
})

const dashboardHealthRoute = createRoute({
  getParentRoute: () => dashboardRoute,
  path: '/health',
  component: HealthScreen,
})

const routeTree = rootRoute.addChildren([
  indexRoute,
  libraryBrowseRoute,
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
    dashboardIntegrationsRoute,
    dashboardIntegrationDetailRoute,
    dashboardWebhookRoute,
    dashboardHistoricoOrfaosRoute,
    dashboardJobsRoute,
    dashboardQueueRoute,
    dashboardHealthRoute,
  ]),
])

export const router = createRouter({
  routeTree,
  context: { queryClient },
  // Wrap wraps the whole router tree with the TanStack Query provider,
  // using the same client that is in the context.
  Wrap: ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={queryClient}>
      <I18nProvider>
        <LanguageProvider>{children}</LanguageProvider>
      </I18nProvider>
    </QueryClientProvider>
  ),
})

// Type registration: it gives global type-safety to the router (typed navigation/params).
declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
