import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { SuggestedMatchWarning } from '@/components/suggested-match-warning'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson } from '@/test/detail-harness'
import { corpoEnviado, stubRotas } from '@/test/stub-fetch'

// Tests the "did you mean this?" warning: it SHOWS a candidate found by a cheaper variant of
// the name, applies it through the existing confirm-candidate endpoint, or dismisses the
// question. It never applies anything by itself -- that is the point of the feature.

const sugestao = {
  source: 'tmdb',
  source_id: '39340',
  title: 'Duas Garotas em Apuros',
  year: '2011',
  poster_url: 'https://image.tmdb.org/t/p/w342/broken.jpg',
  score: 95,
  query: '2 Brok',
}

// stubAviso installs the two routes the warning talks to (apply + dismiss).
function stubAviso(
  respostaConfirmar?: () => Response,
  respostaDescartar?: () => Response,
) {
  return stubRotas([
    [
      (p, m) => p === '/api/v1/entities/s1/confirm-candidate' && m === 'POST',
      respostaConfirmar ?? (() => okJson({ id: 's1', type: 'tv', title: 'x' })),
    ],
    [
      (p, m) => p === '/api/v1/entities/s1/match-suggestion' && m === 'DELETE',
      respostaDescartar ?? (() => new Response(null, { status: 204 })),
    ],
  ])
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('SuggestedMatchWarning', () => {
  it('shows the candidate with its poster, year and the variant that found it', () => {
    vi.stubGlobal('fetch', stubAviso())
    renderWithClient(
      <SuggestedMatchWarning entityId="s1" suggestion={sugestao} />,
    )

    expect(screen.getByTestId('suggested-match-warning')).toBeInTheDocument()
    expect(screen.getByText('Duas Garotas em Apuros')).toBeInTheDocument()
    expect(screen.getByText(/2011/)).toBeInTheDocument()
    expect(screen.getByTestId('suggested-match-poster')).toHaveAttribute(
      'src',
      sugestao.poster_url,
    )
    // The reason names the query that found it: the guess explains itself.
    expect(screen.getByText(/2 Brok/)).toBeInTheDocument()
  })

  it('applies the candidate through confirm-candidate, without any threshold', async () => {
    const fetchMock = stubAviso()
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(
      <SuggestedMatchWarning entityId="s1" suggestion={sugestao} />,
    )

    await userEvent.click(screen.getByTestId('suggested-match-apply'))

    await waitFor(() => {
      const indice = fetchMock.mock.calls.findIndex(
        ([url, init]) =>
          String(url).endsWith('/api/v1/entities/s1/confirm-candidate') &&
          (init as RequestInit | undefined)?.method === 'POST',
      )
      expect(indice).toBeGreaterThanOrEqual(0)
      expect(corpoEnviado(fetchMock, indice)).toEqual({
        method: 'external',
        source: 'tmdb',
        source_id: '39340',
      })
    })
  })

  it('dismisses the hint for good', async () => {
    const fetchMock = stubAviso()
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(
      <SuggestedMatchWarning entityId="s1" suggestion={sugestao} />,
    )

    await userEvent.click(screen.getByTestId('suggested-match-dismiss'))

    await waitFor(() => {
      const chamada = fetchMock.mock.calls.find(
        ([url, init]) =>
          String(url).endsWith('/api/v1/entities/s1/match-suggestion') &&
          (init as RequestInit | undefined)?.method === 'DELETE',
      )
      expect(chamada).toBeTruthy()
    })
  })

  it('shows an error when the source refuses the candidate, and keeps the hint', async () => {
    vi.stubGlobal(
      'fetch',
      stubAviso(() =>
        erroJson(409, 'conflict', 'identidade já pertence a outro item'),
      ),
    )
    renderWithClient(
      <SuggestedMatchWarning entityId="s1" suggestion={sugestao} />,
    )

    await userEvent.click(screen.getByTestId('suggested-match-apply'))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      /Could not apply the suggestion/,
    )
    expect(screen.getByTestId('suggested-match-warning')).toBeInTheDocument()
  })

  it('renders a placeholder when the candidate has no poster', () => {
    vi.stubGlobal('fetch', stubAviso())
    renderWithClient(
      <SuggestedMatchWarning
        entityId="s1"
        suggestion={{ ...sugestao, poster_url: null, year: null }}
      />,
    )

    expect(
      screen.queryByTestId('suggested-match-poster'),
    ).not.toBeInTheDocument()
    expect(screen.getByTestId('suggested-match-warning')).toBeInTheDocument()
  })
})
