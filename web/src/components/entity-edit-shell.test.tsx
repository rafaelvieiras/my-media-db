import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { EntityEditShell } from '@/components/entity-edit-shell'
import { LanguageProvider } from '@/lib/language-context'
import { useUnsavedGuard } from '@/lib/use-unsaved-guard'
import { renderWithClient } from '@/test/render'
import { baseEntity, okJson, settingsPadrao } from '@/test/entity-fixtures'

// The "leave without saving" guard is the router blocker: outside a
// RouterProvider it cannot run, so it is mocked here (the hook has its own test and
// each screen asserts the argument it passes).
vi.mock('@/lib/use-unsaved-guard', () => ({ useUnsavedGuard: vi.fn() }))

// DYNAMIC import of the Link mock (`@/test/router-mock`): the factory of
// `vi.mock` runs hoisted to the top of the file, BEFORE the static imports
// are resolved — referencing `LinkMock` as a static import here would cause
// "Cannot access before initialization". The `await import` postpones the resolution
// until the factory actually executes.
vi.mock('@tanstack/react-router', async () => {
  const { LinkMock } = await import('@/test/router-mock')
  return { Link: LinkMock }
})

// EntityEditShell: the "chrome" shared by the 3 edit subroutes —
// header (poster+title+language), navigation sidebar with the active section
// highlighted and the tabs WRAPPING on mobile (no clipped third tab). The
// "leave without saving" guard lives in `lib/use-unsaved-guard` (the router
// blocker) and only receives the dirty state from here.

beforeEach(() => {
  localStorage.clear()
  vi.mocked(useUnsavedGuard).mockClear()
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

  it('wraps the section tabs instead of scrolling them out of view', () => {
    renderShell()
    const nav = screen.getByRole('navigation', { name: 'Edit sections' })
    expect(nav.className).toContain('flex-wrap')
    expect(nav.className).not.toContain('overflow-x-auto')
  })

  it('hands a pristine section to the guard (no confirmation to ask)', () => {
    renderShell({ sujo: false })
    expect(useUnsavedGuard).toHaveBeenLastCalledWith(false)
  })

  it('hands a dirty section to the guard (leave asks for confirmation)', async () => {
    const user = userEvent.setup()
    renderShell({ sujo: true })
    expect(useUnsavedGuard).toHaveBeenLastCalledWith(true)
    // The links are plain navigation now: the router blocker intercepts them, so
    // the shell does not prevent the click by itself.
    const link = screen.getByRole('link', { name: 'Cast and crew' })
    const evento = new MouseEvent('click', { bubbles: true, cancelable: true })
    link.dispatchEvent(evento)
    expect(evento.defaultPrevented).toBe(false)
    await user.click(link)
  })
})
