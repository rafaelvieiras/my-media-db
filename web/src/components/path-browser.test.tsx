import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { PathBrowser } from '@/components/path-browser'
import type { BrowseResult } from '@/lib/api'
import { renderWithClient } from '@/test/render'

// Navegador de pastas (briefing 0071): cobre navegação por clique/breadcrumb,
// "Usar esta pasta", entrada sem permissão de leitura e erro da API.

function okJson<T>(body: T): Response {
  return { ok: true, status: 200, json: async () => body } as Response
}

function erroJson(status: number, code: string, message: string): Response {
  return {
    ok: false,
    status,
    json: async () => ({ error: { code, message } }),
  } as Response
}

function browseResult(over: Partial<BrowseResult> = {}): BrowseResult {
  return {
    path: '/mnt',
    parent: '/',
    entries: [
      { name: 'filmes', path: '/mnt/filmes', readable: true },
      { name: 'privado', path: '/mnt/privado', readable: false },
    ],
    truncated: false,
    ...over,
  }
}

// Roteia GET /api/v1/library/browse?path=... por `path` — cada chave do mapa é
// a resposta esperada para aquele caminho ('erro' devolve 422 invalid_path).
function fetchRouter(byPath: Record<string, BrowseResult | 'erro'>) {
  return vi.fn(async (url: string) => {
    const u = new URL(String(url), 'http://localhost')
    if (u.pathname !== '/api/v1/library/browse') {
      throw new Error(`rota não mockada: ${u.pathname}`)
    }
    const path = u.searchParams.get('path') ?? '/'
    const resultado = byPath[path]
    if (resultado === 'erro') {
      return erroJson(422, 'invalid_path', 'o caminho informado não existe')
    }
    if (!resultado) {
      throw new Error(`path não mockado no teste: ${path}`)
    }
    return okJson(resultado)
  })
}

afterEach(() => {
  vi.restoreAllMocks()
})

// Abre o navegador já com o fetch roteado e o componente montado — a maioria
// dos testes só difere no mapa de rotas/`value`/`onChange`, então centraliza
// aqui o trio stubGlobal+render+clique em "Procurar pasta…".
async function montarEAbrir(
  byPath: Record<string, BrowseResult | 'erro'>,
  props: Partial<{ value: string; onChange: (path: string) => void }> = {},
) {
  const user = userEvent.setup()
  vi.stubGlobal('fetch', fetchRouter(byPath))
  const onChange = props.onChange ?? vi.fn()
  renderWithClient(
    <PathBrowser value={props.value ?? ''} onChange={onChange} />,
  )
  await user.click(screen.getByRole('button', { name: 'Procurar pasta…' }))
  return { user, onChange }
}

describe('PathBrowser', () => {
  it('começa colapsado atrás do botão "Procurar pasta…"', () => {
    vi.stubGlobal('fetch', fetchRouter({}))
    renderWithClient(<PathBrowser value="" onChange={vi.fn()} />)

    expect(
      screen.getByRole('button', { name: 'Procurar pasta…' }),
    ).toBeInTheDocument()
    expect(screen.queryByText('filmes')).not.toBeInTheDocument()
  })

  it('lista as subpastas da raiz ao abrir e navega ao clicar numa subpasta (breadcrumb reflete)', async () => {
    const { user } = await montarEAbrir({
      '/': browseResult({ path: '/', parent: null }),
      '/mnt/filmes': browseResult({
        path: '/mnt/filmes',
        parent: '/mnt',
        entries: [{ name: 'acao', path: '/mnt/filmes/acao', readable: true }],
      }),
    })

    await waitFor(() => expect(screen.getByText('filmes')).toBeInTheDocument())
    await user.click(screen.getByText('filmes'))

    await waitFor(() => expect(screen.getByText('acao')).toBeInTheDocument())
    // Breadcrumb mostra o segmento "mnt" como um botão clicável.
    expect(screen.getByRole('button', { name: 'mnt' })).toBeInTheDocument()
  })

  it('navega para cima ao clicar num segmento do breadcrumb', async () => {
    const { user } = await montarEAbrir(
      {
        '/': browseResult({ path: '/', parent: null }),
        '/mnt/filmes': browseResult({
          path: '/mnt/filmes',
          parent: '/mnt',
          entries: [{ name: 'acao', path: '/mnt/filmes/acao', readable: true }],
        }),
        '/mnt': browseResult({ path: '/mnt', parent: '/' }),
      },
      { value: '/mnt/filmes' },
    )

    await waitFor(() => expect(screen.getByText('acao')).toBeInTheDocument())
    await user.click(screen.getByRole('button', { name: 'mnt' }))

    await waitFor(() => expect(screen.getByText('filmes')).toBeInTheDocument())
  })

  it('entrada sem permissão de leitura aparece desabilitada e não navega', async () => {
    const { user } = await montarEAbrir({
      '/': browseResult({ path: '/', parent: null }),
    })

    await waitFor(() => expect(screen.getByText('privado')).toBeInTheDocument())
    const botaoPrivado = screen.getByRole('button', { name: /privado/ })
    expect(botaoPrivado).toBeDisabled()

    await user.click(botaoPrivado)
    // Continua mostrando o conteúdo da raiz — não navegou para /mnt/privado.
    expect(screen.getByText('filmes')).toBeInTheDocument()
  })

  it('"Usar esta pasta" chama onChange com o path atual (normalizado pela API)', async () => {
    const { user, onChange } = await montarEAbrir({
      '/': browseResult({ path: '/', parent: null }),
    })

    await waitFor(() => expect(screen.getByText('filmes')).toBeInTheDocument())
    await user.click(screen.getByRole('button', { name: 'Usar esta pasta' }))

    expect(onChange).toHaveBeenCalledWith('/')
  })

  it('desabilita "Subir um nível" quando já está na raiz (parent null)', async () => {
    await montarEAbrir({ '/': browseResult({ path: '/', parent: null }) })

    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: /Subir um nível/ }),
      ).toBeDisabled(),
    )
  })

  it('mostra o erro da API de forma sensata quando o path não é navegável', async () => {
    await montarEAbrir({ '/': 'erro' })

    expect(
      await screen.findByText('o caminho informado não existe'),
    ).toBeInTheDocument()
    expect(screen.getByRole('alert')).toBeInTheDocument()
  })

  it('mostra o aviso quando a resposta vem truncada', async () => {
    await montarEAbrir({
      '/': browseResult({ path: '/', parent: null, truncated: true }),
    })

    expect(
      await screen.findByText('Mostrando as primeiras 2000 pastas.'),
    ).toBeInTheDocument()
  })

  it('caminho inicial quebrado (fonte offline) recupera sozinho para a raiz em vez de travar', async () => {
    await montarEAbrir(
      {
        '/media/quebrado': 'erro',
        '/': browseResult({ path: '/', parent: null }),
      },
      { value: '/media/quebrado' },
    )

    // Erra ao abrir no path quebrado, mas se recupera para a raiz sozinho —
    // não fica preso na mensagem de erro esperando o dono clicar em algo.
    await waitFor(() => expect(screen.getByText('filmes')).toBeInTheDocument())
  })

  it('navegação manual não é desfeita pelo debounce de sincronização do texto digitado (regressão)', async () => {
    // `value` do chamador permanece parado em '/mnt' (o dono não editou o
    // campo de texto) enquanto o dono navega manualmente pelo navegador —
    // antes da correção, o debounce de sincronização reagia a QUALQUER
    // divergência entre `pasta` e `value`, não só a uma edição real do
    // campo, e revertia a navegação de volta para `value` ~400ms depois.
    const { user } = await montarEAbrir(
      {
        '/mnt': browseResult({
          path: '/mnt',
          parent: '/',
          entries: [{ name: 'filmes', path: '/mnt/filmes', readable: true }],
        }),
        '/mnt/filmes': browseResult({
          path: '/mnt/filmes',
          parent: '/mnt',
          entries: [{ name: 'acao', path: '/mnt/filmes/acao', readable: true }],
        }),
      },
      { value: '/mnt' },
    )

    await waitFor(() => expect(screen.getByText('filmes')).toBeInTheDocument())
    await user.click(screen.getByText('filmes'))
    await waitFor(() => expect(screen.getByText('acao')).toBeInTheDocument())

    // Espera passar do intervalo do debounce (400ms) sem editar `value`: a
    // navegação para /mnt/filmes deve continuar de pé.
    await new Promise((resolve) => setTimeout(resolve, 600))
    expect(screen.getByText('acao')).toBeInTheDocument()
  })
})
