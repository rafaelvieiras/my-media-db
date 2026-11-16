import { expect, test } from '../fixtures'
import { resetBiblioteca } from '../lib/reset'

// Block B: LANGUAGE journeys. The "Idioma de leitura" selector of the
// header re-resolves the title/tags of the displayed item (reading, client-side), while
// /settings exposes the catalog config (primary/fallback/allowed). Each test
// starts with the library cleared (reset through the API) and does not change the global config.

test.beforeEach(async ({ request }) => {
  await resetBiblioteca(request)
})

test('trocar o idioma de leitura re-resolve o titulo exibido', async ({ page, request }) => {
  // Seeds Matrix (tmdb 603) and opens the internal detail.
  const res = await request.post('/api/v1/import', {
    data: { source: 'tmdb', source_id: '603', type: 'movie' },
  })
  expect(res.ok()).toBeTruthy()
  const { id } = await res.json()
  await page.goto(`/entities/${id}`)

  const idioma = page.getByLabel('Idioma de leitura')

  // pt-BR (default): title and tags resolved in Portuguese.
  await expect(page.getByRole('heading', { level: 1, name: 'Matrix' })).toBeVisible()
  await expect(page.getByText('Ficcao cientifica')).toBeVisible()

  // English: re-resolves to the English title/tag.
  await idioma.selectOption('en')
  await expect(page.getByRole('heading', { level: 1, name: 'The Matrix' })).toBeVisible()
  await expect(page.getByText('Science Fiction')).toBeVisible()

  // Back to pt-BR: title/tag go back to Portuguese.
  await idioma.selectOption('pt-BR')
  await expect(page.getByRole('heading', { level: 1, name: 'Matrix' })).toBeVisible()
  await expect(page.getByText('Ficcao cientifica')).toBeVisible()
})

test('pagina de configuracoes de idioma', async ({ page }) => {
  // Configurações lives in the sidebar of the administrative area,
  // no longer in the "/" header -- the real route is /dashboard/settings.
  await page.goto('/dashboard')
  await page.getByRole('link', { name: 'Configurações' }).click()
  await expect(page).toHaveURL(/\/dashboard\/settings$/)

  await expect(page.getByRole('heading', { level: 1, name: 'Configurações' })).toBeVisible()

  // Scope: the languages form (the page also has the cataloging completeness form,
  // which has its own "Salvar" button).
  const formIdiomas = page.locator('form').filter({ hasText: 'Idioma principal' })

  // The controls reflect the current catalog config: pt-BR primary, en fallback.
  await expect(page.getByLabel('Idioma principal')).toHaveValue('pt-BR')
  await expect(page.getByLabel('Idioma de fallback')).toHaveValue('en')

  // Allowed languages: pt-BR and en checked; a non-allowed one (es) unchecked.
  await expect(page.getByRole('checkbox', { name: 'Português (BR) pt-BR' })).toBeChecked()
  await expect(page.getByRole('checkbox', { name: 'Inglês en' })).toBeChecked()
  await expect(page.getByRole('checkbox', { name: 'Espanhol es' })).not.toBeChecked()

  await expect(formIdiomas.getByRole('button', { name: 'Salvar' })).toBeVisible()
})

test('pagina de configuracoes de enriquecimento retroativo (throttle global)', async ({
  page,
  request,
}) => {
  // "Enriquecimento retroativo" section: GLOBAL throttle that a library without its own
  // override uses. Same page as the languages, separate form (its own "Salvar" button).
  await page.goto('/dashboard/settings')
  await expect(page.getByRole('heading', { level: 1, name: 'Configurações' })).toBeVisible()

  const formRetroativo = page.locator('form').filter({ hasText: 'Repetir tentativa após (dias)' })
  const input = formRetroativo.getByLabel('Repetir tentativa após (dias)')

  // Global default: 15 days.
  await expect(input).toHaveValue('15')

  // Client-side validation (mirrors the backend, [1, 365]): a value outside the
  // range shows the inline message and does NOT trigger the PUT.
  await input.fill('400')
  await formRetroativo.getByRole('button', { name: 'Salvar' }).click()
  await expect(
    formRetroativo.getByText('Informe um número inteiro entre 1 e 365 dias.'),
  ).toBeVisible()
  const resNaoDisparado = await request.get('/api/v1/settings/enrichment-retroativo')
  expect(await resNaoDisparado.json()).toEqual({ retry_days: 15 })

  await input.fill('20')
  await formRetroativo.getByRole('button', { name: 'Salvar' }).click()
  await expect(formRetroativo.getByText('Configurações salvas.')).toBeVisible()

  // It really persisted: the API reflects the new value.
  const res = await request.get('/api/v1/settings/enrichment-retroativo')
  expect(await res.json()).toEqual({ retry_days: 20 })

  // Restores the default: the value is GLOBAL and the single worker (workers=1)
  // shares the database with the other journeys of the file -- the same spirit
  // as the reading language above, which goes back to pt-BR at the end.
  await input.fill('15')
  await formRetroativo.getByRole('button', { name: 'Salvar' }).click()
  await expect(formRetroativo.getByText('Configurações salvas.')).toBeVisible()
})

test('card yt-dlp mostra status e permite configurar path customizado', async ({
  page,
}) => {
  // The real yt-dlp (internal/ytdlp) downloads the binary from the internet (GitHub
  // API) on the 1st call without a custom path -- mocks GET/PUT
  // /settings/ytdlp to keep the journey deterministic and offline, without
  // depending on an external network in CI (ADR-0025).
  let pathAtual: string | null = null
  await page.route('**/api/v1/settings/ytdlp', async (route) => {
    const req = route.request()
    if (req.method() === 'PUT') {
      const body = req.postDataJSON() as { custom_binary_path: string | null }
      pathAtual = body.custom_binary_path
    }
    await route.fulfill({
      json: {
        disponivel: pathAtual !== null,
        origem: pathAtual !== null ? 'custom' : 'nenhum',
        custom_binary_path: pathAtual,
        detalhe: pathAtual !== null ? null : 'yt-dlp indisponível (mock e2e)',
      },
    })
  })

  await page.goto('/dashboard/settings')
  const secaoYtdlp = page.locator('section').filter({ hasText: 'yt-dlp' })

  await expect(
    page.getByRole('heading', { level: 2, name: 'yt-dlp' }),
  ).toBeVisible()
  // With no custom path (initial mock), the badge shows "indisponível" (exact
  // match: the detail below the badge also contains the substring).
  await expect(
    secaoYtdlp.getByText('indisponível', { exact: true }),
  ).toBeVisible()

  const input = page.getByLabel('Path customizado do binário yt-dlp')
  await input.fill('/usr/local/bin/yt-dlp')
  await secaoYtdlp.getByRole('button', { name: 'Salvar' }).click()

  await expect(secaoYtdlp.getByText('Configurações salvas.')).toBeVisible()
  // Custom path saved: the badge reflects "disponível (path customizado)".
  await expect(
    secaoYtdlp.getByText('disponível (path customizado)'),
  ).toBeVisible()
})
