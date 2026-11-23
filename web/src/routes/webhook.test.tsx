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
  it('shows the outbound webhooks section as a placeholder, without an API call', async () => {
    const fetchMock = fetchRouter()
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<WebhookScreen />)

    expect(
      await screen.findByRole('heading', { name: 'Outbound webhooks' }),
    ).toBeInTheDocument()
    expect(
      screen.getByText('No outbound webhook available yet.'),
    ).toBeInTheDocument()
    expect(
      fetchMock.mock.calls.every(
        ([url]) => String(url) === '/api/v1/webhooks/jellyfin/token',
      ),
    ).toBe(true)
  })

  it('empty state (404): shows "no token" and generates one on click', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({
      postResposta: { token: 'token-recem-gerado' },
    })
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<WebhookScreen />)

    expect(
      await screen.findByText('No token generated yet.'),
    ).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Generate token' }))

    await waitFor(() =>
      expect(screen.getByLabelText('Webhook token')).toHaveValue(
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

  it('existing token: field masked by default, toggle shows/hides', async () => {
    const user = userEvent.setup()
    vi.stubGlobal('fetch', fetchRouter({ tokenInicial: 'abc123token' }))
    renderWithClient(<WebhookScreen />)

    const campo = await screen.findByLabelText('Webhook token')
    expect(campo).toHaveAttribute('type', 'password')
    expect(campo).toHaveValue('abc123token')

    await user.click(screen.getByRole('button', { name: 'Show token' }))
    expect(campo).toHaveAttribute('type', 'text')

    await user.click(screen.getByRole('button', { name: 'Hide token' }))
    expect(campo).toHaveAttribute('type', 'password')
  })

  it('copies the token and the webhook URL to the clipboard', async () => {
    const user = userEvent.setup()
    const writeText = mockarClipboard()
    vi.stubGlobal('fetch', fetchRouter({ tokenInicial: 'abc123token' }))
    renderWithClient(<WebhookScreen />)
    await screen.findByLabelText('Webhook token')

    await user.click(screen.getByRole('button', { name: 'Copy token' }))
    expect(writeText).toHaveBeenCalledWith('abc123token')
    expect(
      await screen.findByRole('button', { name: 'Copied!' }),
    ).toBeInTheDocument()
    // The announcement lives in a live region NEXT TO the button: a `role="status"`
    // inside it would be skipped by the accessible name computation (Chrome drops the
    // name, the E2E hit it) — the button keeps its name, the region says "Copied!".
    expect(screen.getAllByRole('status').map((r) => r.textContent)).toContain(
      'Copied!',
    )

    await user.click(screen.getByRole('button', { name: 'Copy webhook URL' }))
    expect(writeText).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/webhooks/jellyfin?token=abc123token'),
    )
  })

  it('regenerate asks for confirmation; cancelling does not call the POST', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({ tokenInicial: 'abc123token' })
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<WebhookScreen />)
    await screen.findByLabelText('Webhook token')

    // The confirmation is an AlertDialog (a `window.confirm` can be suppressed by
    // the browser and was invisible to the screen reader).
    await user.click(screen.getByRole('button', { name: 'Generate new token' }))
    expect(screen.getByRole('alertdialog')).toBeTruthy()

    await user.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(
      fetchMock.mock.calls.some(
        ([url, init]) =>
          String(url) === '/api/v1/webhooks/jellyfin/token' &&
          (init as RequestInit | undefined)?.method === 'POST',
      ),
    ).toBe(false)
  })

  it('confirmed regenerate: replaces the displayed token', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({
      tokenInicial: 'token-antigo',
      postResposta: { token: 'token-substituto' },
    })
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<WebhookScreen />)
    await screen.findByLabelText('Webhook token')

    await user.click(screen.getByRole('button', { name: 'Generate new token' }))
    const dialogo = screen.getByRole('alertdialog')
    await user.click(
      within(dialogo).getByRole('button', { name: 'Regenerate token' }),
    )

    await waitFor(() =>
      expect(screen.getByLabelText('Webhook token')).toHaveValue(
        'token-substituto',
      ),
    )
  })

  it('real error state (non-404) shows the ErrorState', async () => {
    vi.stubGlobal(
      'fetch',
      fetchRouter({
        getErro: { status: 500, code: 'erro_interno', message: 'falhou' },
      }),
    )
    renderWithClient(<WebhookScreen />)

    expect(
      await screen.findByText('Could not load the webhook token'),
    ).toBeInTheDocument()
  })
})

// Ensures that the main heading of the screen is also present (structure smoke test,
// useful to catch import/broken-route regressions).
it('renders the "Webhook" heading', async () => {
  vi.stubGlobal('fetch', fetchRouter())
  renderWithClient(<WebhookScreen />)
  const heading = await screen.findByRole('heading', { name: 'Webhook' })
  expect(
    within(heading.closest('main')!).getByText(/Inbound webhook/),
  ).toBeInTheDocument()
})
