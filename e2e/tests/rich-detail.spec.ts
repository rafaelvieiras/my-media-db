import { expect, test } from '../fixtures'
import { resetBiblioteca } from '../lib/reset'

// RICH sections of RichDetailView (backend internal/richfields) that the
// other journeys do not assert yet: the ORIGINAL title from the source (distinct from the translated
// one), the original LANGUAGE resolved to a label (endonym), the LINK to the source (TMDB) and the GRACEFUL
// degradation of the sections without data. The Matrix franchise fake does not bring video/similar/collection
// in the movie detail (only title/synopsis/genres/language/link), so this block targets the
// rich fields that actually EXIST and locks the behavior of the empty sections.
//
// The screen is SHARED between the external item (before importing) and the imported entity; the
// last test checks that PARITY, exercising the adapter of the resolved entity
// (deResolvedEntity) in addition to the external detail (deExternalDetail).
//
// The default reading language of the Playwright context is pt-BR (as in the sibling journeys),
// so the translated title ("Matrix Reloaded") differs from the original ("The Matrix
// Reloaded") — the condition for the original title line to render; the interface chrome
// is in English (the default UI language).

test.beforeEach(async ({ request }) => {
  await resetBiblioteca(request)
})

test('rich external detail: original title, source language and link to the source', async ({
  page,
}) => {
  await page.goto('/external/tmdb/movie/604')
  const main = page.getByRole('main')

  await expect(
    page.getByRole('heading', { level: 1, name: 'Matrix Reloaded' }),
  ).toBeVisible()
  // ORIGINAL title (en) renders distinct from the translated title (pt-BR) in the hero.
  await expect(
    main.getByText('The Matrix Reloaded', { exact: true }),
  ).toBeVisible()
  // Language of the source ("en") resolved to its endonym "English" (outside the language
  // selector, which sits in the header — that is why the search is scoped to <main>).
  await expect(main.getByText('English', { exact: true })).toBeVisible()
  // Link to the SOURCE, with the real TMDB href (not a loose text).
  await expect(page.getByRole('link', { name: 'TMDB' })).toHaveAttribute(
    'href',
    'https://www.themoviedb.org/movie/604',
  )
  // No poster in the fixture (empty imageBase) => placeholder, never a broken image.
  await expect(page.getByTestId('poster-placeholder').first()).toBeVisible()
})

test('external detail: sections without data do not render (graceful degradation)', async ({
  page,
}) => {
  await page.goto('/external/tmdb/movie/603')

  await expect(
    page.getByRole('heading', { level: 1, name: 'Matrix' }),
  ).toBeVisible()
  // The rich sections WITH data appear. Genres became compact badges
  // near the title, without their own heading -- it checks the badge instead of the heading.
  // Synopsis moves INTO the hero, also without its own heading --
  // it checks the paragraph text.
  await expect(
    page.getByText('Um hacker descobre a verdadeira natureza da sua realidade.'),
  ).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Synopsis' })).toHaveCount(0)
  await expect(page.getByRole('heading', { name: 'Genres' })).toHaveCount(0)
  await expect(page.getByText('Ficcao cientifica')).toBeVisible()
  // The ones without data in the fake (videos/similars/collection/keywords) emit no header.
  for (const secao of ['Videos', 'Similar', 'Collection', 'Keywords']) {
    await expect(page.getByRole('heading', { name: secao })).toHaveCount(0)
  }
})

test('external<->internal parity: the imported entity shows the same rich screen', async ({
  page,
  request,
}) => {
  // Seeds the import through the API and opens the INTERNAL detail (same RichDetailView).
  const res = await request.post('/api/v1/import', {
    data: { source: 'tmdb', source_id: '604', type: 'movie' },
  })
  expect(res.ok()).toBeTruthy()
  const entity: { id: string } = await res.json()

  await page.goto(`/entities/${entity.id}`)
  const main = page.getByRole('main')

  await expect(
    page.getByRole('heading', { level: 1, name: 'Matrix Reloaded' }),
  ).toBeVisible()
  // The SAME rich fields of the external page, now served by the resolved entity.
  await expect(
    main.getByText('The Matrix Reloaded', { exact: true }),
  ).toBeVisible()
  await expect(main.getByText('English', { exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: 'TMDB' })).toHaveAttribute(
    'href',
    'https://www.themoviedb.org/movie/604',
  )
  // Genre exclusive to Reloaded (pt-BR metadata), confirming the resolution in the active reading language.
  await expect(page.getByText('Aventura')).toBeVisible()
})

test('downloading the trailer and the theme song materializes them on disk (mocked yt-dlp)', async ({
  page,
  request,
}) => {
  // The fake provider (internal/provider/fake) has NO video fixture, and the theme
  // candidate of ThemerrDB is never fetched live (only a pre-existing cache) -- both of them,
  // besides yt-dlp itself (which would really download the binary from the internet on the
  // 1st call without a mock), need to be injected through network interception to exercise the
  // "Download trailer"/"Download theme" buttons in a deterministic and offline way in CI.
  const res = await request.post('/api/v1/import', {
    data: { source: 'tmdb', source_id: '604', type: 'movie' },
  })
  expect(res.ok()).toBeTruthy()
  const entity: { id: string } = await res.json()

  await page.route(
    (url) => url.pathname === '/api/v1/settings/ytdlp',
    async (route) => {
      await route.fulfill({
        json: { disponivel: true, origem: 'baixado', custom_binary_path: null },
      })
    },
  )
  // Reuses the REAL response of the entity (title/genres/etc.) and only injects
  // `videos` -- that way the rest of the screen (already covered by the tests above) keeps
  // coming from the real fake provider.
  await page.route(
    (url) => url.pathname === `/api/v1/entities/${entity.id}`,
    async (route) => {
      if (route.request().method() !== 'GET') {
        await route.continue()
        return
      }
      const response = await route.fetch()
      const json = await response.json()
      json.videos = [
        {
          name: 'Trailer Oficial',
          key: 'dQw4w9WgXcQ',
          site: 'YouTube',
          type: 'Trailer',
          official: true,
          url: 'https://www.youtube.com/watch?v=dQw4w9WgXcQ',
        },
      ]
      await route.fulfill({ response, json })
    },
  )
  await page.route(
    (url) => url.pathname === `/api/v1/entities/${entity.id}/theme/candidato`,
    async (route) => {
      await route.fulfill({ json: { disponivel: true, ja_tem_local: false } })
    },
  )
  await page.route(
    (url) =>
      url.pathname === `/api/v1/entities/${entity.id}/trailer/download`,
    async (route) => {
      await route.fulfill({
        json: { local_path: '/biblioteca/Matrix Reloaded/trailer.mp4' },
      })
    },
  )
  await page.route(
    (url) => url.pathname === `/api/v1/entities/${entity.id}/theme/download`,
    async (route) => {
      await route.fulfill({
        json: { local_path: '/biblioteca/Matrix Reloaded/theme.mp3' },
      })
    },
  )

  await page.goto(`/entities/${entity.id}`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Matrix Reloaded' }),
  ).toBeVisible()

  // "Watch trailer" (embedded) stays the same; "Download trailer"
  // (ADR-0025) is purely additive next to it. Clicking it materializes the file and the button
  // becomes a "Trailer downloaded" link.
  await expect(
    page.getByRole('button', { name: 'Watch trailer' }),
  ).toBeVisible()
  const baixarTrailer = page.getByRole('button', { name: 'Download trailer' })
  await expect(baixarTrailer).toBeVisible()
  await baixarTrailer.click()
  await expect(page.getByRole('link', { name: 'Trailer downloaded' })).toBeVisible()

  // "Download theme" (ADR-0025): mocked ThemerrDB candidate available, with no
  // local theme yet -- clicking it materializes the file and the button disappears (no `force`,
  // it never offers a re-download).
  const baixarTema = page.getByRole('button', { name: 'Download theme' })
  await expect(baixarTema).toBeVisible()
  await baixarTema.click()
  await expect(baixarTema).toHaveCount(0)
})
