import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { entidadeResolvidaPadrao, entidades } from '@/test/msw/handlers'
import { renderRota } from '@/test/route-tree-harness'

// Route `/entities/$id/edit/ids` (subroute of the item edit): the section
// "IDs de terceiros" — `ExternalIdsEditor` in isolation (the behavior of
// saving/re-enriching is already covered by `external-ids-editor.test.tsx`; the
// mechanics of the "leave without saving" guard is already covered in detail by
// `entity-edit-shell.test.tsx`). Here, over the REAL ROUTE TREE (the same
// MSW harness as the tests in `routes/__msw__/`), we cover only the mounting of this
// subroute and the WIRING of the guard (the "dirty" state of the `ExternalIdsEditor` reaches the
// `EntityEditShell` and intercepts the navigation for real).
describe('EntityEditIdsScreen (árvore real)', () => {
  beforeEach(() => {
    entidades.set('e1', entidadeResolvidaPadrao({ id: 'e1', title: 'Dune' }))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('mostra só o editor de IDs externos, com sidebar e cabeçalho', async () => {
    await renderRota('/entities/e1/edit/ids')
    await screen.findByRole('button', { name: 'Adicionar fonte' })
    expect(
      screen.getByRole('link', { name: 'IDs de terceiros' }),
    ).toHaveAttribute('aria-current', 'page')
    expect(
      screen.getByRole('link', { name: 'Informações básicas' }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('link', { name: 'Elenco e equipe' }),
    ).toBeInTheDocument()
  })

  it('id inexistente (404) mostra o estado de erro', async () => {
    await renderRota('/entities/e2/edit/ids')
    expect(
      await screen.findByText('Não foi possível carregar o item'),
    ).toBeInTheDocument()
  })

  it('editar os IDs sem salvar dispara confirmação; cancelar mantém, confirmar navega', async () => {
    const confirmar = vi.fn(() => false)
    vi.stubGlobal('confirm', confirmar)
    const user = userEvent.setup()
    await renderRota('/entities/e1/edit/ids')
    await user.click(
      await screen.findByRole('button', { name: 'Adicionar fonte' }),
    )
    await user.type(screen.getByLabelText('Fonte do id externo'), 'tmdb')
    await user.click(screen.getByRole('link', { name: 'Elenco e equipe' }))
    expect(confirmar).toHaveBeenCalledTimes(1)
    // It cancelled the exit: it stays in the same section, with the typed text intact.
    expect(screen.getByLabelText('Fonte do id externo')).toHaveValue('tmdb')

    // Confirming this time, the navigation actually happens.
    confirmar.mockReturnValue(true)
    await user.click(screen.getByRole('link', { name: 'Elenco e equipe' }))
    await waitFor(() =>
      expect(
        screen.getByRole('link', { name: 'Elenco e equipe' }),
      ).toHaveAttribute('aria-current', 'page'),
    )
  })
})
