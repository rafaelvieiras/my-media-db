import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { SettingsScreen } from '@/routes/settings'
import type { LanguageSettings } from '@/lib/api'
import { renderWithClient } from '@/test/render'

// Config vigente devolvida pelo GET /settings/languages nos testes.
const settingsIniciais: LanguageSettings = {
  primary: 'pt-BR',
  fallback: 'en',
  allowed: ['pt-BR', 'en'],
}

function okJson<T>(body: T, status = 200) {
  return { ok: true, status, json: async () => body } as Response
}

// Roteia o fetch: GET devolve as settings iniciais; o PUT devolve `putResposta`
// (config gravada) ou, quando `putErro` é dado, um envelope de erro (422).
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
        return {
          ok: false,
          status: opts.putErro.status,
          json: async () => ({
            error: { code: opts.putErro!.code, message: opts.putErro!.message },
          }),
        } as Response
      }
      return okJson(opts?.putResposta ?? settingsIniciais)
    }
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
async function renderForm(fetchMock: ReturnType<typeof fetchRouter>) {
  vi.stubGlobal('fetch', fetchMock)
  renderWithClient(<SettingsScreen />)
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Salvar' })).toBeInTheDocument(),
  )
}

describe('SettingsScreen', () => {
  it('le a config vigente e semeia o formulario', async () => {
    await renderForm(fetchRouter())

    // Principal semeado em pt-BR, fallback em en; ambos permitidos marcados.
    expect(screen.getByLabelText('Idioma principal')).toHaveValue('pt-BR')
    expect(screen.getByLabelText('Idioma de fallback')).toHaveValue('en')
    expect(
      screen.getByRole('checkbox', { name: /Português \(BR\)/ }),
    ).toBeChecked()
    expect(screen.getByRole('checkbox', { name: /Inglês/ })).toBeChecked()
    expect(screen.getByRole('checkbox', { name: /Espanhol/ })).not.toBeChecked()
  })

  it('valida principal fora dos permitidos e nao chama o PUT', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter()
    await renderForm(fetchMock)

    // 'es' nao esta em allowed; escolher como principal deve barrar no cliente.
    await user.selectOptions(screen.getByLabelText('Idioma principal'), 'es')
    await user.click(screen.getByRole('button', { name: 'Salvar' }))

    expect(
      screen.getByText('O idioma principal precisa estar entre os permitidos.'),
    ).toBeInTheDocument()
    // Nenhum PUT foi enviado (validacao client-side barrou).
    expect(
      fetchMock.mock.calls.some(
        ([, init]) => (init as RequestInit | undefined)?.method === 'PUT',
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
    await renderForm(fetchMock)

    // Adiciona 'es' aos permitidos e salva (estado valido).
    await user.click(screen.getByRole('checkbox', { name: /Espanhol/ }))
    await user.click(screen.getByRole('button', { name: 'Salvar' }))

    await waitFor(() =>
      expect(screen.getByText('Configurações salvas.')).toBeInTheDocument(),
    )
    const putCall = fetchMock.mock.calls.find(
      ([, init]) => (init as RequestInit | undefined)?.method === 'PUT',
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
    await renderForm(fetchMock)

    // Estado valido no cliente (nao mexe em nada), mas o servidor rejeita.
    await user.click(screen.getByRole('button', { name: 'Salvar' }))

    await waitFor(() =>
      expect(
        screen.getByText('principal precisa estar entre os permitidos'),
      ).toBeInTheDocument(),
    )
  })
})
