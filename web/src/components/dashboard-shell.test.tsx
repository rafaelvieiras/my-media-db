import { describe, expect, it } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from '@tanstack/react-router'
import { DashboardShell } from '@/components/dashboard-shell'

// DashboardShell (later extended with the Webhook, Events without an
// item and Integrations sections): sidebar of the administrative
// area (Bibliotecas/Revisão/Órfãs/Jobs/Configurações/Integrações/
// Webhook/Eventos sem item/Saúde) + `<Outlet/>` for the active section. The highlight
// of the active section (`aria-current="page"`) comes from the `activeProps` of the
// `<Link>` of TanStack Router itself (the same mechanism as `AppHeader`) — it is not
// a manual computation as `EntityEditShell` used to do via the
// `secaoAtiva` prop. That is why the test mounts a MINIMAL, isolated route tree
// (rootRoute + dashboardRoute + 9 trivial children), on a
// `memoryHistory` — not the real tree of the app (each of the 8 real screens has its own
// endpoints/MSW, irrelevant here) nor does it mock the `Link` (that would lose the
// `activeProps` behavior, which is exactly what we want to exercise).
const SECOES = [
  'library',
  'review',
  'orphans',
  'jobs',
  'settings',
  'integrations',
  'webhook',
  'historico-orfaos',
  'health',
] as const

async function montarEmRota(pathInicial: string) {
  const rootRoute = createRootRoute({ component: () => <Outlet /> })
  const dashboardRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/dashboard',
    component: DashboardShell,
  })
  const filhas = SECOES.map((secao) =>
    createRoute({
      getParentRoute: () => dashboardRoute,
      path: `/${secao}`,
      component: () => <div>conteúdo de {secao}</div>,
    }),
  )
  const routeTree = rootRoute.addChildren([dashboardRoute.addChildren(filhas)])
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [pathInicial] }),
  })
  await router.load()
  render(<RouterProvider router={router} />)
  return router
}

const ROTULOS = [
  'Bibliotecas',
  'Revisão',
  'Órfãs',
  'Jobs',
  'Configurações',
  'Integrações',
  'Webhook',
  'Eventos sem item',
  'Saúde',
]

describe('DashboardShell', () => {
  it('mostra as 8 seções na sidebar e renderiza o conteúdo da rota ativa', async () => {
    await montarEmRota('/dashboard/review')

    const nav = screen.getByRole('navigation', { name: 'Administração' })
    for (const rotulo of ROTULOS) {
      expect(
        within(nav).getByRole('link', { name: rotulo }),
      ).toBeInTheDocument()
    }
    expect(screen.getByText('conteúdo de review')).toBeInTheDocument()
  })

  it('destaca a seção ativa com aria-current', async () => {
    await montarEmRota('/dashboard/orphans')

    const nav = screen.getByRole('navigation', { name: 'Administração' })
    expect(within(nav).getByRole('link', { name: 'Órfãs' })).toHaveAttribute(
      'aria-current',
      'page',
    )
    expect(
      within(nav).getByRole('link', { name: 'Bibliotecas' }),
    ).not.toHaveAttribute('aria-current')
  })

  it('mobile: o botão de menu tem md:hidden e abre um Sheet com as 8 seções', async () => {
    const user = userEvent.setup()
    await montarEmRota('/dashboard/library')

    const botaoMenu = screen.getByRole('button', {
      name: 'Abrir menu de administração',
    })
    expect(botaoMenu.className).toContain('md:hidden')

    expect(screen.queryByRole('dialog')).toBeNull()

    await user.click(botaoMenu)
    const painel = await screen.findByRole('dialog', { name: 'Administração' })
    for (const rotulo of ROTULOS) {
      expect(
        within(painel).getByRole('link', { name: rotulo }),
      ).toBeInTheDocument()
    }
  })

  it('fecha o Sheet ao navegar por um link do painel mobile', async () => {
    const user = userEvent.setup()
    await montarEmRota('/dashboard/library')

    await user.click(
      screen.getByRole('button', { name: 'Abrir menu de administração' }),
    )
    const painel = await screen.findByRole('dialog', { name: 'Administração' })
    await user.click(within(painel).getByRole('link', { name: 'Jobs' }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: 'Administração' })).toBeNull()
    })
    expect(screen.getByText('conteúdo de jobs')).toBeInTheDocument()
  })
})
