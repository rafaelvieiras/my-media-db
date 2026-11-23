import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { I18nProvider } from '@/lib/i18n'
import { LanguageProvider } from '@/lib/language-context'
import { ListaEpisodiosTemporada } from '@/components/lista-episodios-temporada'
import type { EntityCard } from '@/lib/api'
import type { ReactNode } from 'react'
import { entityCardBase } from '@/test/detail-harness'

// O PosterCard usa o Link do router: fora do app ele vira um <a> simples.
vi.mock('@tanstack/react-router', () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
}))

// The season list must not weigh the missing episodes: they are a gap in the collection, not a
// task. Hidden by default, no highlight, and the owner opens them when he wants to check the
// shape of what he has.

const membros: EntityCard[] = [
  entityCardBase({ id: 'ep-1', type: 'episode', title: 'Pilot' }),
  entityCardBase({ id: 'ep-2', type: 'episode', title: 'Second' }),
  entityCardBase({
    id: 'ep-3',
    type: 'episode',
    title: 'Missing one',
    status: 'draft',
    owned: false,
    file_count: 0,
    match_confidence: null,
  }),
]

// Só a query dos membros é falsa: o provider de idioma continua a ser o de verdade.
vi.mock('@/lib/queries', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/queries')>()),
  entityMembersQueryOptions: () => ({
    queryKey: ['members', 'season'],
    queryFn: () => Promise.resolve(membros),
  }),
}))

function montar() {
  // A tela é em inglês: o provider de i18n lê o idioma guardado.
  localStorage.setItem('mmdb.ui-lang', 'en')
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={qc}>
      <LanguageProvider>
        <I18nProvider>
          <ListaEpisodiosTemporada id="temp-1" />
        </I18nProvider>
      </LanguageProvider>
    </QueryClientProvider>,
  )
}

describe('ListaEpisodiosTemporada', () => {
  beforeEach(() => vi.clearAllMocks())

  it('shows only the episodes the library has', async () => {
    montar()
    expect(await screen.findByText('Pilot')).toBeInTheDocument()
    expect(screen.getByText('Second')).toBeInTheDocument()
    expect(screen.queryByText('Missing one')).not.toBeInTheDocument()
  })

  it('counts the episodes outside the library and reveals them on demand', async () => {
    montar()
    const botao = await screen.findByRole('switch', {
      name: /show episodes outside the library/i,
    })
    expect(
      screen.getByText(/1 episodes outside the library/i),
    ).toBeInTheDocument()
    await userEvent.click(botao)
    const lista = await screen.findByTestId('episodios-fora-da-biblioteca')
    expect(lista).toHaveTextContent('Missing one')
    expect(lista).toHaveTextContent(/not in the library/i)
  })
})
