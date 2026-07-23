import { describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AppHeader } from '@/components/app-header'
import { renderWithClient } from '@/test/render'

// Mocka o Link do router (mesmo padrão de colecao-band.test.tsx) — isola o
// teste de navegação real, foco é só o CONTRATO de colapso/menu.
vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock,
}))

// LanguageSelector faz fetch/contexto próprios (cobertos em
// language-selector.test.tsx) — irrelevante para o colapso da nav (0075).
vi.mock('@/components/language-selector', () => ({
  LanguageSelector: () => <div data-testid="seletor-idioma-stub" />,
}))

const TODOS_OS_LINKS = [
  'Biblioteca',
  'Buscar',
  'Bibliotecas',
  'Revisão',
  'Órfãs',
  'Configurações',
  'Jobs',
  'Saúde',
]

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
    const user = userEvent.setup()
    renderWithClient(<AppHeader />)

    await user.click(
      screen.getByRole('button', { name: 'Abrir menu de navegação' }),
    )
    const painel = await screen.findByRole('dialog', { name: 'Navegação' })

    await user.click(within(painel).getByRole('link', { name: 'Saúde' }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: 'Navegação' })).toBeNull()
    })
  })
})
