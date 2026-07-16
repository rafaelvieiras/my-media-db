import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { SettingsScreen } from '@/routes/settings'
import type { LanguageSettings } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson } from '@/test/detail-harness'
import { respostaCompletude } from '@/test/catalog-completeness-harness'

// Config vigente devolvida pelo GET /settings/languages nos testes.
const settingsIniciais: LanguageSettings = {
  primary: 'pt-BR',
  fallback: 'en',
  allowed: ['pt-BR', 'en'],
}

// Roteia o fetch: GET/PUT de idiomas são tratados aqui; GET/PUT de completude de
// catalogação (0057) delegam a `respostaCompletude` (compartilhada com
// catalog-completeness-form.test.tsx) — esta suíte só precisa que a 2ª seção não
// quebre o mock de fetch, o comportamento próprio do formulário é coberto lá.
function fetchRouter(opts?: {
  putResposta?: LanguageSettings
  putErro?: { status: number; code: string; message: string }
}) {
  return vi.fn(async (url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'
    if (u === '/api/v1/settings/languages' && method === 'GET') {
      return okJson(settingsIniciais)
    }
    if (u === '/api/v1/settings/languages' && method === 'PUT') {
      if (opts?.putErro) {
        return erroJson(
          opts.putErro.status,
          opts.putErro.code,
          opts.putErro.message,
        )
      }
      return okJson(opts?.putResposta ?? settingsIniciais)
    }
    const completude = respostaCompletude(u, method)
    if (completude) return completude
    throw new Error(`rota nao mockada: ${method} ${u}`)
  })
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  vi.restoreAllMocks()
})

// Renderiza a tela e espera o formulário (pós-carregamento das settings).
// Devolve o escopo da seção de IDIOMAS: a tela agora tem duas seções, cada
// uma com seu próprio botão "Salvar" — os testes desta suíte miram a de
// idiomas (a de completude de catalogação tem sua própria suíte, ver
// catalog-completeness-form.test.tsx).
async function renderForm(fetchMock: ReturnType<typeof fetchRouter>) {
  vi.stubGlobal('fetch', fetchMock)
  renderWithClient(<SettingsScreen />)
  const heading = await screen.findByRole('heading', { name: 'Configurações' })
  const secaoIdiomas = within(heading.closest('section')!)
  await waitFor(() =>
    expect(
      secaoIdiomas.getByRole('button', { name: 'Salvar' }),
    ).toBeInTheDocument(),
  )
  return secaoIdiomas
}

describe('SettingsScreen', () => {
  it('le a config vigente e semeia o formulario', async () => {
    const secaoIdiomas = await renderForm(fetchRouter())

    // Principal semeado em pt-BR, fallback em en; ambos permitidos marcados.
    expect(secaoIdiomas.getByLabelText('Idioma principal')).toHaveValue('pt-BR')
    expect(secaoIdiomas.getByLabelText('Idioma de fallback')).toHaveValue('en')
    expect(
      secaoIdiomas.getByRole('checkbox', { name: /Português \(BR\)/ }),
    ).toBeChecked()
    expect(secaoIdiomas.getByRole('checkbox', { name: /Inglês/ })).toBeChecked()
    expect(
      secaoIdiomas.getByRole('checkbox', { name: /Espanhol/ }),
    ).not.toBeChecked()

    // A 2ª seção (0057) aparece abaixo, com o formulário de completude já
    // carregado.
    await waitFor(() =>
      expect(
        screen.getByRole('heading', { name: 'Completude de catalogação' }),
      ).toBeInTheDocument(),
    )
  })

  it('valida principal fora dos permitidos e nao chama o PUT', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter()
    const secaoIdiomas = await renderForm(fetchMock)

    // 'es' nao esta em allowed; escolher como principal deve barrar no cliente.
    await user.selectOptions(
      secaoIdiomas.getByLabelText('Idioma principal'),
      'es',
    )
    await user.click(secaoIdiomas.getByRole('button', { name: 'Salvar' }))

    expect(
      secaoIdiomas.getByText(
        'O idioma principal precisa estar entre os permitidos.',
      ),
    ).toBeInTheDocument()
    // Nenhum PUT foi enviado (validacao client-side barrou) para /languages.
    expect(
      fetchMock.mock.calls.some(
        ([url, init]) =>
          String(url) === '/api/v1/settings/languages' &&
          (init as RequestInit | undefined)?.method === 'PUT',
      ),
    ).toBe(false)
  })

  it('grava via PUT com o corpo editado e confirma o sucesso', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({
      putResposta: {
        primary: 'pt-BR',
        fallback: 'en',
        allowed: ['pt-BR', 'en', 'es'],
      },
    })
    const secaoIdiomas = await renderForm(fetchMock)

    // Adiciona 'es' aos permitidos e salva (estado valido).
    await user.click(secaoIdiomas.getByRole('checkbox', { name: /Espanhol/ }))
    await user.click(secaoIdiomas.getByRole('button', { name: 'Salvar' }))

    await waitFor(() =>
      expect(
        secaoIdiomas.getByText('Configurações salvas.'),
      ).toBeInTheDocument(),
    )
    const putCall = fetchMock.mock.calls.find(
      ([url, init]) =>
        String(url) === '/api/v1/settings/languages' &&
        (init as RequestInit | undefined)?.method === 'PUT',
    )
    expect(putCall).toBeDefined()
    expect(JSON.parse((putCall![1] as RequestInit).body as string)).toEqual({
      primary: 'pt-BR',
      fallback: 'en',
      allowed: ['pt-BR', 'en', 'es'],
    })
  })

  it('exibe inline a mensagem do 422 do servidor', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({
      putErro: {
        status: 422,
        code: 'invalid_language_settings',
        message: 'principal precisa estar entre os permitidos',
      },
    })
    const secaoIdiomas = await renderForm(fetchMock)

    // Estado valido no cliente (nao mexe em nada), mas o servidor rejeita.
    await user.click(secaoIdiomas.getByRole('button', { name: 'Salvar' }))

    await waitFor(() =>
      expect(
        secaoIdiomas.getByText('principal precisa estar entre os permitidos'),
      ).toBeInTheDocument(),
    )
  })
})
