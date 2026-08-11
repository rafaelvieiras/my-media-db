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

// DashboardShell (briefing 0083, seção Webhook somada no 0097): sidebar da
// área administrativa (Bibliotecas/Revisão/Órfãs/Jobs/Configurações/Webhook/
// Saúde) + `<Outlet/>` para a seção ativa. O destaque da seção ativa
// (`aria-current="page"`) vem do PRÓPRIO `activeProps`
// do `<Link>` do TanStack Router (0075: mesmo mecanismo do `AppHeader`) — não é
// um cálculo manual como o `EntityEditShell` (0080) fazia via prop `secaoAtiva`.
// Por isso o teste monta uma árvore de rota MÍNIMA e isolada (rootRoute +
// dashboardRoute + 7 filhas triviais), num `memoryHistory` — não a árvore real
// do app (cada uma das 7 telas reais tem endpoints/MSW próprios, irrelevantes
// aqui) nem mocka o `Link` (perderia o comportamento de `activeProps` que é
// exatamente o que queremos exercitar).
const SECOES = [
  'library',
  'review',
  'orphans',
  'jobs',
  'settings',
  'webhook',
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
  'Webhook',
  'Saúde',
]

describe('DashboardShell', () => {
  it('mostra as 7 seções na sidebar e renderiza o conteúdo da rota ativa', async () => {
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

  it('mobile: o botão de menu tem md:hidden e abre um Sheet com as 7 seções', async () => {
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
