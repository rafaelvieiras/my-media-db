import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {
  CorrectMatchResultCard,
  ExternalResultCard,
} from '@/components/external-result-card'
import type { ExternalResult } from '@/lib/api'
import { renderWithClient } from '@/test/render'

// Link (shared helper) interpolates the `params` into the `href` — to assert that
// the card points to the external detail screen. `useNavigate` is a spy (only used
// after import, not exercised here).
vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock,
  useNavigate: () => vi.fn(),
}))

const externo: ExternalResult = {
  source: 'tmdb',
  source_id: '603',
  type: 'movie',
  title: 'The Matrix',
  year: '1999',
  overview: 'Um hacker descobre a verdade.',
  poster_url: null,
  saved: false,
}

afterEach(() => vi.restoreAllMocks())

describe('ExternalResultCard', () => {
  it('links the poster/title to the external detail screen', () => {
    renderWithClient(<ExternalResultCard result={externo} />)

    const link = screen.getByRole('link', { name: /The Matrix/ })
    expect(link.getAttribute('href')).toBe('/external/tmdb/movie/603')
  })

  it('keeps the Import button as a shortcut outside the anchor', () => {
    renderWithClient(<ExternalResultCard result={externo} />)

    // The button exists and is not nested in the link (an anchor does not contain the button).
    const botao = screen.getByRole('button', { name: /Import/ })
    const link = screen.getByRole('link', { name: /The Matrix/ })
    expect(link.contains(botao)).toBe(false)
  })

  it('with internalId it links to the internal one, shows "In the library" and drops Import', () => {
    // Internal-first: the item is already an entity — link to /entities/{id}, badge and
    // NO Import (it does not reimport what is already cataloged).
    renderWithClient(<ExternalResultCard result={externo} internalId="ent-9" />)

    const link = screen.getByRole('link', { name: /The Matrix/ })
    expect(link.getAttribute('href')).toBe('/entities/ent-9')
    expect(screen.getByText('In the library')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /Import/ })).toBeNull()
  })
})

describe('CorrectMatchResultCard (0074)', () => {
  it('shows poster/synopsis (same core as ExternalResultCard) and calls onEscolher with the result', async () => {
    const user = userEvent.setup()
    const onEscolher = vi.fn()
    renderWithClient(
      <CorrectMatchResultCard result={externo} onEscolher={onEscolher} />,
    )

    expect(screen.getByText('Um hacker descobre a verdade.')).toBeTruthy()
    await user.click(screen.getByRole('button', { name: 'Use this' }))
    expect(onEscolher).toHaveBeenCalledWith(externo)
  })

  it('with internalId it shows the "In the library" badge ALONGSIDE "Use this" (the action stays, unlike Import)', () => {
    // Duplicate merge case: the result already is an internal entity, but the
    // correction still needs to be able to pick it.
    renderWithClient(
      <CorrectMatchResultCard
        result={externo}
        internalId="ent-9"
        onEscolher={() => {}}
      />,
    )

    expect(screen.getByText('In the library')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Use this' })).toBeInTheDocument()
  })
})
