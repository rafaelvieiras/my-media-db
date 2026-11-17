import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { EntityEditShell } from '@/components/entity-edit-shell'
import { LanguageProvider } from '@/lib/language-context'
import { renderWithClient } from '@/test/render'
import { baseEntity, okJson, settingsPadrao } from '@/test/entity-fixtures'

// EntityEditShell: the "chrome" shared by the 3 edit subroutes —
// header (poster+title+language), navigation sidebar with the active section
// highlighted, and the "leave without saving" guard reapplied to the 3 links
// of the sidebar + "Back to the item", now over the "dirty" state received via prop (only from the
// current section — each subroute mounts a single block).
const spies = vi.hoisted(() => ({ navigate: vi.fn() }))

// DYNAMIC import of the Link mock (`@/test/router-mock`): the factory of
// `vi.mock` runs hoisted to the top of the file, BEFORE the static imports
// are resolved — referencing `LinkMock` as a static import here would cause
// "Cannot access before initialization". The `await import` postpones the resolution
// until the factory actually executes.
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
        <div>section content</div>
      </EntityEditShell>
    </LanguageProvider>,
  )
}

describe('EntityEditShell', () => {
  it('shows the item title and the 3 sections in the sidebar', () => {
    renderShell()
    expect(screen.getByText('Dune')).toBeInTheDocument()
    expect(
      screen.getByRole('link', { name: 'Basic information' }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('link', { name: 'External IDs' }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('link', { name: 'Cast and crew' }),
    ).toBeInTheDocument()
  })

  it('highlights the active section with aria-current', () => {
    renderShell({ secaoAtiva: 'ids' })
    expect(screen.getByRole('link', { name: 'External IDs' })).toHaveAttribute(
      'aria-current',
      'page',
    )
    expect(
      screen.getByRole('link', { name: 'Basic information' }),
    ).not.toHaveAttribute('aria-current')
  })

  it('without changes, navigating to another section does not confirm', async () => {
    const confirmar = vi.fn()
    vi.stubGlobal('confirm', confirmar)
    const user = userEvent.setup()
    renderShell({ sujo: false })
    await user.click(screen.getByRole('link', { name: 'Cast and crew' }))
    expect(confirmar).not.toHaveBeenCalled()
    expect(spies.navigate).not.toHaveBeenCalled()
  })

  it('with changes, navigating to another section confirms before navigating', async () => {
    const confirmar = vi.fn(() => true)
    vi.stubGlobal('confirm', confirmar)
    const user = userEvent.setup()
    renderShell({ sujo: true, secaoAtiva: 'basico' })
    await user.click(screen.getByRole('link', { name: 'Cast and crew' }))
    expect(confirmar).toHaveBeenCalledTimes(1)
    expect(spies.navigate).toHaveBeenCalledWith({
      to: '/entities/$id/edit/creditos',
      params: { id: 'e1' },
    })
  })

  it('with changes, cancelling the confirmation does not navigate', async () => {
    vi.stubGlobal(
      'confirm',
      vi.fn(() => false),
    )
    const user = userEvent.setup()
    renderShell({ sujo: true })
    await user.click(screen.getByRole('link', { name: 'External IDs' }))
    expect(spies.navigate).not.toHaveBeenCalled()
  })

  it('with changes, "Back to the item" confirms before navigating', async () => {
    vi.stubGlobal(
      'confirm',
      vi.fn(() => true),
    )
    const user = userEvent.setup()
    renderShell({ sujo: true })
    await user.click(screen.getByRole('link', { name: /Back to the item/ }))
    expect(spies.navigate).toHaveBeenCalledWith({
      to: '/entities/$id',
      params: { id: 'e1' },
    })
  })

  it('without changes, beforeunload is not intercepted', () => {
    renderShell({ sujo: false })
    const evento = new Event('beforeunload', { cancelable: true })
    const preventDefault = vi.spyOn(evento, 'preventDefault')
    window.dispatchEvent(evento)
    expect(preventDefault).not.toHaveBeenCalled()
  })

  it('with changes, it fires the native warning (beforeunload)', () => {
    renderShell({ sujo: true })
    const evento = new Event('beforeunload', { cancelable: true })
    const preventDefault = vi.spyOn(evento, 'preventDefault')
    window.dispatchEvent(evento)
    expect(preventDefault).toHaveBeenCalled()
  })
})
