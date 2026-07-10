import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import { ExternalResultCard } from '@/components/external-result-card'
import type { ExternalResult } from '@/lib/api'
import { renderWithClient } from '@/test/render'

// Link (helper compartilhado) interpola os `params` no `href` — para afirmar que
// o card aponta para a tela de detalhe do externo. `useNavigate` é spy (só usado
// no pós-import, não exercido aqui).
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

    // O botão existe e não está aninhado no link (âncora não contém o botão).
    const botao = screen.getByRole('button', { name: /Importar/ })
    const link = screen.getByRole('link', { name: /The Matrix/ })
    expect(link.contains(botao)).toBe(false)
  })
})
