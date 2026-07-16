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

async function renderForm(fetchMock: ReturnType<typeof fetchRouter>) {
  vi.stubGlobal('fetch', fetchMock)
  renderWithClient(<CatalogCompletenessForm />)
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Salvar' })).toBeInTheDocument(),
  )
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('CatalogCompletenessForm', () => {
  it('renderiza os dois blocos (Filme/Série) com os checkboxes esperados', async () => {
    await renderForm(fetchRouter())

    const blocoFilme = screen.getByText('Filme').closest('div')!
    const blocoSerie = screen.getByText('Série').closest('div')!

    for (const bloco of [blocoFilme, blocoSerie]) {
      const utils = within(bloco)
      expect(
        utils.getByRole('checkbox', { name: 'Sinopse' }),
      ).toBeInTheDocument()
      expect(
        utils.getByRole('checkbox', { name: 'Duração' }),
      ).toBeInTheDocument()
      expect(
        utils.getByRole('checkbox', { name: 'Classificação indicativa' }),
      ).toBeInTheDocument()
      expect(
        utils.getByRole('checkbox', { name: 'Palavras-chave' }),
      ).toBeInTheDocument()
      expect(
        utils.getByRole('checkbox', { name: 'Elenco' }),
      ).toBeInTheDocument()
      expect(
        utils.getByRole('checkbox', { name: 'Arquivo de mídia' }),
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
        utils.getByRole('checkbox', { name: 'Capa de disco' }),
      ).toBeInTheDocument()
      // `still`/`characterart` ficam fora da oferta desta tela (briefing §6).
      expect(
        utils.queryByRole('checkbox', { name: /still/i }),
      ).not.toBeInTheDocument()
      expect(
        utils.queryByRole('checkbox', { name: /characterart/i }),
      ).not.toBeInTheDocument()
    }
  })

  it('pre-marca o default de fabrica no primeiro acesso (sem config previa)', async () => {
    await renderForm(fetchRouter())

    const blocoFilme = screen.getByText('Filme').closest('div')!
    const utils = within(blocoFilme)

    expect(utils.getByRole('checkbox', { name: 'Sinopse' })).toBeChecked()
    expect(
      utils.getByRole('checkbox', { name: 'Arquivo de mídia' }),
    ).toBeChecked()
    expect(utils.getByRole('checkbox', { name: 'Poster' })).toBeChecked()

    expect(utils.getByRole('checkbox', { name: 'Duração' })).not.toBeChecked()
    expect(
      utils.getByRole('checkbox', { name: 'Classificação indicativa' }),
    ).not.toBeChecked()
    expect(
      utils.getByRole('checkbox', { name: 'Palavras-chave' }),
    ).not.toBeChecked()
    expect(utils.getByRole('checkbox', { name: 'Elenco' })).not.toBeChecked()
    expect(utils.getByRole('checkbox', { name: 'Fanart' })).not.toBeChecked()
    expect(utils.getByRole('checkbox', { name: 'Logo' })).not.toBeChecked()
    expect(utils.getByRole('checkbox', { name: 'Clearart' })).not.toBeChecked()
    expect(utils.getByRole('checkbox', { name: 'Banner' })).not.toBeChecked()
    expect(utils.getByRole('checkbox', { name: 'Thumb' })).not.toBeChecked()
    expect(
      utils.getByRole('checkbox', { name: 'Capa de disco' }),
    ).not.toBeChecked()
  })

  it('marca/desmarca um checkbox e salva chamando o PUT com o corpo esperado', async () => {
    const user = userEvent.setup()
    const putResposta: CatalogCompletenessSettings = {
      movie: { ...defaultFabrica.movie, cast: true },
      tv: defaultFabrica.tv,
    }
    const fetchMock = fetchRouter({ putResposta })
    await renderForm(fetchMock)

    const blocoFilme = screen.getByText('Filme').closest('div')!
    await user.click(
      within(blocoFilme).getByRole('checkbox', { name: 'Elenco' }),
    )
    await user.click(screen.getByRole('button', { name: 'Salvar' }))

    await waitFor(() =>
      expect(screen.getByText('Configurações salvas.')).toBeInTheDocument(),
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

  it('exibe inline a mensagem do 422 do servidor', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({
      putErro: {
        status: 422,
        code: 'invalid_body',
        message: 'kind de arte desconhecido',
      },
    })
    await renderForm(fetchMock)

    await user.click(screen.getByRole('button', { name: 'Salvar' }))

    await waitFor(() =>
      expect(screen.getByText('kind de arte desconhecido')).toBeInTheDocument(),
    )
  })
})
