import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { ItemNewScreen } from '@/routes/item-new'
import { renderWithClient } from '@/test/render'
import { okJson } from '@/test/entity-fixtures'
import { stubCriacao } from '@/test/criacao-harness'
import { erroJson } from '@/test/detail-harness'

// Rota `/new/item` (0030): o formulário dirigido por schema cria um item LOCAL e
// navega para `/entities/{id}`; o 422 aparece inline. `useNavigate` é spy; `Link`
// vira um <a>.
const spies = vi.hoisted(() => ({ navigate: vi.fn() }))

vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => spies.navigate,
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
}))

// fetchRouter serve os descritores (GET) e roteia o POST /entities para `resp`.
const fetchRouter = (resp: () => Response) =>
  stubCriacao('/api/v1/entities', resp)

afterEach(() => {
  vi.restoreAllMocks()
  spies.navigate.mockReset()
})

describe('ItemNewScreen', () => {
  it('cria o item (type+título) e navega para /entities/{id}', async () => {
    const m = fetchRouter(() =>
      okJson({ id: 'mv-3', type: 'tv', title: 'Severance' }),
    )
    renderWithClient(<ItemNewScreen />)
    const user = userEvent.setup()

    await user.selectOptions(await screen.findByLabelText('Tipo do item'), 'tv')
    await user.type(screen.getByLabelText('Título'), 'Severance')
    await user.click(screen.getByRole('button', { name: 'Criar item' }))

    await vi.waitFor(() =>
      expect(spies.navigate).toHaveBeenCalledWith({
        to: '/entities/$id',
        params: { id: 'mv-3' },
      }),
    )
    const corpo = JSON.parse(String(m.mock.calls.at(-1)?.[1]?.body))
    expect(corpo.type).toBe('tv')
    expect(corpo.title).toBe('Severance')
  })

  it('não submete com título vazio (botão desabilitado)', async () => {
    fetchRouter(() => okJson({}))
    renderWithClient(<ItemNewScreen />)
    const botao = await screen.findByRole('button', { name: 'Criar item' })
    expect(botao).toBeDisabled()
  })

  it('mostra o erro 422 inline e não navega', async () => {
    fetchRouter(() => erroJson(422, 'invalid_body', 'titulo obrigatorio'))
    renderWithClient(<ItemNewScreen />)
    const user = userEvent.setup()

    await user.type(await screen.findByLabelText('Título'), 'x')
    await user.click(screen.getByRole('button', { name: 'Criar item' }))

    expect(await screen.findByText('titulo obrigatorio')).toBeInTheDocument()
    expect(spies.navigate).not.toHaveBeenCalled()
  })
})
