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
// "Elenco e equipe" section — `CreditsEditor` in isolation (the saving behavior
// and the batch reordering are already covered by `credits-editor.test.tsx`;
// the mechanics of the "leave without saving" guard is already covered in detail by
// `entity-edit-shell.test.tsx`). Here, over the REAL ROUTE TREE (the same
// MSW harness as the tests in `routes/__msw__/`), we cover only the mounting of this
// subroute and the WIRING of the guard (the "dirty" state of the `CreditsEditor` reaches the
// `EntityEditShell` and intercepts the navigation for real).
describe('EntityEditCreditosScreen (árvore real)', () => {
  beforeEach(() => {
    entidades.set(
      'e1',
      entidadeResolvidaPadrao({ id: 'e1', title: 'Dune', cast: CAST_DUPLO }),
    )
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('mostra só o editor de créditos, com sidebar e cabeçalho', async () => {
    await renderRota('/entities/e1/edit/creditos')
    await screen.findAllByLabelText('Mover para baixo')
    expect(screen.getByText('Ana')).toBeInTheDocument()
    expect(
      screen.getByRole('link', { name: 'Elenco e equipe' }),
    ).toHaveAttribute('aria-current', 'page')
    expect(
      screen.getByRole('link', { name: 'Informações básicas' }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('link', { name: 'IDs de terceiros' }),
    ).toBeInTheDocument()
  })

  it('id inexistente (404) mostra o estado de erro', async () => {
    await renderRota('/entities/e2/edit/creditos')
    expect(
      await screen.findByText('Não foi possível carregar o item'),
    ).toBeInTheDocument()
  })

  it('reordenar sem salvar dispara confirmação; cancelar mantém, confirmar navega', async () => {
    const confirmar = vi.fn(() => false)
    vi.stubGlobal('confirm', confirmar)
    const user = userEvent.setup()
    await renderRota('/entities/e1/edit/creditos')
    await user.click((await screen.findAllByLabelText('Mover para baixo'))[0])
    await user.click(screen.getByRole('link', { name: 'IDs de terceiros' }))
    expect(confirmar).toHaveBeenCalledTimes(1)
    // It cancelled the exit: it stays in the same section (Beto still 2nd, unchanged).
    expect(
      screen.getByRole('link', { name: 'Elenco e equipe' }),
    ).toHaveAttribute('aria-current', 'page')

    // Confirming this time, the navigation actually happens.
    confirmar.mockReturnValue(true)
    await user.click(screen.getByRole('link', { name: 'IDs de terceiros' }))
    await waitFor(() =>
      expect(
        screen.getByRole('link', { name: 'IDs de terceiros' }),
      ).toHaveAttribute('aria-current', 'page'),
    )
  })
})
