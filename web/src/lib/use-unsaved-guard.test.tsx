import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { UseBlockerOpts } from '@tanstack/react-router'
import { useBlocker } from '@tanstack/react-router'
import { useUnsavedGuard } from '@/lib/use-unsaved-guard'

// The guard is a thin adapter over the router blocker plus the product's own
// confirmation dialog. The test pins the contract the routes rely on: the blocker is only
// armed while the form is dirty, `beforeunload` follows the same state, and the dialog's two
// buttons resolve the blocked navigation (proceed / reset) instead of `window.confirm`.
vi.mock('@tanstack/react-router', () => ({ useBlocker: vi.fn() }))
vi.mock('@/lib/i18n', () => ({
  useT: () => (chave: string) => chave,
}))

const blocker = {
  status: 'blocked' as 'blocked' | 'idle' | 'proceeding',
  proceed: vi.fn(),
  reset: vi.fn(),
}

function Alvo({ sujo }: { sujo: boolean }) {
  return <>{useUnsavedGuard(sujo)}</>
}

function ultimasOpcoes(): UseBlockerOpts {
  const chamadas = vi.mocked(useBlocker).mock.calls as unknown as [
    UseBlockerOpts,
  ][]
  const ultima = chamadas[chamadas.length - 1]
  expect(ultima).toBeDefined()
  return ultima![0]
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.mocked(useBlocker).mockClear()
  blocker.proceed.mockClear()
  blocker.reset.mockClear()
})

describe('useUnsavedGuard', () => {
  it('is disabled while the form is pristine', () => {
    // With the blocker disabled the router never reports `blocked`, so the dialog cannot show.
    vi.mocked(useBlocker).mockReturnValue({
      ...blocker,
      status: 'idle',
    } as never)
    render(<Alvo sujo={false} />)

    expect(ultimasOpcoes().disabled).toBe(true)
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  })

  it('asks the router to resolve the block, not the browser', () => {
    vi.mocked(useBlocker).mockReturnValue(blocker as never)
    render(<Alvo sujo />)

    const opcoes = ultimasOpcoes()
    expect(opcoes.disabled).toBe(false)
    expect(opcoes.withResolver).toBe(true)
    expect(opcoes.shouldBlockFn({} as never)).toBe(true)
  })

  it('enables the native beforeunload warning only with a dirty form', () => {
    vi.mocked(useBlocker).mockReturnValue(blocker as never)
    const { rerender } = render(<Alvo sujo={false} />)

    const enabled = ultimasOpcoes().enableBeforeUnload
    expect(typeof enabled).toBe('function')
    expect((enabled as () => boolean)()).toBe(false)

    rerender(<Alvo sujo />)
    const enabledDirty = ultimasOpcoes().enableBeforeUnload
    expect((enabledDirty as () => boolean)()).toBe(true)
  })

  it('renders the product dialog while the navigation is blocked', () => {
    vi.mocked(useBlocker).mockReturnValue(blocker as never)
    render(<Alvo sujo />)

    expect(screen.getByRole('alertdialog')).toBeVisible()
    expect(
      screen.getByText('common.message.unsavedChanges'),
    ).toBeInTheDocument()
  })

  it('cancelling keeps the owner on the form (reset)', async () => {
    vi.mocked(useBlocker).mockReturnValue(blocker as never)
    render(<Alvo sujo />)

    await userEvent.click(
      screen.getByRole('button', { name: 'common.action.cancel' }),
    )

    expect(blocker.reset).toHaveBeenCalled()
    expect(blocker.proceed).not.toHaveBeenCalled()
  })

  it('leaving without saving resolves the blocked navigation (proceed)', async () => {
    vi.mocked(useBlocker).mockReturnValue(blocker as never)
    render(<Alvo sujo />)

    await userEvent.click(
      screen.getByRole('button', { name: 'common.message.leaveWithoutSaving' }),
    )

    expect(blocker.proceed).toHaveBeenCalled()
  })

  it('renders nothing while the router has no pending navigation', () => {
    vi.mocked(useBlocker).mockReturnValue({
      ...blocker,
      status: 'idle',
    } as never)
    render(<Alvo sujo />)

    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  })
})
