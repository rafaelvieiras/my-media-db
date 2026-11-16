import { expect, test } from '../fixtures'
import { resetBiblioteca } from '../lib/reset'

// Block A: EXTERNAL journeys. The external detail of a movie and the external
// collection page, both importing to the INTERNAL side. Each test starts with the
// library cleared (reset through the API), so "Importar" is always available.

test.beforeEach(async ({ request }) => {
  await resetBiblioteca(request)
})

test('detalhe externo de um filme -> importar leva ao detalhe interno', async ({ page }) => {
  // Reloaded (604) has a rich external detail served by the fake.
  await page.goto('/external/tmdb/movie/604')
  await expect(page.getByRole('heading', { level: 1, name: 'Matrix Reloaded' })).toBeVisible()
  await expect(page.getByText('2003')).toBeVisible()
  await expect(page.getByText('Aventura')).toBeVisible() // genre exclusive to Reloaded

  await page.getByRole('button', { name: 'Importar' }).click()

  // Importing from the external page leads to the INTERNAL one (never offers the external one again).
  await expect(page).toHaveURL(/\/entities\//)
  await expect(page.getByRole('heading', { level: 1, name: 'Matrix Reloaded' })).toBeVisible()
})

test('colecao externa -> importar colecao cria entity com os membros', async ({ page }) => {
  await page.goto('/collection/tmdb/2344')
  await expect(page.getByRole('heading', { level: 1, name: 'The Matrix Collection' })).toBeVisible()
  // The parts show up in the grid.
  await expect(page.getByText('The Matrix', { exact: true })).toBeVisible()
  await expect(page.getByText('The Matrix Reloaded')).toBeVisible()

  await page.getByRole('button', { name: 'Importar coleção' }).click()

  // It becomes an entity type=collection with the member movies linked.
  await expect(page).toHaveURL(/\/entities\//)
  await expect(page.getByRole('heading', { level: 1, name: 'The Matrix Collection' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Itens da coleção' })).toBeVisible()
  // Members: Matrix and Matrix Reloaded (each with the remove-from-collection button).
  await expect(page.getByRole('button', { name: 'Remover Matrix da coleção' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Remover Matrix Reloaded da coleção' })).toBeVisible()
})
