import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { DatasourceList } from '@/components/datasource-list'
import type { ScanStatus, Source } from '@/lib/api'
import { renderWithClient } from '@/test/render'

function okJson<T>(body: T, status = 200) {
  return { ok: true, status, json: async () => body } as Response
}

// Roteia o fetch por URL/método para simular a API de biblioteca+scan.
function fetchRouter(sources: Source[], status: ScanStatus) {
  return vi.fn(async (url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'
    if (u === '/api/v1/library' && method === 'GET') {
      return okJson({ items: sources })
    }
    if (u.endsWith('/scan') && method === 'GET') {
      return okJson(status)
    }
    if (u.endsWith('/scan') && method === 'POST') {
      return okJson({ job_id: 'j', status: 'accepted' }, 202)
    }
    if (method === 'DELETE') {
      return { ok: true, status: 204 } as Response
    }
    if (u.startsWith('/api/v1/library/') && method === 'PATCH') {
      const source = sources.find((s) => u === `/api/v1/library/${s.id}`)
      const body = JSON.parse(String(init?.body ?? '{}')) as {
        watch_enabled?: boolean
        path?: string
      }
      // Muta em memória para que o refetch pós-invalidação (disparado pelo
      // componente) reflita o novo estado, como faria a API real.
      Object.assign(source ?? {}, body)
      return okJson({ ...source, ...body })
    }
    throw new Error(`rota não mockada: ${method} ${u}`)
  })
}

const idle: ScanStatus = { running: false, files_seen: 0, files_total: 0 }

afterEach(() => {
  vi.restoreAllMocks()
})

describe('DatasourceList', () => {
  it('mostra o estado vazio quando não há datasources', async () => {
    vi.stubGlobal('fetch', fetchRouter([], idle))
    renderWithClient(<DatasourceList />)
    await waitFor(() =>
      expect(
        screen.getByText('Nenhum datasource cadastrado'),
      ).toBeInTheDocument(),
    )
  })

  it('lista o datasource com estado e progresso de scan em andamento', async () => {
    const source: Source = {
      id: '1',
      path: '/mnt/filmes',
      type: 'movie',
      content_type: 'movie',
      state: 'online',
      last_scan: null,
      watch_enabled: false,
    }
    vi.stubGlobal(
      'fetch',
      fetchRouter([source], {
        running: true,
        files_seen: 5,
        files_total: 10,
      }),
    )
    renderWithClient(<DatasourceList />)

    await waitFor(() =>
      expect(screen.getByText('/mnt/filmes')).toBeInTheDocument(),
    )
    expect(screen.getByText('online')).toBeInTheDocument()
    await waitFor(() =>
      expect(
        screen.getByText('Escaneando… 5 de 10 arquivos vistos'),
      ).toBeInTheDocument(),
    )
  })

  it('dispara o scan ao clicar em Escanear', async () => {
    const user = userEvent.setup()
    const source: Source = {
      id: '7',
      path: '/tv',
      type: 'tv',
      content_type: 'series',
      state: 'online',
      last_scan: null,
      watch_enabled: false,
    }
    const fetchMock = fetchRouter([source], idle)
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<DatasourceList />)

    await waitFor(() => expect(screen.getByText('/tv')).toBeInTheDocument())
    await user.click(screen.getByRole('button', { name: /Escanear/ }))

    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(
          ([u, init]) =>
            String(u) === '/api/v1/library/7/scan' &&
            (init as RequestInit | undefined)?.method === 'POST',
        ),
      ).toBe(true),
    )
  })

  it('liga o watch de filesystem via PATCH ao alternar o switch', async () => {
    const user = userEvent.setup()
    const source: Source = {
      id: '9',
      path: '/mnt/filmes',
      type: 'movie',
      content_type: 'movie',
      state: 'online',
      last_scan: null,
      watch_enabled: false,
    }
    const fetchMock = fetchRouter([source], idle)
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<DatasourceList />)

    await waitFor(() =>
      expect(screen.getByText('/mnt/filmes')).toBeInTheDocument(),
    )
    const toggle = screen.getByRole('switch', {
      name: 'Observar mudanças nesta pasta',
    })
    expect(toggle).not.toBeChecked()
    await user.click(toggle)

    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(([u, init]) => {
          const i = init as RequestInit | undefined
          return (
            String(u) === '/api/v1/library/9' &&
            i?.method === 'PATCH' &&
            JSON.parse(String(i.body)).watch_enabled === true
          )
        }),
      ).toBe(true),
    )
    await waitFor(() => expect(toggle).toBeChecked())
  })

  it('edita o caminho via PATCH ao salvar o formulário inline (briefing 0070)', async () => {
    const user = userEvent.setup()
    const source: Source = {
      id: '11',
      path: '/mnt/antigo',
      type: 'movie',
      content_type: 'movie',
      state: 'offline',
      last_scan: null,
      watch_enabled: false,
    }
    const fetchMock = fetchRouter([source], idle)
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<DatasourceList />)

    await waitFor(() =>
      expect(screen.getByText('/mnt/antigo')).toBeInTheDocument(),
    )
    await user.click(screen.getByRole('button', { name: 'Editar caminho' }))

    const input = screen.getByLabelText('Novo caminho do datasource')
    expect(input).toHaveValue('/mnt/antigo')
    await user.clear(input)
    await user.type(input, '/mnt/novo')
    await user.click(screen.getByRole('button', { name: 'Salvar' }))

    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(([u, init]) => {
          const i = init as RequestInit | undefined
          return (
            String(u) === '/api/v1/library/11' &&
            i?.method === 'PATCH' &&
            JSON.parse(String(i.body)).path === '/mnt/novo'
          )
        }),
      ).toBe(true),
    )
    // Sucesso: volta a mostrar o path (agora atualizado) sem o formulário.
    await waitFor(() =>
      expect(screen.getByText('/mnt/novo')).toBeInTheDocument(),
    )
    expect(
      screen.queryByLabelText('Novo caminho do datasource'),
    ).not.toBeInTheDocument()
  })

  it('mostra o erro inline ao editar caminho para um path inválido', async () => {
    const user = userEvent.setup()
    const source: Source = {
      id: '12',
      path: '/mnt/atual',
      type: 'movie',
      content_type: 'movie',
      state: 'online',
      last_scan: null,
      watch_enabled: false,
    }
    const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
      const u = String(url)
      const method = init?.method ?? 'GET'
      if (u === '/api/v1/library' && method === 'GET') {
        return okJson({ items: [source] })
      }
      if (u.endsWith('/scan') && method === 'GET') {
        return okJson(idle)
      }
      if (u === `/api/v1/library/${source.id}` && method === 'PATCH') {
        return {
          ok: false,
          status: 422,
          json: async () => ({
            error: {
              code: 'invalid_path',
              message: 'o caminho informado não existe',
            },
          }),
        } as Response
      }
      throw new Error(`rota não mockada: ${method} ${u}`)
    })
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<DatasourceList />)

    await waitFor(() =>
      expect(screen.getByText('/mnt/atual')).toBeInTheDocument(),
    )
    await user.click(screen.getByRole('button', { name: 'Editar caminho' }))
    await user.clear(screen.getByLabelText('Novo caminho do datasource'))
    await user.type(
      screen.getByLabelText('Novo caminho do datasource'),
      '/nao/existe',
    )
    await user.click(screen.getByRole('button', { name: 'Salvar' }))

    await waitFor(() =>
      expect(
        screen.getByText('o caminho informado não existe'),
      ).toBeInTheDocument(),
    )
    expect(screen.getByRole('alert')).toBeInTheDocument()
    // Erro fica inline: o formulário de edição permanece aberto.
    expect(
      screen.getByLabelText('Novo caminho do datasource'),
    ).toBeInTheDocument()
  })

  it('cancela a edição de caminho sem chamar a API', async () => {
    const user = userEvent.setup()
    const source: Source = {
      id: '13',
      path: '/mnt/original',
      type: 'movie',
      content_type: 'movie',
      state: 'online',
      last_scan: null,
      watch_enabled: false,
    }
    const fetchMock = fetchRouter([source], idle)
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<DatasourceList />)

    await waitFor(() =>
      expect(screen.getByText('/mnt/original')).toBeInTheDocument(),
    )
    await user.click(screen.getByRole('button', { name: 'Editar caminho' }))
    await user.type(
      screen.getByLabelText('Novo caminho do datasource'),
      '-mudanca',
    )
    await user.click(
      screen.getByRole('button', { name: 'Cancelar edição do caminho' }),
    )

    expect(
      screen.queryByLabelText('Novo caminho do datasource'),
    ).not.toBeInTheDocument()
    expect(
      fetchMock.mock.calls.some(
        ([u, init]) =>
          String(u) === '/api/v1/library/13' &&
          (init as RequestInit | undefined)?.method === 'PATCH',
      ),
    ).toBe(false)
  })
})
