import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { DatasourceForm } from '@/components/datasource-form'
import { renderWithClient } from '@/test/render'

afterEach(() => {
  vi.restoreAllMocks()
})

describe('DatasourceForm', () => {
  it('adiciona um datasource via API e limpa o caminho', async () => {
    const user = userEvent.setup()
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 201,
      json: async () => ({
        id: '1',
        path: '/mnt/filmes',
        type: 'movie',
        state: 'online',
        last_scan: null,
      }),
    } as Response)
    vi.stubGlobal('fetch', fetchMock)

    renderWithClient(<DatasourceForm />)

    const caminho = screen.getByLabelText('Caminho do datasource')
    await user.type(caminho, '/mnt/filmes')
    await user.click(screen.getByRole('button', { name: /Adicionar/ }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/v1/library')
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body)).toEqual({
      path: '/mnt/filmes',
      type: 'movie',
    })
    // Ao concluir com sucesso, o campo de caminho é limpo.
    await waitFor(() => expect(caminho).toHaveValue(''))
  })

  it('exibe o erro da API inline (path inválido)', async () => {
    const user = userEvent.setup()
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 422,
      json: async () => ({
        error: {
          code: 'invalid_path',
          message: 'o caminho informado não existe',
        },
      }),
    } as Response)
    vi.stubGlobal('fetch', fetchMock)

    renderWithClient(<DatasourceForm />)

    await user.type(
      screen.getByLabelText('Caminho do datasource'),
      '/nao/existe',
    )
    await user.click(screen.getByRole('button', { name: /Adicionar/ }))

    await waitFor(() =>
      expect(
        screen.getByText('o caminho informado não existe'),
      ).toBeInTheDocument(),
    )
    expect(screen.getByRole('alert')).toBeInTheDocument()
  })

  it('não chama a API com caminho vazio', async () => {
    const user = userEvent.setup()
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    renderWithClient(<DatasourceForm />)
    await user.click(screen.getByRole('button', { name: /Adicionar/ }))

    expect(fetchMock).not.toHaveBeenCalled()
  })
})
