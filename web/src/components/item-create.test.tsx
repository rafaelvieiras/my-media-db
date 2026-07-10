import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen } from '@testing-library/react'
import { ItemCreate } from '@/components/item-create'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson } from '@/test/detail-harness'
import { corpoEnviado, stubRota } from '@/test/stub-fetch'

// Testa a criação de item LOCAL sem fonte (0027): o botão abre o form, o submit chama
// POST /entities com type+título e NAVEGA para a entity nova; o 422 aparece inline.

const spies = vi.hoisted(() => ({ navigate: vi.fn() }))

vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => spies.navigate,
}))

// postEntities roteia POST /api/v1/entities para `resp`.
const postEntities = (resp: () => Response) =>
  stubRota((p, m) => p === '/api/v1/entities' && m === 'POST', resp)

afterEach(() => {
  vi.restoreAllMocks()
  spies.navigate.mockReset()
})

describe('ItemCreate', () => {
  it('abre o formulário ao clicar em "Novo item"', () => {
    postEntities(() => okJson({}, 201))
    renderWithClient(<ItemCreate />)
    expect(screen.queryByLabelText('Título do item')).toBeNull()
    fireEvent.click(screen.getByText('Novo item'))
    expect(screen.getByLabelText('Título do item')).toBeTruthy()
    expect(screen.getByLabelText('Tipo do item')).toBeTruthy()
  })

  it('cria o item (type+título) e navega para /entities/{id}', async () => {
    const m = postEntities(() =>
      okJson({ id: 'mv-3', type: 'tv', title: 'Severance' }, 201),
    )
    renderWithClient(<ItemCreate />)
    fireEvent.click(screen.getByText('Novo item'))
    fireEvent.change(screen.getByLabelText('Tipo do item'), {
      target: { value: 'tv' },
    })
    fireEvent.change(screen.getByLabelText('Título do item'), {
      target: { value: 'Severance' },
    })
    fireEvent.click(screen.getByText('Criar item'))

    await vi.waitFor(() =>
      expect(spies.navigate).toHaveBeenCalledWith({
        to: '/entities/$id',
        params: { id: 'mv-3' },
      }),
    )
    const corpo = corpoEnviado(m) as { type: string; title: string }
    expect(corpo.type).toBe('tv')
    expect(corpo.title).toBe('Severance')
  })

  it('não submete com título vazio (botão desabilitado)', () => {
    postEntities(() => okJson({}, 201))
    renderWithClient(<ItemCreate />)
    fireEvent.click(screen.getByText('Novo item'))
    const botao = screen.getByText('Criar item').closest('button')
    expect(botao?.disabled).toBe(true)
  })

  it('mostra o erro 422 inline e não navega', async () => {
    postEntities(() => erroJson(422, 'invalid_body', 'titulo obrigatorio'))
    renderWithClient(<ItemCreate />)
    fireEvent.click(screen.getByText('Novo item'))
    fireEvent.change(screen.getByLabelText('Título do item'), {
      target: { value: 'x' },
    })
    fireEvent.click(screen.getByText('Criar item'))

    expect(await screen.findByText('titulo obrigatorio')).toBeTruthy()
    expect(spies.navigate).not.toHaveBeenCalled()
  })
})
