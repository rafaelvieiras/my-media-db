import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { CollectionNewScreen } from '@/routes/collection-new'
import { renderWithClient } from '@/test/render'
import { okJson } from '@/test/entity-fixtures'
import { stubCriacao } from '@/test/criacao-harness'
import { erroJson } from '@/test/detail-harness'

// Route `/new/collection`: creates a LOCAL collection (title + description) and
// navigates to `/entities/{id}`; the 422 (empty title) appears inline.
const spies = vi.hoisted(() => ({ navigate: vi.fn() }))

vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => spies.navigate,
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
}))

const fetchRouter = (resp: () => Response) =>
  stubCriacao('/api/v1/collections', resp)

afterEach(() => {
  vi.restoreAllMocks()
  spies.navigate.mockReset()
})

describe('CollectionNewScreen', () => {
  it('só mostra os campos aplicáveis à coleção (título + descrição)', async () => {
    fetchRouter(() => okJson({}))
    renderWithClient(<CollectionNewScreen />)
    await screen.findByLabelText('Título')
    expect(screen.getByLabelText('Sinopse')).toBeInTheDocument()
    // Fields that do not apply to a collection do not appear.
    expect(screen.queryByLabelText('Data de lançamento')).toBeNull()
    expect(screen.queryByLabelText('Sua nota')).toBeNull()
  })

  it('cria a coleção e navega para /entities/{id}', async () => {
    const m = fetchRouter(() =>
      okJson({ id: 'col-9', type: 'collection', title: 'Trilogia' }),
    )
    renderWithClient(<CollectionNewScreen />)
    const user = userEvent.setup()

    await user.type(await screen.findByLabelText('Título'), 'Trilogia')
    await user.click(screen.getByRole('button', { name: 'Criar coleção' }))

    await vi.waitFor(() =>
      expect(spies.navigate).toHaveBeenCalledWith({
        to: '/entities/$id',
        params: { id: 'col-9' },
      }),
    )
    const corpo = JSON.parse(String(m.mock.calls.at(-1)?.[1]?.body))
    expect(corpo.title).toBe('Trilogia')
  })

  it('mostra o erro 422 inline e não navega', async () => {
    fetchRouter(() => erroJson(422, 'invalid_body', 'titulo obrigatorio'))
    renderWithClient(<CollectionNewScreen />)
    const user = userEvent.setup()

    await user.type(await screen.findByLabelText('Título'), 'x')
    await user.click(screen.getByRole('button', { name: 'Criar coleção' }))

    expect(await screen.findByText('titulo obrigatorio')).toBeInTheDocument()
    expect(spies.navigate).not.toHaveBeenCalled()
  })
})
