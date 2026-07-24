import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { EntityEditShell } from '@/components/entity-edit-shell'
import { LanguageProvider } from '@/lib/language-context'
import { renderWithClient } from '@/test/render'
import { baseEntity, okJson, settingsPadrao } from '@/test/entity-fixtures'

// EntityEditShell (briefing 0080): o "chrome" compartilhado pelas 3 subrotas de
// edição — cabeçalho (pôster+título+idioma), sidebar de navegação com a seção
// ativa destacada, e o guard de "sair sem salvar" (0073) reaplicado aos 3 links
// da sidebar + "Voltar ao item", agora sobre o "sujo" recebido via prop (só da
// seção atual — cada subrota monta um único bloco).
const spies = vi.hoisted(() => ({ navigate: vi.fn() }))

// Import DINÂMICO do mock do Link (`@/test/router-mock`): a fábrica de
// `vi.mock` roda içada para o topo do arquivo, ANTES dos imports estáticos
// serem resolvidos — referenciar `LinkMock` como import estático aqui causaria
// "Cannot access before initialization". O `await import` adia a resolução
// para quando a fábrica de fato executa.
vi.mock('@tanstack/react-router', async () => {
  const { LinkMock } = await import('@/test/router-mock')
  return {
    useNavigate: () => spies.navigate,
    Link: LinkMock,
  }
})

beforeEach(() => {
  localStorage.clear()
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      const u = new URL(String(url), 'http://localhost')
      if (u.pathname === '/api/v1/settings/languages') {
        return okJson(settingsPadrao)
      }
      throw new Error(`rota nao mockada: ${u.pathname}`)
    }),
  )
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  spies.navigate.mockReset()
})

function renderShell({
  secaoAtiva = 'basico' as const,
  sujo = false,
}: { secaoAtiva?: 'basico' | 'ids' | 'creditos'; sujo?: boolean } = {}) {
  return renderWithClient(
    <LanguageProvider>
      <EntityEditShell
        id="e1"
        entity={baseEntity()}
        secaoAtiva={secaoAtiva}
        sujo={sujo}
      >
        <div>conteúdo da seção</div>
      </EntityEditShell>
    </LanguageProvider>,
  )
}

describe('EntityEditShell', () => {
  it('mostra o título do item e as 3 seções na sidebar', () => {
    renderShell()
    expect(screen.getByText('Dune')).toBeInTheDocument()
    expect(
      screen.getByRole('link', { name: 'Informações básicas' }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('link', { name: 'IDs de terceiros' }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('link', { name: 'Elenco e equipe' }),
    ).toBeInTheDocument()
  })

  it('destaca a seção ativa com aria-current', () => {
    renderShell({ secaoAtiva: 'ids' })
    expect(
      screen.getByRole('link', { name: 'IDs de terceiros' }),
    ).toHaveAttribute('aria-current', 'page')
    expect(
      screen.getByRole('link', { name: 'Informações básicas' }),
    ).not.toHaveAttribute('aria-current')
  })

  it('sem sujeira, navegar para outra seção não confirma', async () => {
    const confirmar = vi.fn()
    vi.stubGlobal('confirm', confirmar)
    const user = userEvent.setup()
    renderShell({ sujo: false })
    await user.click(screen.getByRole('link', { name: 'Elenco e equipe' }))
    expect(confirmar).not.toHaveBeenCalled()
    expect(spies.navigate).not.toHaveBeenCalled()
  })

  it('com sujeira, navegar para outra seção confirma antes de navegar', async () => {
    const confirmar = vi.fn(() => true)
    vi.stubGlobal('confirm', confirmar)
    const user = userEvent.setup()
    renderShell({ sujo: true, secaoAtiva: 'basico' })
    await user.click(screen.getByRole('link', { name: 'Elenco e equipe' }))
    expect(confirmar).toHaveBeenCalledTimes(1)
    expect(spies.navigate).toHaveBeenCalledWith({
      to: '/entities/$id/edit/creditos',
      params: { id: 'e1' },
    })
  })

  it('com sujeira, cancelar a confirmação não navega', async () => {
    vi.stubGlobal(
      'confirm',
      vi.fn(() => false),
    )
    const user = userEvent.setup()
    renderShell({ sujo: true })
    await user.click(screen.getByRole('link', { name: 'IDs de terceiros' }))
    expect(spies.navigate).not.toHaveBeenCalled()
  })

  it('com sujeira, "Voltar ao item" confirma antes de navegar', async () => {
    vi.stubGlobal(
      'confirm',
      vi.fn(() => true),
    )
    const user = userEvent.setup()
    renderShell({ sujo: true })
    await user.click(screen.getByRole('link', { name: /Voltar ao item/ }))
    expect(spies.navigate).toHaveBeenCalledWith({
      to: '/entities/$id',
      params: { id: 'e1' },
    })
  })

  it('sem sujeira, o beforeunload não é interceptado', () => {
    renderShell({ sujo: false })
    const evento = new Event('beforeunload', { cancelable: true })
    const preventDefault = vi.spyOn(evento, 'preventDefault')
    window.dispatchEvent(evento)
    expect(preventDefault).not.toHaveBeenCalled()
  })

  it('com sujeira, dispara o aviso nativo (beforeunload)', () => {
    renderShell({ sujo: true })
    const evento = new Event('beforeunload', { cancelable: true })
    const preventDefault = vi.spyOn(evento, 'preventDefault')
    window.dispatchEvent(evento)
    expect(preventDefault).toHaveBeenCalled()
  })
})
