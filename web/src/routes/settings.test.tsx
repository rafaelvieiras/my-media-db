import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { SettingsScreen } from '@/routes/settings'
import type {
  AutoMaterializeArtworkSettings,
  LanguageSettings,
  RetroactiveEnrichmentSettings,
} from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson } from '@/test/detail-harness'
import { respostaCompletude } from '@/test/catalog-completeness-harness'

// Config vigente devolvida pelo GET /settings/languages nos testes.
const settingsIniciais: LanguageSettings = {
  primary: 'pt-BR',
  fallback: 'en',
  allowed: ['pt-BR', 'en'],
}

// Config vigente devolvida pelo GET /settings/enrichment-retroativo (0085).
const retroativoIniciais: RetroactiveEnrichmentSettings = { retry_days: 15 }

// Config vigente devolvida pelo GET /settings/auto-materialize-artwork (0089).
const autoMaterializarIniciais: AutoMaterializeArtworkSettings = {
  enabled: false,
}

// Roteia o fetch: GET/PUT de idiomas, de throttle de enriquecimento
// retroativo (0085) e do toggle de materialização automática de artes
// (0089) são tratados aqui; GET/PUT de completude de catalogação (0057)
// delegam a `respostaCompletude` (compartilhada com
// catalog-completeness-form.test.tsx) — esta suíte só precisa que a seção
// não quebre o mock de fetch, o comportamento próprio do formulário é
// coberto lá.
function fetchRouter(opts?: {
  putResposta?: LanguageSettings
  putErro?: { status: number; code: string; message: string }
  putRetroativoResposta?: RetroactiveEnrichmentSettings
  putRetroativoErro?: { status: number; code: string; message: string }
  autoMaterializarInicial?: AutoMaterializeArtworkSettings
  putAutoMaterializarErro?: { status: number; code: string; message: string }
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
    if (u === '/api/v1/settings/enrichment-retroativo' && method === 'GET') {
      return okJson(retroativoIniciais)
    }
    if (u === '/api/v1/settings/enrichment-retroativo' && method === 'PUT') {
      if (opts?.putRetroativoErro) {
        return erroJson(
          opts.putRetroativoErro.status,
          opts.putRetroativoErro.code,
          opts.putRetroativoErro.message,
        )
      }
      return okJson(opts?.putRetroativoResposta ?? retroativoIniciais)
    }
    if (u === '/api/v1/settings/auto-materialize-artwork' && method === 'GET') {
      return okJson(opts?.autoMaterializarInicial ?? autoMaterializarIniciais)
    }
    if (u === '/api/v1/settings/auto-materialize-artwork' && method === 'PUT') {
      if (opts?.putAutoMaterializarErro) {
        return erroJson(
          opts.putAutoMaterializarErro.status,
          opts.putAutoMaterializarErro.code,
          opts.putAutoMaterializarErro.message,
        )
      }
      const body = JSON.parse(
        (init?.body as string) ?? '{}',
      ) as AutoMaterializeArtworkSettings
      return okJson(body)
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

// Mesma ideia de `renderForm`, mas devolve o escopo da seção de ENRIQUECIMENTO
// RETROATIVO (0085) — cada seção tem seu próprio botão "Salvar".
async function renderFormRetroativo(fetchMock: ReturnType<typeof fetchRouter>) {
  vi.stubGlobal('fetch', fetchMock)
  renderWithClient(<SettingsScreen />)
  const heading = await screen.findByRole('heading', {
    name: 'Enriquecimento retroativo',
  })
  const secao = within(heading.closest('section')!)
  await waitFor(() =>
    expect(secao.getByRole('button', { name: 'Salvar' })).toBeInTheDocument(),
  )
  return secao
}

// Mesma ideia de `renderForm`, mas devolve o escopo da seção ARTES (0089) —
// sem botão "Salvar" (o switch grava a cada mudança), por isso espera pelo
// próprio switch em vez de um botão.
async function renderFormArtes(fetchMock: ReturnType<typeof fetchRouter>) {
  vi.stubGlobal('fetch', fetchMock)
  renderWithClient(<SettingsScreen />)
  const heading = await screen.findByRole('heading', { name: 'Artes' })
  const secao = within(heading.closest('section')!)
  await waitFor(() =>
    expect(
      secao.getByRole('switch', {
        name: 'Baixar artes automaticamente após o scan',
      }),
    ).toBeInTheDocument(),
  )
  return secao
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

  it('le o throttle vigente e semeia o campo de enriquecimento retroativo', async () => {
    const secao = await renderFormRetroativo(fetchRouter())

    expect(secao.getByLabelText('Repetir tentativa após (dias)')).toHaveValue(
      15,
    )
  })

  it('grava o throttle de enriquecimento retroativo via PUT e confirma o sucesso', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({
      putRetroativoResposta: { retry_days: 30 },
    })
    const secao = await renderFormRetroativo(fetchMock)

    const input = secao.getByLabelText('Repetir tentativa após (dias)')
    await user.clear(input)
    await user.type(input, '30')
    await user.click(secao.getByRole('button', { name: 'Salvar' }))

    await waitFor(() =>
      expect(secao.getByText('Configurações salvas.')).toBeInTheDocument(),
    )
    const putCall = fetchMock.mock.calls.find(
      ([url, init]) =>
        String(url) === '/api/v1/settings/enrichment-retroativo' &&
        (init as RequestInit | undefined)?.method === 'PUT',
    )
    expect(putCall).toBeDefined()
    expect(JSON.parse((putCall![1] as RequestInit).body as string)).toEqual({
      retry_days: 30,
    })
  })

  it('valida client-side o range [1, 365] do throttle e nao chama o PUT', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter()
    const secao = await renderFormRetroativo(fetchMock)

    const input = secao.getByLabelText('Repetir tentativa após (dias)')
    await user.clear(input)
    await user.type(input, '400')
    await user.click(secao.getByRole('button', { name: 'Salvar' }))

    await waitFor(() =>
      expect(
        secao.getByText('Informe um número inteiro entre 1 e 365 dias.'),
      ).toBeInTheDocument(),
    )
    expect(
      fetchMock.mock.calls.some(
        ([url, init]) =>
          String(url) === '/api/v1/settings/enrichment-retroativo' &&
          (init as RequestInit | undefined)?.method === 'PUT',
      ),
    ).toBe(false)
  })

  it('exibe inline a mensagem do 422 do servidor para o throttle', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({
      putRetroativoErro: {
        status: 422,
        code: 'invalid_retroactive_enrichment_settings',
        message: 'retry_days precisa estar entre 1 e 365',
      },
    })
    const secao = await renderFormRetroativo(fetchMock)

    await user.click(secao.getByRole('button', { name: 'Salvar' }))

    await waitFor(() =>
      expect(
        secao.getByText('retry_days precisa estar entre 1 e 365'),
      ).toBeInTheDocument(),
    )
  })

  it('le a config vigente e semeia o switch de materializacao automatica de artes (desligado)', async () => {
    const secao = await renderFormArtes(fetchRouter())

    expect(
      secao.getByRole('switch', {
        name: 'Baixar artes automaticamente após o scan',
      }),
    ).not.toBeChecked()
  })

  it('semeia o switch ligado quando o backend ja devolve enabled=true', async () => {
    const secao = await renderFormArtes(
      fetchRouter({ autoMaterializarInicial: { enabled: true } }),
    )

    expect(
      secao.getByRole('switch', {
        name: 'Baixar artes automaticamente após o scan',
      }),
    ).toBeChecked()
  })

  it('grava o toggle de materializacao automatica via PUT ao ligar o switch', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter()
    const secao = await renderFormArtes(fetchMock)

    const switchArtes = secao.getByRole('switch', {
      name: 'Baixar artes automaticamente após o scan',
    })
    await user.click(switchArtes)

    await waitFor(() => expect(switchArtes).toBeChecked())
    const putCall = fetchMock.mock.calls.find(
      ([url, init]) =>
        String(url) === '/api/v1/settings/auto-materialize-artwork' &&
        (init as RequestInit | undefined)?.method === 'PUT',
    )
    expect(putCall).toBeDefined()
    expect(JSON.parse((putCall![1] as RequestInit).body as string)).toEqual({
      enabled: true,
    })
  })

  it('exibe inline a mensagem do erro do servidor ao falhar o PUT do toggle de artes', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({
      putAutoMaterializarErro: {
        status: 400,
        code: 'invalid_body',
        message: 'corpo invalido',
      },
    })
    const secao = await renderFormArtes(fetchMock)

    const switchArtes = secao.getByRole('switch', {
      name: 'Baixar artes automaticamente após o scan',
    })
    await user.click(switchArtes)

    await waitFor(() =>
      expect(secao.getByText('corpo invalido')).toBeInTheDocument(),
    )
  })
})
