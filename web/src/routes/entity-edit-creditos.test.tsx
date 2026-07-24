import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { entidadeResolvidaPadrao, entidades } from '@/test/msw/handlers'
import { renderRota } from '@/test/route-tree-harness'

const CAST_DUPLO = [
  { source_id: '1', name: 'Ana', character: 'Heroína', order: 0 },
  { source_id: '2', name: 'Beto', character: 'Vilão', order: 1 },
]

// Rota `/entities/$id/edit/creditos` (briefing 0080, subrota da edição 0030): a
// seção "Elenco e equipe" — `CreditsEditor` isolado (o comportamento de salvar
// e a reordenação em lote 0076 já são cobertos por `credits-editor.test.tsx`;
// a mecânica do guard de "sair sem salvar" já é coberta em detalhe por
// `entity-edit-shell.test.tsx`). Aqui, sobre a ÁRVORE DE ROTA REAL (mesmo
// harness MSW dos testes em `routes/__msw__/`), cobrimos só a montagem desta
// subrota e a FIAÇÃO do guard (o "sujo" do `CreditsEditor` chega até o
// `EntityEditShell` e intercepta a navegação de verdade).
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
    // Cancelou a saída: segue na mesma seção (Beto ainda em 2º, não trocou).
    expect(
      screen.getByRole('link', { name: 'Elenco e equipe' }),
    ).toHaveAttribute('aria-current', 'page')

    // Confirmando desta vez, a navegação de fato acontece.
    confirmar.mockReturnValue(true)
    await user.click(screen.getByRole('link', { name: 'IDs de terceiros' }))
    await waitFor(() =>
      expect(
        screen.getByRole('link', { name: 'IDs de terceiros' }),
      ).toHaveAttribute('aria-current', 'page'),
    )
  })
})
