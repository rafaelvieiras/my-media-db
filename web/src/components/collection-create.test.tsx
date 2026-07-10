import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen } from '@testing-library/react'
import { CollectionCreate } from '@/components/collection-create'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson } from '@/test/detail-harness'

// Testa a criação de coleção LOCAL (0025 Fase B): o botão abre o form, o submit chama
// POST /collections e NAVEGA para a entity nova; o 422 (título vazio) aparece inline.

const spies = vi.hoisted(() => ({ navigate: vi.fn() }))

vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => spies.navigate,
}))

// stubCollections roteia POST /api/v1/collections para `resp`.
function stubCollections(resp: () => Response) {
  const m = vi.fn(async (url: string, init?: RequestInit) => {
    const u = new URL(String(url), 'http://localhost')
    const method = init?.method ?? 'GET'
    if (u.pathname === '/api/v1/collections' && method === 'POST') {
      return resp()
    }
    throw new Error(`rota nao mockada: ${method} ${u.pathname}`)
  })
  vi.stubGlobal('fetch', m)
  return m
}

afterEach(() => {
  vi.restoreAllMocks()
  spies.navigate.mockReset()
})

describe('CollectionCreate', () => {
  it('abre o formulário ao clicar em "Nova coleção"', () => {
    stubCollections(() => okJson({}, 201))
    renderWithClient(<CollectionCreate />)
    expect(screen.queryByLabelText('Título da coleção')).toBeNull()
    fireEvent.click(screen.getByText('Nova coleção'))
    expect(screen.getByLabelText('Título da coleção')).toBeTruthy()
  })

  it('cria a coleção e navega para /entities/{id}', async () => {
    const m = stubCollections(() =>
      okJson({ id: 'col-9', type: 'collection', title: 'Trilogia' }, 201),
    )
    renderWithClient(<CollectionCreate />)
    fireEvent.click(screen.getByText('Nova coleção'))
    fireEvent.change(screen.getByLabelText('Título da coleção'), {
      target: { value: 'Trilogia' },
    })
    fireEvent.click(screen.getByText('Criar coleção'))

    await vi.waitFor(() =>
      expect(spies.navigate).toHaveBeenCalledWith({
        to: '/entities/$id',
        params: { id: 'col-9' },
      }),
    )
    const corpo = JSON.parse(String(m.mock.calls[0][1]?.body))
    expect(corpo.title).toBe('Trilogia')
  })

  it('mostra o erro 422 inline e não navega', async () => {
    stubCollections(() => erroJson(422, 'invalid_body', 'titulo obrigatorio'))
    renderWithClient(<CollectionCreate />)
    fireEvent.click(screen.getByText('Nova coleção'))
    fireEvent.change(screen.getByLabelText('Título da coleção'), {
      target: { value: 'x' },
    })
    fireEvent.click(screen.getByText('Criar coleção'))

    expect(await screen.findByText('titulo obrigatorio')).toBeTruthy()
    expect(spies.navigate).not.toHaveBeenCalled()
  })
})
