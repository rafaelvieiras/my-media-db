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

type FetchMock = ReturnType<typeof fetchRouter>
type Usuario = ReturnType<typeof userEvent.setup>

// fetchRouter serves the PUT of the integration `slug` with `putResposta` (or an
// empty success); any other route fails loudly.
function fetchRouter(putResposta?: () => Response, slug = 'tmdb') {
  return vi.fn(async (url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'
    if (u === `/api/v1/integrations/${slug}` && method === 'PUT') {
      return putResposta
        ? putResposta()
        : okJson({ slug, nome: slug, descricao: '', campos: [] })
    }
    throw new Error(`rota nao mockada: ${method} ${u}`)
  })
}

// montarFormulario is the arrange step that every submission test repeats: it
// installs the fetch mock and renders the form over ONE field.
function montarFormulario({
  campo = campoSegredo(),
  slug = 'tmdb',
  responder,
}: {
  campo?: IntegrationField
  slug?: string
  responder?: () => Response
} = {}) {
  const user = userEvent.setup()
  const fetchMock = fetchRouter(responder, slug)
  vi.stubGlobal('fetch', fetchMock)
  renderWithClient(<IntegrationForm slug={slug} campos={[campo]} />)
  return { user, fetchMock }
}

// montarComCampoDefinido renders the form over a secret already saved in the panel
// (origem "banco") — the state in which the "Clear" button exists.
function montarComCampoDefinido() {
  return montarFormulario({
    campo: campoSegredo({ definido: true, origem: 'banco' }),
  })
}

// corpoDoPut returns the `valores` body of the PUT already sent for `slug`.
function corpoDoPut(fetchMock: FetchMock, slug = 'tmdb') {
  const url = `/api/v1/integrations/${slug}`
  const chamada = fetchMock.mock.calls.find(
    ([u, init]) =>
      String(u) === url && (init as RequestInit | undefined)?.method === 'PUT',
  )
  return JSON.parse(String(chamada?.[1]?.body)) as {
    valores: Record<string, string | null>
  }
}

// salvar clicks Save, waits for the PUT and returns the body that went out.
async function salvar(
  user: Usuario,
  fetchMock: FetchMock,
  slug = 'tmdb',
): Promise<{ valores: Record<string, string | null> }> {
  await user.click(screen.getByRole('button', { name: BOTAO_SALVAR }))
  await waitFor(() => expect(fetchMock).toHaveBeenCalled())
  return corpoDoPut(fetchMock, slug)
}

// `FormSaveFooter` is a shared component outside this domain (its own extraction
// lands in parallel), so the submit label is accepted in either language.
const BOTAO_SALVAR = /^(Save|Salvar)$/

afterEach(() => {
  vi.restoreAllMocks()
})

describe('IntegrationForm', () => {
  it('the secret field never pre-fills, even with definido:true/origem:"banco"', () => {
    montarComCampoDefinido()
    expect(screen.getByLabelText('Read Access Token (v4)')).toHaveValue('')
  })

  it('origem "banco": "Set in the panel." caption + Clear button', () => {
    montarComCampoDefinido()
    expect(screen.getByText('Set in the panel.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Clear' })).toBeInTheDocument()
  })

  it('origem "env" with a known hint (tmdb/token) names the exact variable', () => {
    montarFormulario({ campo: campoSegredo({ definido: true, origem: 'env' }) })
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
    montarFormulario({
      slug: 'outra-integracao',
      campo: campoSegredo({
        nome: 'chave',
        label: 'Chave',
        definido: true,
        origem: 'env',
      }),
    })
    expect(
      screen.getByText(
        'Set by a server environment variable — save here to override.',
      ),
    ).toBeInTheDocument()
  })

  it('origem "nenhum": no caption and no Clear button', () => {
    montarFormulario()
    expect(screen.queryByText(/^Set (in|by) /)).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Clear' }),
    ).not.toBeInTheDocument()
  })

  it('the show/hide toggle switches the input type', async () => {
    const { user } = montarFormulario()

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
    const { user, fetchMock } = montarFormulario()

    await user.type(
      screen.getByLabelText('Read Access Token (v4)'),
      'novo-token',
    )

    expect(await salvar(user, fetchMock)).toEqual({
      valores: { token: 'novo-token' },
    })
  })

  it('clicking Clear on a origem "banco" field sends null in the PUT', async () => {
    const { user, fetchMock } = montarComCampoDefinido()

    await user.click(screen.getByRole('button', { name: 'Clear' }))

    expect(await salvar(user, fetchMock)).toEqual({ valores: { token: null } })
  })

  it('typing after clicking Clear cancels the clear intent (sends the typed value)', async () => {
    const { user, fetchMock } = montarComCampoDefinido()

    await user.click(screen.getByRole('button', { name: 'Clear' }))
    await user.type(screen.getByLabelText('Read Access Token (v4)'), 'trocado')

    expect(await salvar(user, fetchMock)).toEqual({
      valores: { token: 'trocado' },
    })
  })

  it('does not call the PUT when nothing was touched', async () => {
    const { user, fetchMock } = montarFormulario()

    await user.click(screen.getByRole('button', { name: BOTAO_SALVAR }))
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('shows the PUT error inline (422)', async () => {
    const { user } = montarFormulario({
      responder: () => erroJson(422, 'invalid_body', 'campo desconhecido'),
    })

    await user.type(screen.getByLabelText('Read Access Token (v4)'), 'x')
    await user.click(screen.getByRole('button', { name: BOTAO_SALVAR }))

    expect(await screen.findByText('campo desconhecido')).toBeInTheDocument()
  })

  it('shows the success message with the restart-the-server warning', async () => {
    const { user } = montarFormulario()

    await user.type(screen.getByLabelText('Read Access Token (v4)'), 'x')
    await user.click(screen.getByRole('button', { name: BOTAO_SALVAR }))

    expect(
      await screen.findByText(
        'Saved. Restart the server (docker restart) to apply.',
      ),
    ).toBeInTheDocument()
  })

  it('a required field marks an asterisk in the label', () => {
    montarFormulario({ campo: campoSegredo({ obrigatorio: true }) })
    expect(screen.getByText('*')).toBeInTheDocument()
  })

  // The contract (openapi.yaml IntegrationField.tipo) supports text/bool/number/
  // select besides secret — no registered integration uses those widgets
  // today (only TMDB, only secret), but `CampoControle` renders them in a
  // generic way. Covered here so the branch does not stay untested.
  it('widget "text": pre-fills with the effective value and edits normally', async () => {
    const { user } = montarFormulario({
      slug: 'outra',
      campo: {
        nome: 'host',
        label: 'Host',
        tipo: 'text',
        obrigatorio: false,
        definido: true,
        origem: 'banco',
        valor: 'atual.example.com',
      },
    })
    const input = screen.getByLabelText('Host')
    expect(input).toHaveValue('atual.example.com')
    await user.clear(input)
    await user.type(input, 'novo.example.com')
    expect(input).toHaveValue('novo.example.com')
  })

  it('widget "number": accepts a numeric value', () => {
    montarFormulario({
      slug: 'outra',
      campo: {
        nome: 'porta',
        label: 'Porta',
        tipo: 'number',
        obrigatorio: false,
        definido: true,
        origem: 'banco',
        valor: '443',
      },
    })
    const input = screen.getByLabelText('Porta')
    expect(input).toHaveAttribute('type', 'number')
    expect(input).toHaveValue(443)
  })

  it('widget "bool": the switch toggles true/false and enters the PUT', async () => {
    const { user, fetchMock } = montarFormulario({
      slug: 'outra',
      campo: {
        nome: 'ativo',
        label: 'Ativo',
        tipo: 'bool',
        obrigatorio: false,
        definido: false,
        origem: 'nenhum',
      },
    })
    const switchEl = screen.getByRole('switch', { name: 'Ativo' })
    expect(switchEl).not.toBeChecked()
    await user.click(switchEl)
    expect(switchEl).toBeChecked()

    expect(await salvar(user, fetchMock, 'outra')).toEqual({
      valores: { ativo: 'true' },
    })
  })

  it('widget "select": lists the options and changes the selected value', async () => {
    const { user } = montarFormulario({
      slug: 'outra',
      campo: {
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
    })
    const select = screen.getByLabelText('Região')
    expect(select).toHaveValue('us')
    await user.selectOptions(select, 'br')
    expect(select).toHaveValue('br')
  })
})
