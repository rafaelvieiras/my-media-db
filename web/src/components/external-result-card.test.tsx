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
  it('linka o pôster/título para a tela de detalhe do externo', () => {
    renderWithClient(<ExternalResultCard result={externo} />)

    const link = screen.getByRole('link', { name: /The Matrix/ })
    expect(link.getAttribute('href')).toBe('/external/tmdb/movie/603')
  })

  it('mantém o botão Importar como atalho fora da âncora', () => {
    renderWithClient(<ExternalResultCard result={externo} />)

    // The button exists and is not nested in the link (an anchor does not contain the button).
    const botao = screen.getByRole('button', { name: /Importar/ })
    const link = screen.getByRole('link', { name: /The Matrix/ })
    expect(link.contains(botao)).toBe(false)
  })

  it('com internalId linka para o interno, marca "Na biblioteca" e some o Importar', () => {
    // Internal-first: the item is already an entity — link to /entities/{id}, badge and
    // NO Importar (it does not reimport what is already cataloged).
    renderWithClient(<ExternalResultCard result={externo} internalId="ent-9" />)

    const link = screen.getByRole('link', { name: /The Matrix/ })
    expect(link.getAttribute('href')).toBe('/entities/ent-9')
    expect(screen.getByText('Na biblioteca')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /Importar/ })).toBeNull()
  })
})

describe('CorrectMatchResultCard (0074)', () => {
  it('mostra pôster/sinopse (mesmo miolo do ExternalResultCard) e chama onEscolher com o resultado', async () => {
    const user = userEvent.setup()
    const onEscolher = vi.fn()
    renderWithClient(
      <CorrectMatchResultCard result={externo} onEscolher={onEscolher} />,
    )

    expect(screen.getByText('Um hacker descobre a verdade.')).toBeTruthy()
    await user.click(screen.getByRole('button', { name: 'Usar este' }))
    expect(onEscolher).toHaveBeenCalledWith(externo)
  })

  it('com internalId mostra o selo "Na biblioteca" JUNTO do "Usar este" (não esconde a ação, diferente do Importar)', () => {
    // Duplicate merge case: the result already is an internal entity, but the
    // correction still needs to be able to pick it.
    renderWithClient(
      <CorrectMatchResultCard
        result={externo}
        internalId="ent-9"
        onEscolher={() => {}}
      />,
    )

    expect(screen.getByText('Na biblioteca')).toBeTruthy()
    expect(
      screen.getByRole('button', { name: 'Usar este' }),
    ).toBeInTheDocument()
  })
})
