import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AppHeader } from '@/components/app-header'
import type { EntityType } from '@/lib/api'
import { renderWithClient } from '@/test/render'

// Mocks the Link of the router (same pattern as colecao-band.test.tsx) — it isolates the
// test from real navigation, the focus is only the CONTRACT of collapse/menu. `LinkMock`
// also resolves `search` into a query string, used by the
// "Biblioteca" dropdown tests below.
vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock,
  // AppHeader hides its own menu inside /dashboard (the DashboardShell owns the mobile
  // navigation there), so the mock has to answer with a path.
  useLocation: () => ({ pathname: estadoRota.pathname }),
}))

// AppHeader reads the current path to decide whether the DashboardShell owns the mobile
// navigation (it does inside /dashboard). Tests flip this instead of re-mocking.
const estadoRota = { pathname: '/' }

// LanguageSelector does its own fetch/context (covered in
// language-selector.test.tsx) — irrelevant for the nav collapse.
vi.mock('@/components/language-selector', () => ({
  LanguageSelector: () => <div data-testid="seletor-idioma-stub" />,
}))

const TODOS_OS_LINKS = ['Library', 'Search', 'Dashboard']

function jsonResponse(body: unknown): Response {
  return { ok: true, status: 200, json: async () => body } as Response
}

// Every test that renders AppHeader fires `useQuery(entityTypesQueryOptions)`
// ("Biblioteca" dropdown) — a standard fetch stub for the mobile
// collapse tests that do not care about the content of the dropdown, only about
// not blowing up (MSW refuses an unmocked request — see test/setup.ts).
function stubEntityTypes(tipos: EntityType[] = []) {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse(tipos)))
}

// abrirMenuMobile stubs the types, renders the AppHeader and opens the mobile Sheet —
// the common preamble of the tests that interact INSIDE the panel (jscpd).
async function abrirMenuMobile(tipos: EntityType[] = []) {
  stubEntityTypes(tipos)
  const user = userEvent.setup()
  renderWithClient(<AppHeader />)
  await user.click(screen.getByRole('button', { name: 'Open navigation menu' }))
  const painel = await screen.findByRole('dialog', { name: 'Navigation' })
  return { user, painel }
}

afterEach(() => {
  vi.restoreAllMocks()
})

// Below ~768px the nav can no longer overflow the page (the finding of the
// audit: `body.scrollWidth` of 1043px against a 390px viewport). jsdom does not
// apply any media query (there is no layout engine) — "no horizontal
// overflow" itself is only verifiable visually/in a real browser. Here
// we lock the CONTRACT that supports the fix: the menu button exists and disappears
// on desktop (`md:hidden`), and NO previously visible route stops being
// reachable when collapsed behind it — including for a screen reader: when
// the Sheet opens, Radix marks the desktop copy (outside the panel) as
// `aria-hidden`, and it is exactly that accessible copy inside the panel that the
// tests below check.
describe('AppHeader — colapso mobile (0075)', () => {
  it('desktop: todos os links continuam na nav, e o botão de menu tem md:hidden', () => {
    stubEntityTypes()
    renderWithClient(<AppHeader />)

    const nav = screen.getByRole('navigation')
    for (const nome of TODOS_OS_LINKS) {
      expect(within(nav).getByRole('link', { name: nome })).toBeInTheDocument()
    }

    const botaoMenu = screen.getByRole('button', {
      name: 'Open navigation menu',
    })
    expect(botaoMenu.className).toContain('md:hidden')
  })

  it('mobile: o painel do menu só monta ao abrir e disponibiliza todos os links', async () => {
    stubEntityTypes()
    const user = userEvent.setup()
    renderWithClient(<AppHeader />)

    // Closed, the Sheet does not even mount (Radix Portal) — only the desktop copy exists.
    expect(screen.getAllByRole('link', { name: 'Library' })).toHaveLength(1)
    expect(screen.queryByRole('heading', { name: 'Navigation' })).toBeNull()

    await user.click(
      screen.getByRole('button', { name: 'Open navigation menu' }),
    )

    const painel = await screen.findByRole('dialog', { name: 'Navigation' })

    // No route of the original set leaves the user's reach in the menu —
    // queried INSIDE the panel (the desktop copy, now `aria-hidden` by
    // Radix for being outside the open Sheet, does not count here).
    for (const nome of TODOS_OS_LINKS) {
      expect(
        within(painel).getByRole('link', { name: nome }),
      ).toBeInTheDocument()
    }
  })

  it('fecha o menu ao navegar por um link do painel ou pelo botão de fechar', async () => {
    const { user, painel } = await abrirMenuMobile()

    await user.click(within(painel).getByRole('link', { name: 'Dashboard' }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: 'Navigation' })).toBeNull()
    })
  })
})

// "Biblioteca" dropdown: it lists the
// schemas with cataloged data (same endpoint/hook as the home carousel)
// and navigates straight to `/library/$entitySlug` of the type — without losing the
// direct link to the library WITHOUT a filter (the text "Biblioteca" stays a
// usual `NavLink`, it only gained a separate trigger next to it, the chevron). The
// trigger does not depend on hover (Radix opens by click/keyboard), so the same
// test is valid for desktop and, via `within(painel)`, for the mobile Sheet — covering
// the acceptance criterion "works by touch". `TIPOS` uses only TOP-LEVEL types
// mapped in `library-slugs.ts` (movie/artista) — `GET /entities/types` no longer
// returns a sub-node (album/faixa).
describe('AppHeader — dropdown "Biblioteca" (0110/0114)', () => {
  const TIPOS: EntityType[] = [
    { type: 'movie', count: 21 },
    { type: 'tv', count: 5 },
    { type: 'artista', count: 2 },
  ]

  it('o texto "Biblioteca" continua um link direto pra "/", sem filtro', () => {
    stubEntityTypes(TIPOS)
    renderWithClient(<AppHeader />)

    const nav = screen.getByRole('navigation')
    const link = within(nav).getByRole('link', { name: 'Library' })
    expect(link).toHaveAttribute('href', '/')
  })

  it('lista os tipos devolvidos por GET /entities/types, rotulados no plural', async () => {
    stubEntityTypes(TIPOS)
    const user = userEvent.setup()
    renderWithClient(<AppHeader />)

    await user.click(
      screen.getByRole('button', { name: 'Filter the library by type' }),
    )

    expect(
      await screen.findByRole('menuitem', { name: 'Movies' }),
    ).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: 'Series' })).toBeInTheDocument()
    expect(
      screen.getByRole('menuitem', { name: 'Artists' }),
    ).toBeInTheDocument()
  })

  it('clicar num item do dropdown navega pra "/library/:entitySlug" do schema escolhido', async () => {
    stubEntityTypes(TIPOS)
    const user = userEvent.setup()
    renderWithClient(<AppHeader />)

    await user.click(
      screen.getByRole('button', { name: 'Filter the library by type' }),
    )
    const item = await screen.findByRole('menuitem', { name: 'Artists' })
    expect(item).toHaveAttribute('href', '/library/artists')
  })

  it('um tipo sem slug mapeado (defensivo) não aparece no dropdown', async () => {
    stubEntityTypes([...TIPOS, { type: 'album', count: 3 }])
    const user = userEvent.setup()
    renderWithClient(<AppHeader />)

    await user.click(
      screen.getByRole('button', { name: 'Filter the library by type' }),
    )
    await screen.findByRole('menuitem', { name: 'Movies' })
    expect(screen.queryByRole('menuitem', { name: 'Albums' })).toBeNull()
  })

  it('mobile: dentro do /dashboard o menu do header sai de cena (o shell do admin navega)', () => {
    stubEntityTypes([])
    estadoRota.pathname = '/dashboard/queue'
    renderWithClient(<AppHeader />)

    const gatilho = screen.getByRole('button', {
      name: 'Open navigation menu',
    })
    expect(gatilho.className).toContain('hidden')

    estadoRota.pathname = '/'
  })

  it('mobile: o dropdown funciona por toque dentro do Sheet', async () => {
    const { user, painel } = await abrirMenuMobile(TIPOS)

    await user.click(
      within(painel).getByRole('button', {
        name: 'Filter the library by type',
      }),
    )

    const item = await screen.findByRole('menuitem', { name: 'Series' })
    expect(item).toHaveAttribute('href', '/library/series')
  })
})
