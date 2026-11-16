import { expect, test } from '../fixtures'
import { resetBiblioteca } from '../lib/reset'

// CONSUMPTION HISTORY journey (ADR-0022) through the MANUAL path of the "Gerenciar"
// dropdown: an item never watched shows neither a badge nor a section (empty
// state) -> "Marcar como assistido" lights up the badge on the hero and populates
// the timeline -> deleting the event from the timeline reverts both. Before this
// spec there was only Vitest unit coverage (historico-section.test.tsx) and the Go
// Layer 1 (internal/e2e); no real browser journey went through `entity-manage-menu.tsx`/
// `historico-section.tsx`.

test.beforeEach(async ({ request }) => {
  await resetBiblioteca(request)
})

test('marcar assistido pelo Gerenciar acende o selo e a timeline; apagar reverte', async ({
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

  // (1) Item never watched: neither the badge on the hero nor the "Historico"
  // section appear (empty state).
  await expect(main.getByText(/Assistido em/)).toHaveCount(0)
  await expect(page.getByRole('heading', { name: 'Histórico' })).toHaveCount(0)

  // (2) "Marcar como assistido" lives inside the "Gerenciar" dropdown,
  // alongside the other actions of the item.
  await page.getByRole('button', { name: 'Gerenciar' }).click()
  await page.getByRole('menuitem', { name: 'Marcar como assistido' }).click()
  await expect(page.getByText('Marcado como assistido.')).toBeVisible()

  // The "Assistido em ..." badge lights up on the hero and the "Historico" section
  // starts listing the manual event just created (status "Assistiu", origin
  // "Registrado manualmente").
  await expect(main.getByText(/Assistido em/)).toBeVisible()
  await expect(
    page.getByRole('heading', { name: 'Histórico' }),
  ).toBeVisible()
  await expect(page.getByText('Assistiu', { exact: true })).toBeVisible()
  await expect(page.getByText('Registrado manualmente')).toBeVisible()

  // (3) Deletes the event from the timeline itself: TWO-step confirmation
  // (x -> "Apagar?"), the same pattern as `MembroEditavel`.
  await page.getByRole('button', { name: 'Apagar evento' }).click()
  await page.getByRole('button', { name: 'Apagar?' }).click()

  // Badge and section disappear entirely again (same empty state as the beginning).
  await expect(main.getByText(/Assistido em/)).toHaveCount(0)
  await expect(page.getByRole('heading', { name: 'Histórico' })).toHaveCount(0)
})
