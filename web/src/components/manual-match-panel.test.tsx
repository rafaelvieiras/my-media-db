import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ManualMatchPanel } from '@/components/manual-match-panel'
import { renderWithClient } from '@/test/render'
import { erroJson } from '@/test/harness'

// The manual match panel is the owner's escape hatch for a WRONG identity (a match above
// the threshold leaves the entity `cataloged`, so it never reaches the review queue).
// These tests pin the four states of the search and the one write it performs.
// The candidate card links to the item's search page: without a RouterProvider the
// render throws, so the <Link> becomes a plain <a> (same mock as the other suites).
vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock,
}))

vi.mock('@/lib/language-context', () => ({
  useLanguage: () => ({
    lang: 'pt-BR',
    allowed: ['pt-BR'],
    primary: 'pt-BR',
    setLang: () => {},
  }),
}))

const externo = {
  source: 'tmdb',
  source_id: '21510',
  type: 'tv',
  title: 'Crimes do Colarinho Branco',
  year: '2009',
  overview: 'O charmoso vigarista Neal Caffrey.',
  poster_url: null,
  saved: false,
}

function okJson(corpo: unknown) {
  return {
    ok: true,
    status: 200,
    json: async () => corpo,
  } as Response
}

// stub monta o fetch: um GET /search configurável e o POST /correct que o painel dispara.
function stub(opts: {
  search?: () => Response
  correct?: () => Response
  onCorrect?: (corpo: Record<string, string>) => void
}) {
  globalThis.fetch = vi.fn(async (url: string, init?: RequestInit) => {
    const u = new URL(String(url), 'http://localhost')
    const method = init?.method ?? 'GET'
    if (u.pathname === '/api/v1/search' && method === 'GET') {
      return opts.search
        ? opts.search()
        : okJson({
            query: u.searchParams.get('q') ?? '',
            local: [],
            external: [externo],
            external_available: true,
          })
    }
    const corrigir = u.pathname.match(/^\/api\/v1\/entities\/(.+)\/correct$/)
    if (corrigir && method === 'POST') {
      opts.onCorrect?.(
        JSON.parse(String(init?.body ?? '{}')) as Record<string, string>,
      )
      return opts.correct
        ? opts.correct()
        : okJson({ id: corrigir[1], title: 'Crimes do Colarinho Branco' })
    }
    throw new Error(`rota nao mockada: ${method} ${u.pathname}`)
  }) as unknown as typeof fetch
}

describe('ManualMatchPanel — match manual de uma identidade errada', () => {
  beforeEach(() => {
    vi.useRealTimers()
  })
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('busca já preenchida com o título atual e aplica o candidato escolhido', async () => {
    const onApplied = vi.fn()
    let corpoCorrigido: Record<string, string> | undefined
    stub({ onCorrect: (c) => (corpoCorrigido = c) })
    const user = userEvent.setup()
    renderWithClient(
      <ManualMatchPanel
        entityId="e1"
        initialQuery="White Collar"
        onApplied={onApplied}
      />,
    )

    // Prefilled: the owner opens the panel already seeing the wrong title.
    const campo = screen.getByRole('searchbox')
    expect((campo as HTMLInputElement).value).toBe('White Collar')

    // The right candidate is offered; choosing it is the WRITE.
    const usar = await screen.findByRole('button', { name: /Use this/ })
    await user.click(usar)

    await waitFor(() => expect(onApplied).toHaveBeenCalled())
    expect(corpoCorrigido).toEqual({
      source: 'tmdb',
      source_id: '21510',
    })
  })

  it('digitar outro termo refaz a busca (debounce) e limpa o campo zera para a dica', async () => {
    const busca = vi.fn((termo: string) =>
      okJson({
        query: termo,
        local: [],
        external: termo ? [externo] : [],
        external_available: true,
      }),
    )
    globalThis.fetch = vi.fn(async (url: string) => {
      const u = new URL(String(url), 'http://localhost')
      busca(u.searchParams.get('q') ?? '')
      return busca.mock.results.at(-1)?.value ?? okJson({})
    }) as unknown as typeof fetch

    const user = userEvent.setup()
    renderWithClient(<ManualMatchPanel entityId="e1" onApplied={vi.fn()} />)

    // Empty query: only the hint, no request.
    expect(screen.getByText(/Type the correct title/i)).toBeInTheDocument()

    await user.type(screen.getByRole('searchbox'), 'White Collar')
    await screen.findByRole('button', { name: /Use this/ })

    await user.clear(screen.getByRole('searchbox'))
    await waitFor(() =>
      expect(screen.getByText(/Type the correct title/i)).toBeInTheDocument(),
    )
  })

  it('mostra o erro do backend quando a correção é recusada', async () => {
    stub({
      correct: () =>
        erroJson(422, 'invalid', 'identidade já usada por outro item'),
    })
    const onApplied = vi.fn()
    const user = userEvent.setup()
    renderWithClient(
      <ManualMatchPanel
        entityId="e1"
        initialQuery="White Collar"
        onApplied={onApplied}
      />,
    )

    await user.click(await screen.findByRole('button', { name: /Use this/ }))

    const alerta = await screen.findByRole('alert')
    expect(alerta).toHaveTextContent('identidade já usada por outro item')
    expect(onApplied).not.toHaveBeenCalled()
  })

  it('fonte externa indisponível tem mensagem própria', async () => {
    stub({
      search: () =>
        okJson({
          query: 'White Collar',
          local: [],
          external: [],
          external_available: false,
        }),
    })
    renderWithClient(
      <ManualMatchPanel
        entityId="e1"
        initialQuery="White Collar"
        onApplied={vi.fn()}
      />,
    )
    expect(
      await screen.findByText(/External search unavailable/i),
    ).toBeInTheDocument()
  })

  it('busca sem resultado diz que não há resultado', async () => {
    stub({
      search: () =>
        okJson({
          query: 'zzz',
          local: [],
          external: [],
          external_available: true,
        }),
    })
    renderWithClient(
      <ManualMatchPanel entityId="e1" initialQuery="zzz" onApplied={vi.fn()} />,
    )
    expect(await screen.findByText(/No external results/i)).toBeInTheDocument()
  })

  it('falha da busca mostra o erro, não a lista', async () => {
    stub({ search: () => erroJson(500, 'internal', 'falhou') })
    renderWithClient(
      <ManualMatchPanel
        entityId="e1"
        initialQuery="White Collar"
        onApplied={vi.fn()}
      />,
    )
    expect(await screen.findByText(/Could not search/i)).toBeInTheDocument()
  })
})
