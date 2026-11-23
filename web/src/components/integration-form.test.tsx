import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { IntegrationForm } from '@/components/integration-form'
import type { IntegrationField } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson } from '@/test/detail-harness'

// The "leave without saving" guard is the router blocker: outside a
// RouterProvider it cannot run, so it is mocked here (the hook has its own test and
// each screen asserts the argument it passes).
vi.mock('@/lib/use-unsaved-guard', () => ({ useUnsavedGuard: vi.fn() }))

// IntegrationForm: the new `secret` widget over the vocabulary
// of `integration.Campo` — it never rehydrates from the server, caption by
// `origem`, a "Clear" button only when `origem==="banco"`, differential PUT
// (only touched/cleared fields enter `valores`).

function campoSegredo(over: Partial<IntegrationField> = {}): IntegrationField {
  return {
    nome: 'token',
    label: 'Read Access Token (v4)',
    tipo: 'secret',
    obrigatorio: false,
    definido: false,
    origem: 'nenhum',
    ...over,
  }
}

function fetchRouter(putResposta?: () => Response) {
  return vi.fn(async (url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'
    if (u === '/api/v1/integrations/tmdb' && method === 'PUT') {
      return putResposta
        ? putResposta()
        : okJson({ slug: 'tmdb', nome: 'TMDB', descricao: '', campos: [] })
    }
    throw new Error(`rota nao mockada: ${method} ${u}`)
  })
}

function corpoPut(fetchMock: ReturnType<typeof fetchRouter>): {
  valores: Record<string, string | null>
} {
  const chamada = fetchMock.mock.calls.find(
    ([url, init]) =>
      String(url) === '/api/v1/integrations/tmdb' &&
      (init as RequestInit | undefined)?.method === 'PUT',
  )
  return JSON.parse(String(chamada?.[1]?.body))
}

// `FormSaveFooter` is a shared component outside this domain (its own extraction
// lands in parallel), so the submit label is accepted in either language.
const BOTAO_SALVAR = /^(Save|Salvar)$/

afterEach(() => {
  vi.restoreAllMocks()
})

describe('IntegrationForm', () => {
  it('the secret field never pre-fills, even with definido:true/origem:"banco"', () => {
    vi.stubGlobal('fetch', fetchRouter())
    renderWithClient(
      <IntegrationForm
        slug="tmdb"
        campos={[campoSegredo({ definido: true, origem: 'banco' })]}
      />,
    )
    expect(screen.getByLabelText('Read Access Token (v4)')).toHaveValue('')
  })

  it('origem "banco": "Set in the panel." caption + Clear button', () => {
    vi.stubGlobal('fetch', fetchRouter())
    renderWithClient(
      <IntegrationForm
        slug="tmdb"
        campos={[campoSegredo({ definido: true, origem: 'banco' })]}
      />,
    )
    expect(screen.getByText('Set in the panel.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Clear' })).toBeInTheDocument()
  })

  it('origem "env" with a known hint (tmdb/token) names the exact variable', () => {
    vi.stubGlobal('fetch', fetchRouter())
    renderWithClient(
      <IntegrationForm
        slug="tmdb"
        campos={[campoSegredo({ definido: true, origem: 'env' })]}
      />,
    )
    expect(
      screen.getByText(
        'Set by the MYMEDIADB_TMDB_TOKEN environment variable — save here to override.',
      ),
    ).toBeInTheDocument()
    // origem "env" does not show the Limpar button (there is no override in the database to remove).
    expect(
      screen.queryByRole('button', { name: 'Clear' }),
    ).not.toBeInTheDocument()
  })

  it('origem "env" without a known hint falls back to the generic text (without inventing a variable name)', () => {
    vi.stubGlobal('fetch', fetchRouter())
    renderWithClient(
      <IntegrationForm
        slug="outra-integracao"
        campos={[
          campoSegredo({
            nome: 'chave',
            label: 'Chave',
            definido: true,
            origem: 'env',
          }),
        ]}
      />,
    )
    expect(
      screen.getByText(
        'Set by a server environment variable — save here to override.',
      ),
    ).toBeInTheDocument()
  })

  it('origem "nenhum": no caption and no Clear button', () => {
    vi.stubGlobal('fetch', fetchRouter())
    renderWithClient(<IntegrationForm slug="tmdb" campos={[campoSegredo()]} />)
    expect(screen.queryByText(/^Set (in|by) /)).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Clear' }),
    ).not.toBeInTheDocument()
  })

  it('the show/hide toggle switches the input type', async () => {
    const user = userEvent.setup()
    vi.stubGlobal('fetch', fetchRouter())
    renderWithClient(<IntegrationForm slug="tmdb" campos={[campoSegredo()]} />)

    const campo = screen.getByLabelText('Read Access Token (v4)')
    expect(campo).toHaveAttribute('type', 'password')

    await user.click(
      screen.getByRole('button', { name: 'Show Read Access Token (v4)' }),
    )
    expect(campo).toHaveAttribute('type', 'text')

    await user.click(
      screen.getByRole('button', { name: 'Hide Read Access Token (v4)' }),
    )
    expect(campo).toHaveAttribute('type', 'password')
  })

  it('typing a new value and saving sends ONLY that field in the PUT', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter()
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<IntegrationForm slug="tmdb" campos={[campoSegredo()]} />)

    await user.type(
      screen.getByLabelText('Read Access Token (v4)'),
      'novo-token',
    )
    await user.click(screen.getByRole('button', { name: BOTAO_SALVAR }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    expect(corpoPut(fetchMock)).toEqual({ valores: { token: 'novo-token' } })
  })

  it('clicking Clear on a origem "banco" field sends null in the PUT', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter()
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(
      <IntegrationForm
        slug="tmdb"
        campos={[campoSegredo({ definido: true, origem: 'banco' })]}
      />,
    )

    await user.click(screen.getByRole('button', { name: 'Clear' }))
    await user.click(screen.getByRole('button', { name: BOTAO_SALVAR }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    expect(corpoPut(fetchMock)).toEqual({ valores: { token: null } })
  })

  it('typing after clicking Clear cancels the clear intent (sends the typed value)', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter()
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(
      <IntegrationForm
        slug="tmdb"
        campos={[campoSegredo({ definido: true, origem: 'banco' })]}
      />,
    )

    await user.click(screen.getByRole('button', { name: 'Clear' }))
    await user.type(screen.getByLabelText('Read Access Token (v4)'), 'trocado')
    await user.click(screen.getByRole('button', { name: BOTAO_SALVAR }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    expect(corpoPut(fetchMock)).toEqual({ valores: { token: 'trocado' } })
  })

  it('does not call the PUT when nothing was touched', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter()
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<IntegrationForm slug="tmdb" campos={[campoSegredo()]} />)

    await user.click(screen.getByRole('button', { name: BOTAO_SALVAR }))
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('shows the PUT error inline (422)', async () => {
    const user = userEvent.setup()
    vi.stubGlobal(
      'fetch',
      fetchRouter(() => erroJson(422, 'invalid_body', 'campo desconhecido')),
    )
    renderWithClient(<IntegrationForm slug="tmdb" campos={[campoSegredo()]} />)

    await user.type(screen.getByLabelText('Read Access Token (v4)'), 'x')
    await user.click(screen.getByRole('button', { name: BOTAO_SALVAR }))

    expect(await screen.findByText('campo desconhecido')).toBeInTheDocument()
  })

  it('shows the success message with the restart-the-server warning', async () => {
    const user = userEvent.setup()
    vi.stubGlobal('fetch', fetchRouter())
    renderWithClient(<IntegrationForm slug="tmdb" campos={[campoSegredo()]} />)

    await user.type(screen.getByLabelText('Read Access Token (v4)'), 'x')
    await user.click(screen.getByRole('button', { name: BOTAO_SALVAR }))

    expect(
      await screen.findByText(
        'Saved. Restart the server (docker restart) to apply.',
      ),
    ).toBeInTheDocument()
  })

  it('a required field marks an asterisk in the label', () => {
    vi.stubGlobal('fetch', fetchRouter())
    renderWithClient(
      <IntegrationForm
        slug="tmdb"
        campos={[campoSegredo({ obrigatorio: true })]}
      />,
    )
    expect(screen.getByText('*')).toBeInTheDocument()
  })

  // The contract (openapi.yaml IntegrationField.tipo) supports text/bool/number/
  // select besides secret — no registered integration uses those widgets
  // today (only TMDB, only secret), but `CampoControle` renders them in a
  // generic way. Covered here so the branch does not stay untested.
  it('widget "text": pre-fills with the effective value and edits normally', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter()
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(
      <IntegrationForm
        slug="outra"
        campos={[
          {
            nome: 'host',
            label: 'Host',
            tipo: 'text',
            obrigatorio: false,
            definido: true,
            origem: 'banco',
            valor: 'atual.example.com',
          },
        ]}
      />,
    )
    const input = screen.getByLabelText('Host')
    expect(input).toHaveValue('atual.example.com')
    await user.clear(input)
    await user.type(input, 'novo.example.com')
    expect(input).toHaveValue('novo.example.com')
  })

  it('widget "number": accepts a numeric value', () => {
    vi.stubGlobal('fetch', fetchRouter())
    renderWithClient(
      <IntegrationForm
        slug="outra"
        campos={[
          {
            nome: 'porta',
            label: 'Porta',
            tipo: 'number',
            obrigatorio: false,
            definido: true,
            origem: 'banco',
            valor: '443',
          },
        ]}
      />,
    )
    const input = screen.getByLabelText('Porta')
    expect(input).toHaveAttribute('type', 'number')
    expect(input).toHaveValue(443)
  })

  it('widget "bool": the switch toggles true/false and enters the PUT', async () => {
    const user = userEvent.setup()
    const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
      const u = String(url)
      const method = init?.method ?? 'GET'
      if (u === '/api/v1/integrations/outra' && method === 'PUT') {
        return okJson({
          slug: 'outra',
          nome: 'Outra',
          descricao: '',
          campos: [],
        })
      }
      throw new Error(`rota nao mockada: ${method} ${u}`)
    })
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(
      <IntegrationForm
        slug="outra"
        campos={[
          {
            nome: 'ativo',
            label: 'Ativo',
            tipo: 'bool',
            obrigatorio: false,
            definido: false,
            origem: 'nenhum',
          },
        ]}
      />,
    )
    const switchEl = screen.getByRole('switch', { name: 'Ativo' })
    expect(switchEl).not.toBeChecked()
    await user.click(switchEl)
    expect(switchEl).toBeChecked()

    await user.click(screen.getByRole('button', { name: BOTAO_SALVAR }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    const chamada = fetchMock.mock.calls.find(
      ([url, init]) =>
        String(url) === '/api/v1/integrations/outra' &&
        (init as RequestInit | undefined)?.method === 'PUT',
    )
    expect(JSON.parse(String(chamada?.[1]?.body))).toEqual({
      valores: { ativo: 'true' },
    })
  })

  it('widget "select": lists the options and changes the selected value', async () => {
    const user = userEvent.setup()
    vi.stubGlobal('fetch', fetchRouter())
    renderWithClient(
      <IntegrationForm
        slug="outra"
        campos={[
          {
            nome: 'regiao',
            label: 'Região',
            tipo: 'select',
            obrigatorio: false,
            definido: true,
            origem: 'banco',
            valor: 'us',
            opcoes: [
              { valor: 'us', label: 'EUA' },
              { valor: 'br', label: 'Brasil' },
            ],
          },
        ]}
      />,
    )
    const select = screen.getByLabelText('Região')
    expect(select).toHaveValue('us')
    await user.selectOptions(select, 'br')
    expect(select).toHaveValue('br')
  })
})
