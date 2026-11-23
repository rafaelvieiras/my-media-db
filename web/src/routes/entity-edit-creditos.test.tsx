import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { entidadeResolvidaPadrao, entidades } from '@/test/msw/handlers'
import { renderRota } from '@/test/route-tree-harness'

const CAST_DUPLO = [
  { source_id: '1', name: 'Ana', character: 'Heroína', order: 0 },
  { source_id: '2', name: 'Beto', character: 'Vilão', order: 1 },
]

// Route `/entities/$id/edit/creditos` (subroute of the item edit): the
// "Cast and crew" section — `CreditsEditor` in isolation (the saving behavior
// and the batch reordering are already covered by `credits-editor.test.tsx`;
// the mechanics of the "leave without saving" guard is already covered in detail by
// `entity-edit-shell.test.tsx`). Here, over the REAL ROUTE TREE (the same
// MSW harness as the tests in `routes/__msw__/`), we cover only the mounting of this
// subroute and the WIRING of the guard (the "dirty" state of the `CreditsEditor` reaches the
// `EntityEditShell` and intercepts the navigation for real).
// The item loading/error state comes from `components/query-states.tsx`, which
// another domain owns: this query tolerates both labels so the test does not depend
// on that extraction.
const ESTADO_ERRO = /Não foi possível carregar o item|Could not load the item/

describe('EntityEditCreditosScreen (real route tree)', () => {
  beforeEach(() => {
    entidades.set(
      'e1',
      entidadeResolvidaPadrao({ id: 'e1', title: 'Dune', cast: CAST_DUPLO }),
    )
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('shows only the credits editor, with the sidebar and the header', async () => {
    await renderRota('/entities/e1/edit/creditos')
    await screen.findAllByLabelText('Move down')
    expect(screen.getByText('Ana')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Cast and crew' })).toHaveAttribute(
      'aria-current',
      'page',
    )
    expect(
      screen.getByRole('link', { name: 'Basic information' }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('link', { name: 'External IDs' }),
    ).toBeInTheDocument()
  })

  it('a missing id (404) shows the error state', async () => {
    await renderRota('/entities/e2/edit/creditos')
    expect(await screen.findByText(ESTADO_ERRO)).toBeInTheDocument()
  })

  it('reordering without saving fires the confirmation; cancelling keeps, confirming navigates', async () => {
    const user = userEvent.setup()
    await renderRota('/entities/e1/edit/creditos')
    await user.click((await screen.findAllByLabelText('Move down'))[0])

    // First attempt, cancelled: the product dialog asks and the owner stays.
    await user.click(screen.getByRole('link', { name: 'External IDs' }))
    await user.click(await screen.findByRole('button', { name: 'Cancel' }))
    expect(screen.getByRole('link', { name: 'Cast and crew' })).toHaveAttribute(
      'aria-current',
      'page',
    )

    // Second attempt, confirmed: the navigation actually happens.
    await user.click(screen.getByRole('link', { name: 'External IDs' }))
    await user.click(
      await screen.findByRole('button', { name: 'Leave without saving' }),
    )
    await waitFor(() =>
      expect(
        screen.getByRole('link', { name: 'External IDs' }),
      ).toHaveAttribute('aria-current', 'page'),
    )
  })
})
