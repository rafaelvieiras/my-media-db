import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { CatalogCompletenessForm } from '@/components/catalog-completeness-form'
import type { CatalogCompletenessSettings } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import {
  completenessDefault as defaultFabrica,
  respostaCompletude,
} from '@/test/catalog-completeness-harness'

function fetchRouter(opts?: Parameters<typeof respostaCompletude>[2]) {
  return vi.fn(async (url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    const resp = respostaCompletude(String(url), method, opts)
    if (!resp) throw new Error(`rota nao mockada: ${method} ${url}`)
    return resp
  })
}

// The save button and the success message come from `FormSaveFooter`
// (`components/query-states.tsx`), which another domain owns: those two queries
// tolerate both labels so this test does not depend on that extraction.
const BOTAO_SALVAR = /^(Salvar|Save)$/
const MENSAGEM_SALVO = /Configurações salvas\.|Settings saved\./

async function renderForm(fetchMock: ReturnType<typeof fetchRouter>) {
  vi.stubGlobal('fetch', fetchMock)
  renderWithClient(<CatalogCompletenessForm />)
  await waitFor(() =>
    expect(
      screen.getByRole('button', { name: BOTAO_SALVAR }),
    ).toBeInTheDocument(),
  )
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('CatalogCompletenessForm', () => {
  it('renders both blocks (Movie/Series) with the expected checkboxes', async () => {
    await renderForm(fetchRouter())

    const blocoFilme = screen.getByText('Movie').closest('div')!
    const blocoSerie = screen.getByText('Series').closest('div')!

    for (const bloco of [blocoFilme, blocoSerie]) {
      const utils = within(bloco)
      expect(
        utils.getByRole('checkbox', { name: 'Synopsis' }),
      ).toBeInTheDocument()
      expect(
        utils.getByRole('checkbox', { name: 'Duration' }),
      ).toBeInTheDocument()
      expect(
        utils.getByRole('checkbox', { name: 'Age rating' }),
      ).toBeInTheDocument()
      expect(
        utils.getByRole('checkbox', { name: 'Keywords' }),
      ).toBeInTheDocument()
      expect(utils.getByRole('checkbox', { name: 'Cast' })).toBeInTheDocument()
      expect(
        utils.getByRole('checkbox', { name: 'Media file' }),
      ).toBeInTheDocument()
      expect(
        utils.getByRole('checkbox', { name: 'Poster' }),
      ).toBeInTheDocument()
      expect(
        utils.getByRole('checkbox', { name: 'Fanart' }),
      ).toBeInTheDocument()
      expect(utils.getByRole('checkbox', { name: 'Logo' })).toBeInTheDocument()
      expect(
        utils.getByRole('checkbox', { name: 'Clearart' }),
      ).toBeInTheDocument()
      expect(
        utils.getByRole('checkbox', { name: 'Banner' }),
      ).toBeInTheDocument()
      expect(utils.getByRole('checkbox', { name: 'Thumb' })).toBeInTheDocument()
      expect(
        utils.getByRole('checkbox', { name: 'Disc cover' }),
      ).toBeInTheDocument()
      // `still`/`characterart` stay out of the offer of this screen (§6).
      expect(
        utils.queryByRole('checkbox', { name: /still/i }),
      ).not.toBeInTheDocument()
      expect(
        utils.queryByRole('checkbox', { name: /characterart/i }),
      ).not.toBeInTheDocument()
    }
  })

  it('pre-checks the factory default on first access (no previous config)', async () => {
    await renderForm(fetchRouter())

    const blocoFilme = screen.getByText('Movie').closest('div')!
    const utils = within(blocoFilme)

    expect(utils.getByRole('checkbox', { name: 'Synopsis' })).toBeChecked()
    expect(utils.getByRole('checkbox', { name: 'Media file' })).toBeChecked()
    expect(utils.getByRole('checkbox', { name: 'Poster' })).toBeChecked()

    expect(utils.getByRole('checkbox', { name: 'Duration' })).not.toBeChecked()
    expect(
      utils.getByRole('checkbox', { name: 'Age rating' }),
    ).not.toBeChecked()
    expect(utils.getByRole('checkbox', { name: 'Keywords' })).not.toBeChecked()
    expect(utils.getByRole('checkbox', { name: 'Cast' })).not.toBeChecked()
    expect(utils.getByRole('checkbox', { name: 'Fanart' })).not.toBeChecked()
    expect(utils.getByRole('checkbox', { name: 'Logo' })).not.toBeChecked()
    expect(utils.getByRole('checkbox', { name: 'Clearart' })).not.toBeChecked()
    expect(utils.getByRole('checkbox', { name: 'Banner' })).not.toBeChecked()
    expect(utils.getByRole('checkbox', { name: 'Thumb' })).not.toBeChecked()
    expect(
      utils.getByRole('checkbox', { name: 'Disc cover' }),
    ).not.toBeChecked()
  })

  it('checks/unchecks a checkbox and saves by calling PUT with the expected body', async () => {
    const user = userEvent.setup()
    const putResposta: CatalogCompletenessSettings = {
      movie: { ...defaultFabrica.movie, cast: true },
      tv: defaultFabrica.tv,
    }
    const fetchMock = fetchRouter({ putResposta })
    await renderForm(fetchMock)

    const blocoFilme = screen.getByText('Movie').closest('div')!
    await user.click(within(blocoFilme).getByRole('checkbox', { name: 'Cast' }))
    await user.click(screen.getByRole('button', { name: BOTAO_SALVAR }))

    await waitFor(() =>
      expect(screen.getByText(MENSAGEM_SALVO)).toBeInTheDocument(),
    )

    const putCall = fetchMock.mock.calls.find(
      ([, init]) => (init as RequestInit | undefined)?.method === 'PUT',
    )
    expect(putCall).toBeDefined()
    expect(JSON.parse((putCall![1] as RequestInit).body as string)).toEqual({
      movie: { ...defaultFabrica.movie, cast: true },
      tv: defaultFabrica.tv,
    })
  })

  it('shows the server 422 message inline', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({
      putErro: {
        status: 422,
        code: 'invalid_body',
        message: 'kind de arte desconhecido',
      },
    })
    await renderForm(fetchMock)

    await user.click(screen.getByRole('button', { name: BOTAO_SALVAR }))

    await waitFor(() =>
      expect(screen.getByText('kind de arte desconhecido')).toBeInTheDocument(),
    )
  })
})
