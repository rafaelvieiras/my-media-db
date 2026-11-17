import { expect, test } from '../fixtures'
import { resetBiblioteca } from '../lib/reset'

// BROWSER journeys (Layer 2): a real Chromium drives the SPA
// served by the binary with the fake provider. Few journeys of value, web-first
// assertions (no sleep), selectors by role/aria/text. workers=1: they share
// one database; 603 (The Matrix) is imported, 604 (Reloaded) is left out on purpose
// to prove internal-first (imported x not-imported side by side).

// matrix locates the card of The Matrix (1999) in the results list; reloaded, the
// one of Reloaded (2003). The year disambiguates ("The Matrix" is a prefix of "The Matrix
// Reloaded").
const seletores = {
  busca: { name: 'Search title' as const },
}

// Each journey starts with the library cleared (order independence).
test.beforeEach(async ({ request }) => {
  await resetBiblioteca(request)
})

test('imports a movie through the search and opens the resolved internal detail', async ({ page }) => {
  await page.goto('/search')
  await page.getByRole('searchbox', seletores.busca).fill('matrix')

  const matrix = page
    .getByRole('listitem')
    .filter({ hasText: 'The Matrix' })
    .filter({ hasText: '1999' })
  await matrix.getByRole('button', { name: 'Import' }).click()

  // Importing leads to the INTERNAL detail (never the external one) and resolves in pt-BR.
  await expect(page).toHaveURL(/\/entities\//)
  await expect(page.getByRole('heading', { level: 1, name: 'Matrix', exact: true })).toBeVisible()
  await expect(page.getByText('1999')).toBeVisible()
  await expect(page.getByText('Ficcao cientifica')).toBeVisible()
})

test('an already imported item shows up as "In the library" and links to the internal one', async ({ page, request }) => {
  // Seeds the import through the API (independent of the journey order): reuses the same
  // entity if it already exists (dedup), guaranteeing 603 in the library.
  const imp = await request.post('/api/v1/import', {
    data: { source: 'tmdb', source_id: '603', type: 'movie' },
  })
  expect(imp.ok()).toBeTruthy()

  await page.goto('/search')
  await page.getByRole('searchbox', seletores.busca).fill('matrix')

  const matrix = page.getByRole('listitem').filter({ hasText: '1999' })
  await expect(matrix.getByText('In the library')).toBeVisible()
  await expect(matrix.getByRole('button', { name: 'Import' })).toHaveCount(0)
  await expect(matrix.getByRole('link')).toHaveAttribute('href', /\/entities\//)

  // The not-imported one (Reloaded, 2003) still offers Import and links to the external page.
  const reloaded = page.getByRole('listitem').filter({ hasText: '2003' })
  await expect(reloaded.getByRole('button', { name: 'Import' })).toBeVisible()

  // Type filter (ADR-0030): dynamic, it lists the REAL top types
  // with cataloged data (here, only "Movies" — 603 was already imported
  // above). Choosing "Movies" exercises the type-switch path
  // (MediaTypeFilter.onChange -> refilters `/search?type=movie`), and Matrix
  // stays visible (it is, in fact, a movie).
  await page.getByLabel('Filter by type').selectOption({ label: 'Movies' })
  await expect(matrix.getByText('In the library')).toBeVisible()
})

test('the library lists the imported movie', async ({ page, request }) => {
  await request.post('/api/v1/import', { data: { source_id: '603', type: 'movie' } })

  // Since ADR-0030 the home page (`/`) only shows the "recently added"
  // carousels (owned=true, see RecentlyAddedRail) — the item
  // of this test is only CATALOGED (import without a file/real ownership), so it does not
  // appear there by design. The complete listing (owned + non-owned, the same
  // behavior the home page had before) became its own route
  // `/library/:entitySlug`, reachable from the "Library" dropdown of the navbar
  // (`app-header.tsx`/`BibliotecaNavItem`) — used here instead of navigating
  // straight by URL, to exercise that path as well.
  await page.goto('/')
  await page
    .getByRole('button', { name: 'Filter the library by type' })
    .click()
  await page.getByRole('menuitem', { name: 'Movies' }).click()

  await expect(page).toHaveURL('/library/movies')
  await expect(page.getByRole('heading', { level: 1, name: 'Movies' })).toBeVisible()
  // The Matrix card appears in the catalog, linking to the internal detail.
  await expect(page.getByRole('link', { name: /Matrix/ }).first()).toBeVisible()

  // Ownership filter (PosterGrid): switching to "In the library" refilters
  // (owned=true) and Matrix disappears (only CATALOGED, without a file/real ownership) —
  // it exercises the `reiniciarPagina(setPosse)` handler and the empty state.
  await page.getByLabel('Filter by ownership').selectOption('owned')
  await expect(
    page.getByText('No items in the catalog'),
  ).toBeVisible()

  // Going back to "Outside the library" shows Matrix again (owned=false).
  await page.getByLabel('Filter by ownership').selectOption('not')
  await expect(page.getByRole('link', { name: /Matrix/ }).first()).toBeVisible()
})
