import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderHook } from '@testing-library/react'
import type { UseBlockerOpts } from '@tanstack/react-router'
import { useBlocker } from '@tanstack/react-router'
import { useUnsavedGuard } from '@/lib/use-unsaved-guard'

// The guard is a thin adapter over the router blocker: the test pins the contract
// the routes rely on (the confirm answer is inverted, beforeunload follows the dirty
// state and a pristine form registers nothing).
vi.mock('@tanstack/react-router', () => ({ useBlocker: vi.fn() }))
vi.mock('@/lib/i18n', () => ({
  useT: () => (chave: string) => chave,
}))

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
})

describe('useUnsavedGuard', () => {
  it('is disabled while the form is pristine', () => {
    renderHook(() => useUnsavedGuard(false))
    expect(ultimasOpcoes().disabled).toBe(true)
  })

  it('is enabled while the form is dirty', () => {
    renderHook(() => useUnsavedGuard(true))
    expect(ultimasOpcoes().disabled).toBe(false)
  })

  it('blocks the navigation when the owner cancels the confirm', () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    renderHook(() => useUnsavedGuard(true))
    const opcoes = ultimasOpcoes()
    expect(opcoes.shouldBlockFn({} as never)).toBe(true)
    expect(window.confirm).toHaveBeenCalled()
  })

  it('lets the navigation through when the owner confirms', () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    renderHook(() => useUnsavedGuard(true))
    expect(ultimasOpcoes().shouldBlockFn({} as never)).toBe(false)
  })

  it('enables the native beforeunload warning only with a dirty form', () => {
    const { result, rerender } = renderHook(
      ({ sujo }: { sujo: boolean }) => useUnsavedGuard(sujo),
      { initialProps: { sujo: false } },
    )
    expect(result.current).toBeUndefined()
    const enabled = ultimasOpcoes().enableBeforeUnload
    expect(typeof enabled).toBe('function')
    expect((enabled as () => boolean)()).toBe(false)

    rerender({ sujo: true })
    const enabledDirty = ultimasOpcoes().enableBeforeUnload
    expect((enabledDirty as () => boolean)()).toBe(true)
  })
})
