import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { WebhookScreen } from '@/routes/webhook'
import type { JellyfinWebhookToken } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson } from '@/test/detail-harness'

// Routes the fetch of the Jellyfin webhook token. An absent `tokenInicial`
// simulates the state "no token generated yet" (GET 404 — a normal state
// on the first visit, not an error). It keeps the current token in a
// local variable: a successful POST has to reflect in the next GET
// (the UI invalidates the query and refetches after generating/regenerating).
function fetchRouter(opts?: {
  tokenInicial?: string
  getErro?: { status: number; code: string; message: string }
  postResposta?: JellyfinWebhookToken
}) {
  let tokenVigente = opts?.tokenInicial
  return vi.fn(async (url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'
    if (u === '/api/v1/webhooks/jellyfin/token' && method === 'GET') {
      if (opts?.getErro) {
        return erroJson(
          opts.getErro.status,
          opts.getErro.code,
          opts.getErro.message,
        )
      }
      if (tokenVigente === undefined) {
        return { ok: false, status: 404, json: async () => ({}) } as Response
      }
      return okJson({ token: tokenVigente })
    }
    if (u === '/api/v1/webhooks/jellyfin/token' && method === 'POST') {
      const resposta = opts?.postResposta ?? { token: 'token-novo-gerado' }
      tokenVigente = resposta.token
      return okJson(resposta)
    }
    throw new Error(`rota nao mockada: ${method} ${u}`)
  })
}

// `userEvent.setup()` resets `navigator.clipboard` to the real implementation
// of jsdom — so the mock has to be installed AFTER setup(), not in a
// global `beforeEach` (otherwise the setup() of `userEvent` itself overwrites the
// mock before the click happens).
function mockarClipboard() {
  const writeText = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText },
    writable: true,
    configurable: true,
  })
  return writeText
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('WebhookScreen', () => {
  it('mostra a seção de webhooks de saída como placeholder, sem chamada de API', async () => {
    const fetchMock = fetchRouter()
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<WebhookScreen />)

    expect(
      await screen.findByRole('heading', { name: 'Webhooks de saída' }),
    ).toBeInTheDocument()
    expect(
      screen.getByText('Nenhum webhook de saída disponível ainda.'),
    ).toBeInTheDocument()
    expect(
      fetchMock.mock.calls.every(
        ([url]) => String(url) === '/api/v1/webhooks/jellyfin/token',
      ),
    ).toBe(true)
  })

  it('estado vazio (404): mostra "nenhum token" e gera um novo ao clicar', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({
      postResposta: { token: 'token-recem-gerado' },
    })
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<WebhookScreen />)

    expect(
      await screen.findByText('Nenhum token gerado ainda.'),
    ).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Gerar token' }))

    await waitFor(() =>
      expect(screen.getByLabelText('Token do webhook')).toHaveValue(
        'token-recem-gerado',
      ),
    )
    const postCall = fetchMock.mock.calls.find(
      ([url, init]) =>
        String(url) === '/api/v1/webhooks/jellyfin/token' &&
        (init as RequestInit | undefined)?.method === 'POST',
    )
    expect(postCall).toBeDefined()
  })

  it('token existente: campo mascarado por padrão, toggle mostra/oculta', async () => {
    const user = userEvent.setup()
    vi.stubGlobal('fetch', fetchRouter({ tokenInicial: 'abc123token' }))
    renderWithClient(<WebhookScreen />)

    const campo = await screen.findByLabelText('Token do webhook')
    expect(campo).toHaveAttribute('type', 'password')
    expect(campo).toHaveValue('abc123token')

    await user.click(screen.getByRole('button', { name: 'Mostrar token' }))
    expect(campo).toHaveAttribute('type', 'text')

    await user.click(screen.getByRole('button', { name: 'Ocultar token' }))
    expect(campo).toHaveAttribute('type', 'password')
  })

  it('copia o token e a URL do webhook para a área de transferência', async () => {
    const user = userEvent.setup()
    const writeText = mockarClipboard()
    vi.stubGlobal('fetch', fetchRouter({ tokenInicial: 'abc123token' }))
    renderWithClient(<WebhookScreen />)
    await screen.findByLabelText('Token do webhook')

    await user.click(screen.getByRole('button', { name: 'Copiar token' }))
    expect(writeText).toHaveBeenCalledWith('abc123token')
    expect(
      await screen.findByRole('button', { name: 'Copiado!' }),
    ).toBeInTheDocument()

    await user.click(
      screen.getByRole('button', { name: 'Copiar URL do webhook' }),
    )
    expect(writeText).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/webhooks/jellyfin?token=abc123token'),
    )
  })

  it('regenerar pede confirmação; cancelar não chama o POST', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({ tokenInicial: 'abc123token' })
    vi.stubGlobal('fetch', fetchMock)
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    renderWithClient(<WebhookScreen />)
    await screen.findByLabelText('Token do webhook')

    await user.click(screen.getByRole('button', { name: 'Gerar novo token' }))

    expect(window.confirm).toHaveBeenCalled()
    expect(
      fetchMock.mock.calls.some(
        ([url, init]) =>
          String(url) === '/api/v1/webhooks/jellyfin/token' &&
          (init as RequestInit | undefined)?.method === 'POST',
      ),
    ).toBe(false)
  })

  it('regenerar confirmado: substitui o token exibido', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({
      tokenInicial: 'token-antigo',
      postResposta: { token: 'token-substituto' },
    })
    vi.stubGlobal('fetch', fetchMock)
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    renderWithClient(<WebhookScreen />)
    await screen.findByLabelText('Token do webhook')

    await user.click(screen.getByRole('button', { name: 'Gerar novo token' }))

    await waitFor(() =>
      expect(screen.getByLabelText('Token do webhook')).toHaveValue(
        'token-substituto',
      ),
    )
  })

  it('estado de erro real (não-404) mostra ErrorState', async () => {
    vi.stubGlobal(
      'fetch',
      fetchRouter({
        getErro: { status: 500, code: 'erro_interno', message: 'falhou' },
      }),
    )
    renderWithClient(<WebhookScreen />)

    expect(
      await screen.findByText('Não foi possível carregar o token do webhook'),
    ).toBeInTheDocument()
  })
})

// Ensures that the main heading of the screen is also present (structure smoke test,
// useful to catch import/broken-route regressions).
it('renderiza o heading "Webhook"', async () => {
  vi.stubGlobal('fetch', fetchRouter())
  renderWithClient(<WebhookScreen />)
  const heading = await screen.findByRole('heading', { name: 'Webhook' })
  expect(
    within(heading.closest('main')!).getByText(/Webhook de entrada/),
  ).toBeInTheDocument()
})
