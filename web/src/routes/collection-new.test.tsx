import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { CollectionNewScreen } from '@/routes/collection-new'
import { useUnsavedGuard } from '@/lib/use-unsaved-guard'
import { I18nProvider } from '@/lib/i18n'
import { renderWithClient } from '@/test/render'
import { okJson } from '@/test/entity-fixtures'
import { navegacaoDaCriacao, stubCriacao } from '@/test/criacao-harness'
import { erroJson } from '@/test/detail-harness'

// `useUnsavedGuard` talks to the router blocker, which needs a RouterProvider: here it
// becomes a spy so the collection screen test can assert the dirty argument (the hook
// itself is covered by its own test).
vi.mock('@/lib/use-unsaved-guard', () => ({ useUnsavedGuard: vi.fn() }))

// Route `/new/collection`: creates a LOCAL collection (title + description) and
// navigates to `/entities/{id}`; the 422 (empty title) appears inline. The mocked
// router comes from the shared harness of the creation screens.
vi.mock('@tanstack/react-router', async () =>
  (await import('@/test/criacao-harness')).roteadorDaCriacao(),
)

const fetchRouter = (resp: () => Response) =>
  stubCriacao('/api/v1/collections', resp)

afterEach(() => {
  vi.restoreAllMocks()
  localStorage.clear()
  navegacaoDaCriacao.navigate.mockReset()
})

describe('CollectionNewScreen', () => {
  it('shows only the fields that apply to a collection (title + description)', async () => {
    fetchRouter(() => okJson({}))
    renderWithClient(<CollectionNewScreen />)
    await screen.findByLabelText('Título')
    expect(screen.getByLabelText('Sinopse')).toBeInTheDocument()
    // Fields that do not apply to a collection do not appear.
    expect(screen.queryByLabelText('Data de lançamento')).toBeNull()
    expect(screen.queryByLabelText('Sua nota')).toBeNull()
  })

  it('creates the collection and navigates to /entities/{id}', async () => {
    const m = fetchRouter(() =>
      okJson({ id: 'col-9', type: 'collection', title: 'Trilogia' }),
    )
    renderWithClient(<CollectionNewScreen />)
    const user = userEvent.setup()

    await user.type(await screen.findByLabelText('Título'), 'Trilogia')
    await user.click(screen.getByRole('button', { name: 'Create collection' }))

    await vi.waitFor(() =>
      expect(navegacaoDaCriacao.navigate).toHaveBeenCalledWith({
        to: '/entities/$id',
        params: { id: 'col-9' },
      }),
    )
    const corpo = JSON.parse(String(m.mock.calls.at(-1)?.[1]?.body))
    expect(corpo.title).toBe('Trilogia')
  })

  it('shows the 422 error inline and does not navigate', async () => {
    fetchRouter(() => erroJson(422, 'invalid_body', 'titulo obrigatorio'))
    renderWithClient(<CollectionNewScreen />)
    const user = userEvent.setup()

    await user.type(await screen.findByLabelText('Título'), 'x')
    await user.click(screen.getByRole('button', { name: 'Create collection' }))

    expect(await screen.findByText('titulo obrigatorio')).toBeInTheDocument()
    expect(navegacaoDaCriacao.navigate).not.toHaveBeenCalled()
  })

  it('renders the copy in pt-BR when the interface language is pt-BR', async () => {
    // The interface language is a browser preference (ADR-0032): pin it before
    // rendering so the Portuguese copy is the one under test.
    localStorage.setItem('mmdb.ui-lang', 'pt-BR')
    fetchRouter(() => okJson({}))
    renderWithClient(
      <I18nProvider>
        <CollectionNewScreen />
      </I18nProvider>,
    )

    expect(
      await screen.findByRole('heading', { name: 'Nova coleção' }),
    ).toBeInTheDocument()
    expect(
      await screen.findByRole('button', { name: 'Criar coleção' }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('link', { name: /Voltar à biblioteca/ }),
    ).toBeInTheDocument()
  })
  it('tells the leave guard the screen is pristine, then dirty after typing', async () => {
    fetchRouter(() => okJson({}))
    renderWithClient(<CollectionNewScreen />)
    const user = userEvent.setup()

    const titulo = await screen.findByLabelText('Título')
    expect(useUnsavedGuard).toHaveBeenLastCalledWith(false)
    await user.type(titulo, 'T')
    await vi.waitFor(() =>
      expect(useUnsavedGuard).toHaveBeenLastCalledWith(true),
    )
  })
})
