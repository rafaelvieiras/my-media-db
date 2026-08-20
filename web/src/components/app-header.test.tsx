import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AppHeader } from '@/components/app-header'
import type { EntityType } from '@/lib/api'
import { renderWithClient } from '@/test/render'

// Mocka o Link do router (mesmo padrão de colecao-band.test.tsx) — isola o
// teste de navegação real, foco é só o CONTRATO de colapso/menu. `LinkMock`
// (0110) também resolve `search` em query string, usado pelos testes do
// dropdown "Biblioteca" abaixo.
vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock,
}))

// LanguageSelector faz fetch/contexto próprios (cobertos em
// language-selector.test.tsx) — irrelevante para o colapso da nav (0075).
vi.mock('@/components/language-selector', () => ({
  LanguageSelector: () => <div data-testid="seletor-idioma-stub" />,
}))

const TODOS_OS_LINKS = ['Biblioteca', 'Buscar', 'Dashboard']

function jsonResponse(body: unknown): Response {
  return { ok: true, status: 200, json: async () => body } as Response
}

// Todo teste que renderiza AppHeader dispara `useQuery(entityTypesQueryOptions)`
// (dropdown "Biblioteca", 0110) — stub padrão do fetch para os testes de
// colapso mobile (0075) que não se importam com o conteúdo do dropdown, só com
// não estourar (MSW recusa request não mockada — ver test/setup.ts).
function stubEntityTypes(tipos: EntityType[] = []) {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse(tipos)))
}

// abrirMenuMobile stuba os tipos, renderiza o AppHeader e abre o Sheet mobile —
// o preâmbulo comum aos testes que interagem DENTRO do painel (jscpd).
async function abrirMenuMobile(tipos: EntityType[] = []) {
  stubEntityTypes(tipos)
  const user = userEvent.setup()
  renderWithClient(<AppHeader />)
  await user.click(
    screen.getByRole('button', { name: 'Abrir menu de navegação' }),
  )
  const painel = await screen.findByRole('dialog', { name: 'Navegação' })
  return { user, painel }
}

afterEach(() => {
  vi.restoreAllMocks()
})

// 0075: abaixo de ~768px a nav não pode mais transbordar a página (o achado da
// auditoria: `body.scrollWidth` de 1043px contra 390px de viewport). jsdom não
// aplica media query nenhuma (não há motor de layout) — o "sem overflow
// horizontal" em si só é verificável visualmente/num navegador real. Aqui
// travamos o CONTRATO que sustenta a correção: o botão de menu existe e some
// no desktop (`md:hidden`), e NENHUMA rota antes visível deixa de ser
// alcançável quando colapsada atrás dele — inclusive para leitor de tela: ao
// abrir o Sheet, o Radix marca a cópia desktop (fora do painel) como
// `aria-hidden`, e é exatamente essa cópia acessível dentro do painel que os
// testes abaixo verificam.
describe('AppHeader — colapso mobile (0075)', () => {
  it('desktop: todos os links continuam na nav, e o botão de menu tem md:hidden', () => {
    stubEntityTypes()
    renderWithClient(<AppHeader />)

    const nav = screen.getByRole('navigation')
    for (const nome of TODOS_OS_LINKS) {
      expect(within(nav).getByRole('link', { name: nome })).toBeInTheDocument()
    }

    const botaoMenu = screen.getByRole('button', {
      name: 'Abrir menu de navegação',
    })
    expect(botaoMenu.className).toContain('md:hidden')
  })

  it('mobile: o painel do menu só monta ao abrir e disponibiliza todos os links', async () => {
    stubEntityTypes()
    const user = userEvent.setup()
    renderWithClient(<AppHeader />)

    // Fechado, o Sheet nem monta (Radix Portal) — só existe a cópia desktop.
    expect(screen.getAllByRole('link', { name: 'Biblioteca' })).toHaveLength(1)
    expect(screen.queryByRole('heading', { name: 'Navegação' })).toBeNull()

    await user.click(
      screen.getByRole('button', { name: 'Abrir menu de navegação' }),
    )

    const painel = await screen.findByRole('dialog', { name: 'Navegação' })

    // Nenhuma rota do conjunto original some do alcance do usuário no menu —
    // consultado DENTRO do painel (a cópia desktop, agora `aria-hidden` pelo
    // Radix por estar fora do Sheet aberto, não entra nessa contagem).
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
      expect(screen.queryByRole('dialog', { name: 'Navegação' })).toBeNull()
    })
  })
})

// Dropdown "Biblioteca" (briefing 0110, atualizado no 0114/ADR-0030): lista os
// schemas com dado catalogado (mesmo endpoint/hook do carrossel da home,
// 0109) e navega direto pra `/library/$entitySlug` do tipo — sem perder o
// link direto pra biblioteca SEM filtro (o texto "Biblioteca" continua um
// `NavLink` de sempre, só ganhou um trigger separado ao lado, o chevron). O
// trigger não depende de hover (Radix abre por clique/teclado), então o mesmo
// teste vale pro desktop e, via `within(painel)`, pro Sheet mobile — cobrindo
// o critério de aceite "funciona por toque". `TIPOS` usa só tipos DE TOPO
// mapeados em `library-slugs.ts` (movie/artista) — `GET /entities/types` já
// não devolve sub-nó (album/faixa) desde o 0112.
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
    const link = within(nav).getByRole('link', { name: 'Biblioteca' })
    expect(link).toHaveAttribute('href', '/')
  })

  it('lista os tipos devolvidos por GET /entities/types, rotulados no plural', async () => {
    stubEntityTypes(TIPOS)
    const user = userEvent.setup()
    renderWithClient(<AppHeader />)

    await user.click(
      screen.getByRole('button', { name: 'Filtrar biblioteca por tipo' }),
    )

    expect(
      await screen.findByRole('menuitem', { name: 'Filmes' }),
    ).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: 'Séries' })).toBeInTheDocument()
    expect(
      screen.getByRole('menuitem', { name: 'Artistas' }),
    ).toBeInTheDocument()
  })

  it('clicar num item do dropdown navega pra "/library/:entitySlug" do schema escolhido', async () => {
    stubEntityTypes(TIPOS)
    const user = userEvent.setup()
    renderWithClient(<AppHeader />)

    await user.click(
      screen.getByRole('button', { name: 'Filtrar biblioteca por tipo' }),
    )
    const item = await screen.findByRole('menuitem', { name: 'Artistas' })
    expect(item).toHaveAttribute('href', '/library/artists')
  })

  it('um tipo sem slug mapeado (defensivo) não aparece no dropdown', async () => {
    stubEntityTypes([...TIPOS, { type: 'album', count: 3 }])
    const user = userEvent.setup()
    renderWithClient(<AppHeader />)

    await user.click(
      screen.getByRole('button', { name: 'Filtrar biblioteca por tipo' }),
    )
    await screen.findByRole('menuitem', { name: 'Filmes' })
    expect(screen.queryByRole('menuitem', { name: 'Álbuns' })).toBeNull()
  })

  it('mobile: o dropdown funciona por toque dentro do Sheet', async () => {
    const { user, painel } = await abrirMenuMobile(TIPOS)

    await user.click(
      within(painel).getByRole('button', {
        name: 'Filtrar biblioteca por tipo',
      }),
    )

    const item = await screen.findByRole('menuitem', { name: 'Séries' })
    expect(item).toHaveAttribute('href', '/library/series')
  })
})
