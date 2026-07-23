import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { DatasourceList } from '@/components/datasource-list'
import type { ScanStatus, Source } from '@/lib/api'
import { renderWithClient } from '@/test/render'

// Briefing 0071: a criação e a edição/remoção de path migraram para rotas
// dedicadas (`/library/new`, `/library/$id/edit` — cobertas em
// `library-new.test.tsx`/`library-edit.test.tsx`). Esta lista só cobre o que
// continua aqui: estado vazio, path+badge+progresso, "Escanear", o switch de
// watch e os links "Adicionar datasource"/"Editar".
vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock,
}))

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

// Fixture da fonte — a maioria dos testes só varia id/path/type/watch_enabled.
function sourceFixture(over: Partial<Source> = {}): Source {
  return {
    id: '1',
    path: '/mnt/filmes',
    type: 'movie',
    content_type: 'movie',
    state: 'online',
    last_scan: null,
    watch_enabled: false,
    ...over,
  }
}

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

  it('mostra o link "Adicionar datasource" apontando para /library/new', async () => {
    vi.stubGlobal('fetch', fetchRouter([], idle))
    renderWithClient(<DatasourceList />)

    const link = await screen.findByRole('link', {
      name: /Adicionar datasource/,
    })
    expect(link).toHaveAttribute('href', '/library/new')
  })

  it('lista o datasource com estado e progresso de scan em andamento', async () => {
    const source = sourceFixture()
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

  it('o link "Editar" de cada linha aponta para /library/$id/edit', async () => {
    const source = sourceFixture({ id: '42' })
    vi.stubGlobal('fetch', fetchRouter([source], idle))
    renderWithClient(<DatasourceList />)

    const link = await screen.findByRole('link', { name: /Editar/ })
    expect(link).toHaveAttribute('href', '/library/42/edit')
  })

  it('dispara o scan ao clicar em Escanear', async () => {
    const user = userEvent.setup()
    const source = sourceFixture({
      id: '7',
      path: '/tv',
      type: 'tv',
      content_type: 'series',
    })
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
    const source = sourceFixture({ id: '9' })
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

  it('não tem mais formulário fixo no topo nem botão de remoção direto na lista', async () => {
    const source = sourceFixture()
    vi.stubGlobal('fetch', fetchRouter([source], idle))
    renderWithClient(<DatasourceList />)

    await waitFor(() =>
      expect(screen.getByText('/mnt/filmes')).toBeInTheDocument(),
    )
    expect(
      screen.queryByLabelText('Caminho do datasource'),
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Remover datasource' }),
    ).not.toBeInTheDocument()
  })
})
