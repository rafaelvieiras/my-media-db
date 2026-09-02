import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { IntegrationForm } from '@/components/integration-form'
import type { IntegrationField } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson } from '@/test/detail-harness'

// IntegrationForm (briefing 0096): widget novo `secret` sobre o vocabulário
// de `integration.Campo` (0095) — nunca reidrata do servidor, legenda por
// `origem`, botão "Limpar" só quando `origem==="banco"`, PUT diferencial
// (só campos tocados/limpos entram em `valores`).

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

afterEach(() => {
  vi.restoreAllMocks()
})

describe('IntegrationForm', () => {
  it('campo secret nunca pré-preenche, mesmo definido:true/origem:"banco"', () => {
    vi.stubGlobal('fetch', fetchRouter())
    renderWithClient(
      <IntegrationForm
        slug="tmdb"
        campos={[campoSegredo({ definido: true, origem: 'banco' })]}
      />,
    )
    expect(screen.getByLabelText('Read Access Token (v4)')).toHaveValue('')
  })

  it('origem "banco": legenda "Definido no painel." + botão Limpar', () => {
    vi.stubGlobal('fetch', fetchRouter())
    renderWithClient(
      <IntegrationForm
        slug="tmdb"
        campos={[campoSegredo({ definido: true, origem: 'banco' })]}
      />,
    )
    expect(screen.getByText('Definido no painel.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Limpar' })).toBeInTheDocument()
  })

  it('origem "env" com hint conhecido (tmdb/token) nomeia a variável exata', () => {
    vi.stubGlobal('fetch', fetchRouter())
    renderWithClient(
      <IntegrationForm
        slug="tmdb"
        campos={[campoSegredo({ definido: true, origem: 'env' })]}
      />,
    )
    expect(
      screen.getByText(
        'Definido pela variável de ambiente MYMEDIADB_TMDB_TOKEN — salve aqui para sobrepor.',
      ),
    ).toBeInTheDocument()
    // origem "env" não mostra o botão Limpar (não há override no banco pra remover).
    expect(
      screen.queryByRole('button', { name: 'Limpar' }),
    ).not.toBeInTheDocument()
  })

  it('origem "env" sem hint conhecido cai no texto genérico (sem inventar nome de variável)', () => {
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
        'Definido por uma variável de ambiente do servidor — salve aqui para sobrepor.',
      ),
    ).toBeInTheDocument()
  })

  it('origem "nenhum": sem legenda e sem botão Limpar', () => {
    vi.stubGlobal('fetch', fetchRouter())
    renderWithClient(<IntegrationForm slug="tmdb" campos={[campoSegredo()]} />)
    expect(screen.queryByText(/Definido/)).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Limpar' }),
    ).not.toBeInTheDocument()
  })

  it('toggle mostrar/ocultar alterna o type do input', async () => {
    const user = userEvent.setup()
    vi.stubGlobal('fetch', fetchRouter())
    renderWithClient(<IntegrationForm slug="tmdb" campos={[campoSegredo()]} />)

    const campo = screen.getByLabelText('Read Access Token (v4)')
    expect(campo).toHaveAttribute('type', 'password')

    await user.click(
      screen.getByRole('button', { name: 'Mostrar Read Access Token (v4)' }),
    )
    expect(campo).toHaveAttribute('type', 'text')

    await user.click(
      screen.getByRole('button', { name: 'Ocultar Read Access Token (v4)' }),
    )
    expect(campo).toHaveAttribute('type', 'password')
  })

  it('digitar um valor novo e salvar manda SÓ esse campo no PUT', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter()
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<IntegrationForm slug="tmdb" campos={[campoSegredo()]} />)

    await user.type(
      screen.getByLabelText('Read Access Token (v4)'),
      'novo-token',
    )
    await user.click(screen.getByRole('button', { name: 'Salvar' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    expect(corpoPut(fetchMock)).toEqual({ valores: { token: 'novo-token' } })
  })

  it('clicar Limpar num campo origem "banco" manda null no PUT', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter()
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(
      <IntegrationForm
        slug="tmdb"
        campos={[campoSegredo({ definido: true, origem: 'banco' })]}
      />,
    )

    await user.click(screen.getByRole('button', { name: 'Limpar' }))
    await user.click(screen.getByRole('button', { name: 'Salvar' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    expect(corpoPut(fetchMock)).toEqual({ valores: { token: null } })
  })

  it('digitar depois de clicar Limpar cancela a intenção de limpar (manda o valor digitado)', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter()
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(
      <IntegrationForm
        slug="tmdb"
        campos={[campoSegredo({ definido: true, origem: 'banco' })]}
      />,
    )

    await user.click(screen.getByRole('button', { name: 'Limpar' }))
    await user.type(screen.getByLabelText('Read Access Token (v4)'), 'trocado')
    await user.click(screen.getByRole('button', { name: 'Salvar' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    expect(corpoPut(fetchMock)).toEqual({ valores: { token: 'trocado' } })
  })

  it('não chama o PUT quando nada foi tocado', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter()
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<IntegrationForm slug="tmdb" campos={[campoSegredo()]} />)

    await user.click(screen.getByRole('button', { name: 'Salvar' }))
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('mostra o erro do PUT inline (422)', async () => {
    const user = userEvent.setup()
    vi.stubGlobal(
      'fetch',
      fetchRouter(() => erroJson(422, 'invalid_body', 'campo desconhecido')),
    )
    renderWithClient(<IntegrationForm slug="tmdb" campos={[campoSegredo()]} />)

    await user.type(screen.getByLabelText('Read Access Token (v4)'), 'x')
    await user.click(screen.getByRole('button', { name: 'Salvar' }))

    expect(await screen.findByText('campo desconhecido')).toBeInTheDocument()
  })

  it('mostra a mensagem de sucesso com o aviso de reiniciar o servidor', async () => {
    const user = userEvent.setup()
    vi.stubGlobal('fetch', fetchRouter())
    renderWithClient(<IntegrationForm slug="tmdb" campos={[campoSegredo()]} />)

    await user.type(screen.getByLabelText('Read Access Token (v4)'), 'x')
    await user.click(screen.getByRole('button', { name: 'Salvar' }))

    expect(
      await screen.findByText(
        'Salvo. Reinicie o servidor (docker restart) para aplicar.',
      ),
    ).toBeInTheDocument()
  })

  it('campo obrigatório marca asterisco no rótulo', () => {
    vi.stubGlobal('fetch', fetchRouter())
    renderWithClient(
      <IntegrationForm
        slug="tmdb"
        campos={[campoSegredo({ obrigatorio: true })]}
      />,
    )
    expect(screen.getByText('*')).toBeInTheDocument()
  })

  // O contrato (openapi.yaml IntegrationField.tipo) suporta text/bool/number/
  // select além de secret — nenhuma integração registrada usa esses widgets
  // hoje (só TMDB, só secret), mas o `CampoControle` os renderiza de forma
  // genérica. Cobertos aqui para não deixar a ramificação sem teste.
  it('widget "text": pré-preenche com o valor efetivo e edita normalmente', async () => {
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

  it('widget "number": aceita valor numérico', () => {
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

  it('widget "bool": switch alterna true/false e entra no PUT', async () => {
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

    await user.click(screen.getByRole('button', { name: 'Salvar' }))
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

  it('widget "select": lista as opções e muda o valor selecionado', async () => {
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
