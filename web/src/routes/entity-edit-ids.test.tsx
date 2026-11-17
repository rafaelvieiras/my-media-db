import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { entidadeResolvidaPadrao, entidades } from '@/test/msw/handlers'
import { renderRota } from '@/test/route-tree-harness'

// Route `/entities/$id/edit/ids` (subroute of the item edit): the section
// "External IDs" — `ExternalIdsEditor` in isolation (the behavior of
// saving/re-enriching is already covered by `external-ids-editor.test.tsx`; the
// mechanics of the "leave without saving" guard is already covered in detail by
// `entity-edit-shell.test.tsx`). Here, over the REAL ROUTE TREE (the same
// MSW harness as the tests in `routes/__msw__/`), we cover only the mounting of this
// subroute and the WIRING of the guard (the "dirty" state of the `ExternalIdsEditor` reaches the
// `EntityEditShell` and intercepts the navigation for real).
// The item loading/error state comes from `components/query-states.tsx`, which
// another domain owns: this query tolerates both labels so the test does not depend
// on that extraction.
const ESTADO_ERRO = /Não foi possível carregar o item|Could not load the item/

describe('EntityEditIdsScreen (real route tree)', () => {
  beforeEach(() => {
    entidades.set('e1', entidadeResolvidaPadrao({ id: 'e1', title: 'Dune' }))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('shows only the external IDs editor, with the sidebar and the header', async () => {
    await renderRota('/entities/e1/edit/ids')
    await screen.findByRole('button', { name: 'Add source' })
    expect(screen.getByRole('link', { name: 'External IDs' })).toHaveAttribute(
      'aria-current',
      'page',
    )
    expect(
      screen.getByRole('link', { name: 'Basic information' }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('link', { name: 'Cast and crew' }),
    ).toBeInTheDocument()
  })

  it('a missing id (404) shows the error state', async () => {
    await renderRota('/entities/e2/edit/ids')
    expect(await screen.findByText(ESTADO_ERRO)).toBeInTheDocument()
  })

  it('editing the IDs without saving fires the confirmation; cancelling keeps, confirming navigates', async () => {
    const confirmar = vi.fn(() => false)
    vi.stubGlobal('confirm', confirmar)
    const user = userEvent.setup()
    await renderRota('/entities/e1/edit/ids')
    await user.click(await screen.findByRole('button', { name: 'Add source' }))
    await user.type(screen.getByLabelText('External id source'), 'tmdb')
    await user.click(screen.getByRole('link', { name: 'Cast and crew' }))
    expect(confirmar).toHaveBeenCalledTimes(1)
    // It cancelled the exit: it stays in the same section, with the typed text intact.
    expect(screen.getByLabelText('External id source')).toHaveValue('tmdb')

    // Confirming this time, the navigation actually happens.
    confirmar.mockReturnValue(true)
    await user.click(screen.getByRole('link', { name: 'Cast and crew' }))
    await waitFor(() =>
      expect(
        screen.getByRole('link', { name: 'Cast and crew' }),
      ).toHaveAttribute('aria-current', 'page'),
    )
  })
})
