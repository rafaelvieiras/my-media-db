import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { SuggestedMatchWarning } from '@/components/suggested-match-warning'
import type { MatchSuggestion } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson } from '@/test/detail-harness'
import { corpoEnviado, stubRotas } from '@/test/stub-fetch'

// Tests the "did you mean this?" warning: it SHOWS a candidate found by a cheaper variant of
// the name, applies it through the existing confirm-candidate endpoint, or dismisses the
// question. It never applies anything by itself -- that is the point of the feature.

const sugestao: MatchSuggestion = {
  source: 'tmdb',
  source_id: '39340',
  title: 'Duas Garotas em Apuros',
  year: '2011',
  poster_url: 'https://image.tmdb.org/t/p/w342/broken.jpg',
  score: 95,
  query: '2 Brok',
  reason: 'name',
  can_apply: true,
  files_unmatched: 0,
  files_total: 0,
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
  // The two reasons migration 0029 added. They reuse the same box: the difference is the
  // sentence, the numbers behind it, and whether applying makes sense at all.
  it('explains the empty tree without offering to apply the same entry again', () => {
    vi.stubGlobal('fetch', stubAviso())
    renderWithClient(
      <SuggestedMatchWarning
        entityId="s1"
        suggestion={{
          ...sugestao,
          source_id: '14383',
          title: 'Aladdin',
          year: null,
          poster_url: null,
          score: 0,
          query: '',
          reason: 'empty_tree',
          can_apply: false,
        }}
      />,
    )

    expect(
      screen.getByText(/source has no episodes for this item/),
    ).toBeInTheDocument()
    // Applying would re-apply the entry that has no episodes: the action must be gone.
    expect(
      screen.queryByTestId('suggested-match-apply'),
    ).not.toBeInTheDocument()
    expect(screen.getByTestId('suggested-match-dismiss')).toBeInTheDocument()
    expect(screen.getByText(/Fix match/)).toBeInTheDocument()
  })

  it('offers the sibling candidate when the empty entry has a real one', () => {
    vi.stubGlobal('fetch', stubAviso())
    renderWithClient(
      <SuggestedMatchWarning
        entityId="s1"
        suggestion={{
          ...sugestao,
          source_id: '2745',
          title: 'Aladdin: A Série Animada',
          year: '1994',
          reason: 'empty_tree',
          can_apply: true,
        }}
      />,
    )

    expect(screen.getByText('Aladdin: A Série Animada')).toBeInTheDocument()
    expect(screen.getByTestId('suggested-match-apply')).toBeInTheDocument()
  })

  it('shows the census of a tree that does not fit the files', () => {
    vi.stubGlobal('fetch', stubAviso())
    renderWithClient(
      <SuggestedMatchWarning
        entityId="s1"
        suggestion={{
          ...sugestao,
          title: 'Batman',
          year: '2004',
          score: 0,
          query: '',
          reason: 'tree_mismatch',
          can_apply: false,
          files_unmatched: 81,
          files_total: 159,
        }}
      />,
    )

    expect(
      screen.getByText(/This match does not fit the files/),
    ).toBeInTheDocument()
    // The numbers are the evidence: they justify the warning.
    expect(screen.getByText(/81 of 159/)).toBeInTheDocument()
    // The candidate IS the current match -- no apply button, and the line says so.
    expect(screen.getByText(/Matched to/)).toBeInTheDocument()
    expect(
      screen.queryByTestId('suggested-match-apply'),
    ).not.toBeInTheDocument()
  })
})
