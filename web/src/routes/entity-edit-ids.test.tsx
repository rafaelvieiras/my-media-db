import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { entidadeResolvidaPadrao, entidades } from '@/test/msw/handlers'
import { renderRota } from '@/test/route-tree-harness'

// Rota `/entities/$id/edit/ids` (briefing 0080, subrota da edição 0030): a seção
// "IDs de terceiros" — `ExternalIdsEditor` isolado (o comportamento de
// salvar/re-enriquecer já é coberto por `external-ids-editor.test.tsx`; a
// mecânica do guard de "sair sem salvar" já é coberta em detalhe por
// `entity-edit-shell.test.tsx`). Aqui, sobre a ÁRVORE DE ROTA REAL (mesmo
// harness MSW dos testes em `routes/__msw__/`), cobrimos só a montagem desta
// subrota e a FIAÇÃO do guard (o "sujo" do `ExternalIdsEditor` chega até o
// `EntityEditShell` e intercepta a navegação de verdade).
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
    // Cancelou a saída: segue na mesma seção, com o texto digitado intacto.
    expect(screen.getByLabelText('Fonte do id externo')).toHaveValue('tmdb')

    // Confirmando desta vez, a navegação de fato acontece.
    confirmar.mockReturnValue(true)
    await user.click(screen.getByRole('link', { name: 'Elenco e equipe' }))
    await waitFor(() =>
      expect(
        screen.getByRole('link', { name: 'Elenco e equipe' }),
      ).toHaveAttribute('aria-current', 'page'),
    )
  })
})
