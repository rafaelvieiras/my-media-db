import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ItemNewScreen } from '@/routes/item-new'
import { useUnsavedGuard } from '@/lib/use-unsaved-guard'
import { renderWithClient } from '@/test/render'
import { okJson } from '@/test/entity-fixtures'
import { navegacaoDaCriacao, stubCriacao } from '@/test/criacao-harness'
import { erroJson } from '@/test/detail-harness'

// The guarding hook is a router blocker: without a RouterProvider it cannot run, so
// it is swapped for a plain spy (the hook has its own test) — the item screen test
// only checks the argument it receives on the dirty transition.
vi.mock('@/lib/use-unsaved-guard', () => ({ useUnsavedGuard: vi.fn() }))

// Route `/new/item`: the schema-driven form creates a LOCAL item and
// navigates to `/entities/{id}`; the 422 appears inline. `useNavigate` is a spy and
// `Link` becomes an <a> (both come from the shared harness of the creation screens).
vi.mock('@tanstack/react-router', async () =>
  (await import('@/test/criacao-harness')).roteadorDaCriacao(),
)

// fetchRouter serves the descriptors (GET) and routes POST /entities to `resp`.
const fetchRouter = (resp: () => Response) =>
  stubCriacao('/api/v1/entities', resp)

afterEach(() => {
  vi.restoreAllMocks()
  navegacaoDaCriacao.navigate.mockReset()
})

describe('ItemNewScreen', () => {
  it('creates the item (type + title) and navigates to /entities/{id}', async () => {
    const m = fetchRouter(() =>
      okJson({ id: 'mv-3', type: 'tv', title: 'Severance' }),
    )
    renderWithClient(<ItemNewScreen />)
    const user = userEvent.setup()

    await user.selectOptions(await screen.findByLabelText('Item type'), 'tv')
    await user.type(screen.getByLabelText('Título'), 'Severance')
    await user.click(screen.getByRole('button', { name: 'Create item' }))

    await vi.waitFor(() =>
      expect(navegacaoDaCriacao.navigate).toHaveBeenCalledWith({
        to: '/entities/$id',
        params: { id: 'mv-3' },
      }),
    )
    const corpo = JSON.parse(String(m.mock.calls.at(-1)?.[1]?.body))
    expect(corpo.type).toBe('tv')
    expect(corpo.title).toBe('Severance')
  })

  it('shows the cross-link to the Search before the form (0077)', async () => {
    fetchRouter(() => okJson({}))
    renderWithClient(<ItemNewScreen />)

    expect(
      screen.getByText(/Looking for a title that already exists\?/),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('link', { name: 'Use the Search' }),
    ).toBeInTheDocument()
  })

  it('submitting with an empty title shows the error and does not create anything', async () => {
    const m = fetchRouter(() => okJson({ id: 'mv-9', type: 'movie' }))
    renderWithClient(<ItemNewScreen />)
    const user = userEvent.setup()

    // The primary action is always enabled (a disabled button with no explanation
    // is a dead end): submitting teaches what is missing instead.
    const botao = await screen.findByRole('button', { name: 'Create item' })
    expect(botao).toBeEnabled()

    await user.click(botao)

    const alertas = await screen.findAllByRole('alert')
    expect(
      alertas.some((a) =>
        a.textContent?.includes(
          'Fill in the required fields before continuing:',
        ),
      ),
    ).toBe(true)
    expect(screen.getByLabelText('Título')).toHaveAttribute(
      'aria-invalid',
      'true',
    )
    expect(screen.getByLabelText('Título')).toHaveAttribute(
      'aria-required',
      'true',
    )
    // The POST of the creation never happened.
    expect(
      m.mock.calls.some(
        ([, init]) => (init as RequestInit | undefined)?.method === 'POST',
      ),
    ).toBe(false)
    expect(navegacaoDaCriacao.navigate).not.toHaveBeenCalled()
  })

  it('Enter in the title submits the form', async () => {
    const m = fetchRouter(() => okJson({ id: 'mv-7', type: 'movie' }))
    renderWithClient(<ItemNewScreen />)
    const user = userEvent.setup()

    await user.type(await screen.findByLabelText('Título'), 'Duna{Enter}')

    await vi.waitFor(() =>
      expect(navegacaoDaCriacao.navigate).toHaveBeenCalledWith({
        to: '/entities/$id',
        params: { id: 'mv-7' },
      }),
    )
    expect(
      m.mock.calls.some(
        ([, init]) => (init as RequestInit | undefined)?.method === 'POST',
      ),
    ).toBe(true)
  })

  it('the required title is marked with an asterisk and aria-required', async () => {
    fetchRouter(() => okJson({}))
    renderWithClient(<ItemNewScreen />)
    const titulo = await screen.findByLabelText('Título')
    expect(titulo).toHaveAttribute('aria-required', 'true')
    expect(titulo.closest('label')?.textContent).toContain('*')
  })

  it('shows the 422 error inline and does not navigate', async () => {
    fetchRouter(() => erroJson(422, 'invalid_body', 'titulo obrigatorio'))
    renderWithClient(<ItemNewScreen />)
    const user = userEvent.setup()

    await user.type(await screen.findByLabelText('Título'), 'x')
    await user.click(screen.getByRole('button', { name: 'Create item' }))

    expect(await screen.findByText('titulo obrigatorio')).toBeInTheDocument()
    expect(navegacaoDaCriacao.navigate).not.toHaveBeenCalled()
  })

  it('tells the leave guard the screen is pristine, then dirty after typing', async () => {
    fetchRouter(() => okJson({}))
    renderWithClient(<ItemNewScreen />)
    const user = userEvent.setup()

    const titulo = await screen.findByLabelText('Título')
    expect(useUnsavedGuard).toHaveBeenLastCalledWith(false)
    await user.type(titulo, 'D')
    await vi.waitFor(() =>
      expect(useUnsavedGuard).toHaveBeenLastCalledWith(true),
    )
  })
})
