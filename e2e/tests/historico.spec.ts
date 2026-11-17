import { expect, test } from '../fixtures'
import { resetBiblioteca } from '../lib/reset'

// CONSUMPTION HISTORY journey (ADR-0022) through the MANUAL path of the "Manage"
// dropdown: an item never watched shows neither a badge nor a section (empty
// state) -> "Mark as watched" lights up the badge on the hero and populates
// the timeline -> deleting the event from the timeline reverts both. Before this
// spec there was only Vitest unit coverage (historico-section.test.tsx) and the Go
// Layer 1 (internal/e2e); no real browser journey went through `entity-manage-menu.tsx`/
// `historico-section.tsx`.

test.beforeEach(async ({ request }) => {
  await resetBiblioteca(request)
})

test('marking as watched through Manage lights the badge and the timeline; deleting reverts', async ({
  page,
  request,
}) => {
  // Seeds the movie 603 (Matrix) through the API and opens the internal detail.
  const res = await request.post('/api/v1/import', {
    data: { source: 'tmdb', source_id: '603', type: 'movie' },
  })
  expect(res.ok()).toBeTruthy()
  const { id } = await res.json()
  await page.goto(`/entities/${id}`)
  const main = page.getByRole('main')
  await expect(
    page.getByRole('heading', { level: 1, name: 'Matrix' }),
  ).toBeVisible()

  // (1) Item never watched: neither the badge on the hero nor the "History"
  // section appear (empty state).
  await expect(main.getByText(/Watched on/)).toHaveCount(0)
  await expect(page.getByRole('heading', { name: 'History' })).toHaveCount(0)

  // (2) "Mark as watched" lives inside the "Manage" dropdown,
  // alongside the other actions of the item.
  await page.getByRole('button', { name: 'Manage' }).click()
  await page.getByRole('menuitem', { name: 'Mark as watched' }).click()
  await expect(page.getByText('Marked as watched.')).toBeVisible()

  // The "Watched on ..." badge lights up on the hero and the "History" section
  // starts listing the manual event just created (status "Watched", origin
  // "Recorded manually").
  await expect(main.getByText(/Watched on/)).toBeVisible()
  await expect(
    page.getByRole('heading', { name: 'History' }),
  ).toBeVisible()
  await expect(page.getByText('Watched', { exact: true })).toBeVisible()
  await expect(page.getByText('Recorded manually')).toBeVisible()

  // (3) Deletes the event from the timeline itself: TWO-step confirmation
  // (x -> "Delete?"), the same pattern as `MembroEditavel`.
  await page.getByRole('button', { name: 'Delete event' }).click()
  await page.getByRole('button', { name: 'Delete?' }).click()

  // Badge and section disappear entirely again (same empty state as the beginning).
  await expect(main.getByText(/Watched on/)).toHaveCount(0)
  await expect(page.getByRole('heading', { name: 'History' })).toHaveCount(0)
})
