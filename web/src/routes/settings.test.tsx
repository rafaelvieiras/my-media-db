import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { SettingsScreen } from '@/routes/settings'
import type {
  AutoMaterializeArtworkSettings,
  AutoDownloadTrailerTemaSettings,
  LanguageSettings,
  RetroactiveEnrichmentSettings,
  YtdlpSettings,
} from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson } from '@/test/detail-harness'
import { respostaCompletude } from '@/test/catalog-completeness-harness'

// Current config returned by GET /settings/languages in the tests.
const settingsIniciais: LanguageSettings = {
  primary: 'pt-BR',
  fallback: 'en',
  allowed: ['pt-BR', 'en'],
}

// Current config returned by GET /settings/enrichment-retroativo.
const retroativoIniciais: RetroactiveEnrichmentSettings = { retry_days: 15 }

// Current config returned by GET /settings/auto-materialize-artwork.
const autoMaterializarIniciais: AutoMaterializeArtworkSettings = {
  enabled: false,
}

// Current config returned by GET /settings/auto-download-trailer-tema.
// (0104).
const autoDownloadTrailerTemaIniciais: AutoDownloadTrailerTemaSettings = {
  enabled: false,
}

// Current status returned by GET /settings/ytdlp (ADR-0025).
const ytdlpIniciais: YtdlpSettings = {
  disponivel: true,
  origem: 'baixado',
  custom_binary_path: null,
  detalhe: null,
}

// Routes the fetch: GET/PUT of languages, of the enrichment
// throttle and of the automatic artwork materialization toggle
// are handled here; GET/PUT of cataloging completeness
// delegate to `respostaCompletude` (shared with
// catalog-completeness-form.test.tsx) — this suite only needs the section
// not to break the fetch mock, the behavior of the form itself is
// covered there.
function fetchRouter(opts?: {
  putResposta?: LanguageSettings
  putErro?: { status: number; code: string; message: string }
  putRetroativoResposta?: RetroactiveEnrichmentSettings
  putRetroativoErro?: { status: number; code: string; message: string }
  autoMaterializarInicial?: AutoMaterializeArtworkSettings
  putAutoMaterializarErro?: { status: number; code: string; message: string }
  autoDownloadTrailerTemaInicial?: AutoDownloadTrailerTemaSettings
  putAutoDownloadTrailerTemaErro?: {
    status: number
    code: string
    message: string
  }
  ytdlpInicial?: YtdlpSettings
  putYtdlpResposta?: YtdlpSettings
  putYtdlpErro?: { status: number; code: string; message: string }
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
    if (
      u === '/api/v1/settings/auto-download-trailer-tema' &&
      method === 'GET'
    ) {
      return okJson(
        opts?.autoDownloadTrailerTemaInicial ?? autoDownloadTrailerTemaIniciais,
      )
    }
    if (
      u === '/api/v1/settings/auto-download-trailer-tema' &&
      method === 'PUT'
    ) {
      if (opts?.putAutoDownloadTrailerTemaErro) {
        return erroJson(
          opts.putAutoDownloadTrailerTemaErro.status,
          opts.putAutoDownloadTrailerTemaErro.code,
          opts.putAutoDownloadTrailerTemaErro.message,
        )
      }
      const body = JSON.parse(
        (init?.body as string) ?? '{}',
      ) as AutoDownloadTrailerTemaSettings
      return okJson(body)
    }
    if (u === '/api/v1/settings/ytdlp' && method === 'GET') {
      return okJson(opts?.ytdlpInicial ?? ytdlpIniciais)
    }
    if (u === '/api/v1/settings/ytdlp' && method === 'PUT') {
      if (opts?.putYtdlpErro) {
        return erroJson(
          opts.putYtdlpErro.status,
          opts.putYtdlpErro.code,
          opts.putYtdlpErro.message,
        )
      }
      return okJson(opts?.putYtdlpResposta ?? ytdlpIniciais)
    }
    const completude = respostaCompletude(u, method)
    if (completude) return completude
    throw new Error(`rota nao mockada: ${method} ${u}`)
  })
}

// `FormSaveFooter` is a shared component outside this domain (its own extraction
// lands in parallel), so the submit label is accepted in either language.
const BOTAO_SALVAR = /^(Save|Salvar)$/

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  vi.restoreAllMocks()
})

// Renders the screen and waits for the form (after the settings load).
// It returns the scope of the LANGUAGES section: the screen now has two sections, each
// with its own "save" button — the tests of this suite target the
// languages one (the cataloging completeness one has its own suite, see
// catalog-completeness-form.test.tsx).
async function renderForm(fetchMock: ReturnType<typeof fetchRouter>) {
  vi.stubGlobal('fetch', fetchMock)
  renderWithClient(<SettingsScreen />)
  const heading = await screen.findByRole('heading', { name: 'Settings' })
  const secaoIdiomas = within(heading.closest('section')!)
  await waitFor(() =>
    expect(
      secaoIdiomas.getByRole('button', { name: BOTAO_SALVAR }),
    ).toBeInTheDocument(),
  )
  return secaoIdiomas
}

// Same idea as `renderForm`, but it returns the scope of the RETROACTIVE
// ENRICHMENT section — each section has its own save button.
async function renderFormRetroativo(fetchMock: ReturnType<typeof fetchRouter>) {
  vi.stubGlobal('fetch', fetchMock)
  renderWithClient(<SettingsScreen />)
  const heading = await screen.findByRole('heading', {
    name: 'Retroactive enrichment',
  })
  const secao = within(heading.closest('section')!)
  await waitFor(() =>
    expect(
      secao.getByRole('button', { name: BOTAO_SALVAR }),
    ).toBeInTheDocument(),
  )
  return secao
}

// Same idea as `renderForm`, but it returns the scope of the ARTWORK section —
// without a save button (the switch saves on every change), which is why it waits for the
// switch itself instead of a button.
async function renderFormArtes(fetchMock: ReturnType<typeof fetchRouter>) {
  vi.stubGlobal('fetch', fetchMock)
  renderWithClient(<SettingsScreen />)
  const heading = await screen.findByRole('heading', { name: 'Artwork' })
  const secao = within(heading.closest('section')!)
  await waitFor(() =>
    expect(
      secao.getByRole('switch', {
        name: 'Download artwork automatically after the scan',
      }),
    ).toBeInTheDocument(),
  )
  return secao
}

// Same idea as `renderFormArtes`, but it returns the scope of the TRAILER AND
// THEME SONG section -- without a save button (the switch saves on every change).
async function renderFormTrailerTema(
  fetchMock: ReturnType<typeof fetchRouter>,
) {
  vi.stubGlobal('fetch', fetchMock)
  renderWithClient(<SettingsScreen />)
  const heading = await screen.findByRole('heading', {
    name: 'Trailer and theme song',
  })
  const secao = within(heading.closest('section')!)
  await waitFor(() =>
    expect(
      secao.getByRole('switch', {
        name: 'Download trailer and theme automatically',
      }),
    ).toBeInTheDocument(),
  )
  return secao
}

// Same idea as `renderForm`, but it returns the scope of the YT-DLP section --
// it has a save button (unlike the Artwork switch: the custom path
// field needs an explicit confirmation before writing).
async function renderFormYtdlp(fetchMock: ReturnType<typeof fetchRouter>) {
  vi.stubGlobal('fetch', fetchMock)
  renderWithClient(<SettingsScreen />)
  const heading = await screen.findByRole('heading', { name: 'yt-dlp' })
  const secao = within(heading.closest('section')!)
  await waitFor(() =>
    expect(
      secao.getByRole('button', { name: BOTAO_SALVAR }),
    ).toBeInTheDocument(),
  )
  return secao
}

// Finds the PUT call made to `url` in the fetch mock and returns the already
// parsed body -- it avoids repeating the find+parse in every test that confirms the PUT.
function corpoDoPut(fetchMock: ReturnType<typeof fetchRouter>, url: string) {
  const putCall = fetchMock.mock.calls.find(
    ([u, init]) =>
      String(u) === url && (init as RequestInit | undefined)?.method === 'PUT',
  )
  expect(putCall).toBeDefined()
  return JSON.parse((putCall![1] as RequestInit).body as string) as unknown
}

describe('SettingsScreen', () => {
  it('reads the current config and seeds the form', async () => {
    const secaoIdiomas = await renderForm(fetchRouter())

    // Primary seeded in pt-BR, fallback in en; both marked as allowed.
    expect(secaoIdiomas.getByLabelText('Primary language')).toHaveValue('pt-BR')
    expect(secaoIdiomas.getByLabelText('Fallback language')).toHaveValue('en')
    expect(
      secaoIdiomas.getByRole('checkbox', { name: /Português \(Brasil\)/ }),
    ).toBeChecked()
    expect(
      secaoIdiomas.getByRole('checkbox', { name: /English/ }),
    ).toBeChecked()
    expect(
      secaoIdiomas.getByRole('checkbox', { name: /Español/ }),
    ).not.toBeChecked()

    // The 2nd section appears below, with the completeness form already
    // loaded.
    await waitFor(() =>
      expect(
        screen.getByRole('heading', { name: 'Cataloging completeness' }),
      ).toBeInTheDocument(),
    )
  })

  it('blocks a primary outside the allowed ones and does not call the PUT', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter()
    const secaoIdiomas = await renderForm(fetchMock)

    // 'es' is not in allowed; choosing it as primary must be blocked on the client.
    await user.selectOptions(
      secaoIdiomas.getByLabelText('Primary language'),
      'es',
    )
    await user.click(secaoIdiomas.getByRole('button', { name: BOTAO_SALVAR }))

    expect(
      secaoIdiomas.getByText(
        'The primary language must be one of the allowed ones.',
      ),
    ).toBeInTheDocument()
    // No PUT was sent (client-side validation blocked it) to /languages.
    expect(
      fetchMock.mock.calls.some(
        ([url, init]) =>
          String(url) === '/api/v1/settings/languages' &&
          (init as RequestInit | undefined)?.method === 'PUT',
      ),
    ).toBe(false)
  })

  it('saves via PUT with the edited body and confirms success', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({
      putResposta: {
        primary: 'pt-BR',
        fallback: 'en',
        allowed: ['pt-BR', 'en', 'es'],
      },
    })
    const secaoIdiomas = await renderForm(fetchMock)

    // It adds 'es' to the allowed ones and saves (valid state).
    await user.click(secaoIdiomas.getByRole('checkbox', { name: /Español/ }))
    await user.click(secaoIdiomas.getByRole('button', { name: BOTAO_SALVAR }))

    await waitFor(() =>
      expect(secaoIdiomas.getByText('Settings saved.')).toBeInTheDocument(),
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

  it('shows the 422 message of the server inline', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({
      putErro: {
        status: 422,
        code: 'invalid_language_settings',
        message: 'principal precisa estar entre os permitidos',
      },
    })
    const secaoIdiomas = await renderForm(fetchMock)

    // Valid state on the client (it touches nothing), but the server rejects it.
    await user.click(secaoIdiomas.getByRole('button', { name: BOTAO_SALVAR }))

    await waitFor(() =>
      expect(
        secaoIdiomas.getByText('principal precisa estar entre os permitidos'),
      ).toBeInTheDocument(),
    )
  })

  it('reads the current throttle and seeds the retroactive enrichment field', async () => {
    const secao = await renderFormRetroativo(fetchRouter())

    expect(secao.getByLabelText('Retry after (days)')).toHaveValue(15)
  })

  it('saves the retroactive enrichment throttle via PUT and confirms success', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({
      putRetroativoResposta: { retry_days: 30 },
    })
    const secao = await renderFormRetroativo(fetchMock)

    const input = secao.getByLabelText('Retry after (days)')
    await user.clear(input)
    await user.type(input, '30')
    await user.click(secao.getByRole('button', { name: BOTAO_SALVAR }))

    await waitFor(() =>
      expect(secao.getByText('Settings saved.')).toBeInTheDocument(),
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

  it('validates the [1, 365] range of the throttle on the client and does not call the PUT', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter()
    const secao = await renderFormRetroativo(fetchMock)

    const input = secao.getByLabelText('Retry after (days)')
    await user.clear(input)
    await user.type(input, '400')
    await user.click(secao.getByRole('button', { name: BOTAO_SALVAR }))

    await waitFor(() =>
      expect(
        secao.getByText('Enter a whole number between 1 and 365 days.'),
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

  it('shows the 422 message of the server inline for the throttle', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({
      putRetroativoErro: {
        status: 422,
        code: 'invalid_retroactive_enrichment_settings',
        message: 'retry_days precisa estar entre 1 e 365',
      },
    })
    const secao = await renderFormRetroativo(fetchMock)

    await user.click(secao.getByRole('button', { name: BOTAO_SALVAR }))

    await waitFor(() =>
      expect(
        secao.getByText('retry_days precisa estar entre 1 e 365'),
      ).toBeInTheDocument(),
    )
  })

  it('reads the current config and seeds the automatic artwork materialization switch (off)', async () => {
    const secao = await renderFormArtes(fetchRouter())

    expect(
      secao.getByRole('switch', {
        name: 'Download artwork automatically after the scan',
      }),
    ).not.toBeChecked()
  })

  it('seeds the switch on when the backend already returns enabled=true', async () => {
    const secao = await renderFormArtes(
      fetchRouter({ autoMaterializarInicial: { enabled: true } }),
    )

    expect(
      secao.getByRole('switch', {
        name: 'Download artwork automatically after the scan',
      }),
    ).toBeChecked()
  })

  it('saves the automatic materialization toggle via PUT when the switch is turned on', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter()
    const secao = await renderFormArtes(fetchMock)

    const switchArtes = secao.getByRole('switch', {
      name: 'Download artwork automatically after the scan',
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

  it('shows the server error message inline when the artwork toggle PUT fails', async () => {
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
      name: 'Download artwork automatically after the scan',
    })
    await user.click(switchArtes)

    await waitFor(() =>
      expect(secao.getByText('corpo invalido')).toBeInTheDocument(),
    )
  })

  it('reads the current config and seeds the automatic trailer/theme download switch (off)', async () => {
    const secao = await renderFormTrailerTema(fetchRouter())

    expect(
      secao.getByRole('switch', {
        name: 'Download trailer and theme automatically',
      }),
    ).not.toBeChecked()
  })

  it('seeds the trailer/theme switch on when the backend already returns enabled=true', async () => {
    const secao = await renderFormTrailerTema(
      fetchRouter({ autoDownloadTrailerTemaInicial: { enabled: true } }),
    )

    expect(
      secao.getByRole('switch', {
        name: 'Download trailer and theme automatically',
      }),
    ).toBeChecked()
  })

  it('saves the trailer/theme toggle via PUT when the switch is turned on', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter()
    const secao = await renderFormTrailerTema(fetchMock)

    const switchTrailerTema = secao.getByRole('switch', {
      name: 'Download trailer and theme automatically',
    })
    await user.click(switchTrailerTema)

    await waitFor(() => expect(switchTrailerTema).toBeChecked())
    expect(
      corpoDoPut(fetchMock, '/api/v1/settings/auto-download-trailer-tema'),
    ).toEqual({ enabled: true })
  })

  it('shows the server error message inline when the trailer/theme toggle PUT fails', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({
      putAutoDownloadTrailerTemaErro: {
        status: 400,
        code: 'invalid_body',
        message: 'corpo invalido',
      },
    })
    const secao = await renderFormTrailerTema(fetchMock)

    const switchTrailerTema = secao.getByRole('switch', {
      name: 'Download trailer and theme automatically',
    })
    await user.click(switchTrailerTema)

    await waitFor(() =>
      expect(secao.getByText('corpo invalido')).toBeInTheDocument(),
    )
  })

  it('reads the current yt-dlp status and shows the "downloaded automatically" badge', async () => {
    const secao = await renderFormYtdlp(fetchRouter())

    expect(
      secao.getByText('available (downloaded automatically)'),
    ).toBeInTheDocument()
    expect(secao.getByLabelText('yt-dlp custom binary path')).toHaveValue('')
  })

  it('shows the "unavailable" badge with the detail when the backend reports a failure', async () => {
    const secao = await renderFormYtdlp(
      fetchRouter({
        ytdlpInicial: {
          disponivel: false,
          origem: 'nenhum',
          custom_binary_path: null,
          detalhe: 'falha ao baixar: sem rede',
        },
      }),
    )

    expect(secao.getByText('unavailable')).toBeInTheDocument()
    expect(secao.getByText('falha ao baixar: sem rede')).toBeInTheDocument()
  })

  it('seeds the custom path field when one is already saved and shows the matching badge', async () => {
    const secao = await renderFormYtdlp(
      fetchRouter({
        ytdlpInicial: {
          disponivel: true,
          origem: 'custom',
          custom_binary_path: '/usr/local/bin/yt-dlp',
          detalhe: null,
        },
      }),
    )

    expect(secao.getByText('available (custom path)')).toBeInTheDocument()
    expect(secao.getByLabelText('yt-dlp custom binary path')).toHaveValue(
      '/usr/local/bin/yt-dlp',
    )
  })

  it('saves the custom path via PUT and confirms success', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({
      putYtdlpResposta: {
        disponivel: true,
        origem: 'custom',
        custom_binary_path: '/opt/yt-dlp',
        detalhe: null,
      },
    })
    const secao = await renderFormYtdlp(fetchMock)

    const input = secao.getByLabelText('yt-dlp custom binary path')
    await user.type(input, '/opt/yt-dlp')
    await user.click(secao.getByRole('button', { name: BOTAO_SALVAR }))

    await waitFor(() =>
      expect(secao.getByText('Settings saved.')).toBeInTheDocument(),
    )
    expect(corpoDoPut(fetchMock, '/api/v1/settings/ytdlp')).toEqual({
      custom_binary_path: '/opt/yt-dlp',
    })
  })

  it('sends custom_binary_path=null when the field is cleared', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({
      ytdlpInicial: {
        disponivel: true,
        origem: 'custom',
        custom_binary_path: '/opt/yt-dlp',
        detalhe: null,
      },
    })
    const secao = await renderFormYtdlp(fetchMock)

    const input = secao.getByLabelText('yt-dlp custom binary path')
    await user.clear(input)
    await user.click(secao.getByRole('button', { name: BOTAO_SALVAR }))

    await waitFor(() =>
      expect(secao.getByText('Settings saved.')).toBeInTheDocument(),
    )
    expect(corpoDoPut(fetchMock, '/api/v1/settings/ytdlp')).toEqual({
      custom_binary_path: null,
    })
  })

  it('shows the server error message inline when the yt-dlp PUT fails', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({
      putYtdlpErro: {
        status: 400,
        code: 'invalid_body',
        message: 'corpo invalido',
      },
    })
    const secao = await renderFormYtdlp(fetchMock)

    await user.click(secao.getByRole('button', { name: BOTAO_SALVAR }))

    await waitFor(() =>
      expect(secao.getByText('corpo invalido')).toBeInTheDocument(),
    )
  })

  it('shows the YouTube ToS risk warning', async () => {
    const secao = await renderFormYtdlp(fetchRouter())

    expect(secao.getByText(/YouTube Terms of Service/)).toBeInTheDocument()
  })
})
