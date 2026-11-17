import { expect, test } from '../fixtures'
import { resetBiblioteca } from '../lib/reset'

// Block B: LANGUAGE journeys. The "Metadata language" section of the header
// language control re-resolves the title/tags of the displayed item (reading,
// client-side), while /settings exposes the catalog config (primary/fallback/allowed).
// Each test starts with the library cleared (reset through the API) and does not change the global config.

test.beforeEach(async ({ request }) => {
  await resetBiblioteca(request)
})

test('switching the reading language re-resolves the displayed title', async ({ page, request }) => {
  // Seeds Matrix (tmdb 603) and opens the internal detail.
  const res = await request.post('/api/v1/import', {
    data: { source: 'tmdb', source_id: '603', type: 'movie' },
  })
  expect(res.ok()).toBeTruthy()
  const { id } = await res.json()
  await page.goto(`/entities/${id}`)

  // The reading language lives in the header language control, under the
  // "Metadata language" section (the interface one is a separate section of the
  // same menu). The language names are endonyms.
  const abrirIdiomas = async (idioma: string) => {
    await page.getByRole('button', { name: 'Language' }).click()
    // .last(): the interface section lists the same endonyms first; the
    // metadata one is rendered after it.
    await page.getByRole('menuitemradio', { name: idioma }).last().click()
  }

  // pt-BR (default): title and tags resolved in Portuguese.
  await expect(page.getByRole('heading', { level: 1, name: 'Matrix' })).toBeVisible()
  await expect(page.getByText('Ficcao cientifica')).toBeVisible()

  // English: re-resolves to the English title/tag.
  await abrirIdiomas('English')
  await expect(page.getByRole('heading', { level: 1, name: 'The Matrix' })).toBeVisible()
  await expect(page.getByText('Science Fiction')).toBeVisible()

  // Back to pt-BR: title/tag go back to Portuguese.
  await abrirIdiomas('Português (Brasil)')
  await expect(page.getByRole('heading', { level: 1, name: 'Matrix' })).toBeVisible()
  await expect(page.getByText('Ficcao cientifica')).toBeVisible()
})

test('language settings page', async ({ page }) => {
  // Settings lives in the sidebar of the administrative area,
  // no longer in the "/" header -- the real route is /dashboard/settings.
  await page.goto('/dashboard')
  await page.getByRole('link', { name: 'Settings' }).click()
  await expect(page).toHaveURL(/\/dashboard\/settings$/)

  await expect(page.getByRole('heading', { level: 1, name: 'Settings' })).toBeVisible()

  // Scope: the languages form (the page also has the cataloging completeness form,
  // which has its own "Save" button).
  const formIdiomas = page.locator('form').filter({ hasText: 'Primary language' })

  // The controls reflect the current catalog config: pt-BR primary, en fallback.
  await expect(page.getByLabel('Primary language')).toHaveValue('pt-BR')
  await expect(page.getByLabel('Fallback language')).toHaveValue('en')

  // Allowed languages: pt-BR and en checked; a non-allowed one (es) unchecked.
  await expect(page.getByRole('checkbox', { name: 'Português (Brasil) pt-BR' })).toBeChecked()
  await expect(page.getByRole('checkbox', { name: 'English en' })).toBeChecked()
  await expect(page.getByRole('checkbox', { name: 'Español es' })).not.toBeChecked()

  await expect(formIdiomas.getByRole('button', { name: 'Save' })).toBeVisible()
})

test('retroactive enrichment settings page (global throttle)', async ({
  page,
  request,
}) => {
  // "Retroactive enrichment" section: GLOBAL throttle that a library without its own
  // override uses. Same page as the languages, separate form (its own "Save" button).
  await page.goto('/dashboard/settings')
  await expect(page.getByRole('heading', { level: 1, name: 'Settings' })).toBeVisible()

  const formRetroativo = page.locator('form').filter({ hasText: 'Retry after (days)' })
  const input = formRetroativo.getByLabel('Retry after (days)')

  // Global default: 15 days.
  await expect(input).toHaveValue('15')

  // Client-side validation (mirrors the backend, [1, 365]): a value outside the
  // range shows the inline message and does NOT trigger the PUT.
  await input.fill('400')
  await formRetroativo.getByRole('button', { name: 'Save' }).click()
  await expect(
    formRetroativo.getByText('Enter a whole number between 1 and 365 days.'),
  ).toBeVisible()
  const resNaoDisparado = await request.get('/api/v1/settings/enrichment-retroativo')
  expect(await resNaoDisparado.json()).toEqual({ retry_days: 15 })

  await input.fill('20')
  await formRetroativo.getByRole('button', { name: 'Save' }).click()
  await expect(formRetroativo.getByText('Settings saved.')).toBeVisible()

  // It really persisted: the API reflects the new value.
  const res = await request.get('/api/v1/settings/enrichment-retroativo')
  expect(await res.json()).toEqual({ retry_days: 20 })

  // Restores the default: the value is GLOBAL and the single worker (workers=1)
  // shares the database with the other journeys of the file -- the same spirit
  // as the reading language above, which goes back to pt-BR at the end.
  await input.fill('15')
  await formRetroativo.getByRole('button', { name: 'Save' }).click()
  await expect(formRetroativo.getByText('Settings saved.')).toBeVisible()
})

test('yt-dlp card shows the status and allows configuring a custom path', async ({
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
  // With no custom path (initial mock), the badge shows "unavailable" (exact
  // match: the detail below the badge also mentions it).
  await expect(
    secaoYtdlp.getByText('unavailable', { exact: true }),
  ).toBeVisible()

  const input = page.getByLabel('yt-dlp custom binary path')
  await input.fill('/usr/local/bin/yt-dlp')
  await secaoYtdlp.getByRole('button', { name: 'Save' }).click()

  await expect(secaoYtdlp.getByText('Settings saved.')).toBeVisible()
  // Custom path saved: the badge reflects "available (custom path)".
  await expect(
    secaoYtdlp.getByText('available (custom path)'),
  ).toBeVisible()
})
