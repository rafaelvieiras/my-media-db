import { expect, test } from '../fixtures'
import { resetBiblioteca } from '../lib/reset'

// Bloco A (briefing 0028): jornadas do EXTERNO. Detalhe externo de um filme e a
// pagina de colecao externa, ambos importando para o INTERNO. Cada teste comeca com
// a biblioteca zerada (reset por API), entao "Importar" esta sempre disponivel.

test.beforeEach(async ({ request }) => {
  await resetBiblioteca(request)
})

test('detalhe externo de um filme -> importar leva ao detalhe interno', async ({ page }) => {
  // Reloaded (604) tem detalhe externo rico servido pelo fake.
  await page.goto('/external/tmdb/movie/604')
  await expect(page.getByRole('heading', { level: 1, name: 'Matrix Reloaded' })).toBeVisible()
  await expect(page.getByText('2003')).toBeVisible()
  await expect(page.getByText('Aventura')).toBeVisible() // genero exclusivo do Reloaded

  await page.getByRole('button', { name: 'Importar' }).click()

  // Importar do externo leva ao INTERNO (nunca reoferece o externo).
  await expect(page).toHaveURL(/\/entities\//)
  await expect(page.getByRole('heading', { level: 1, name: 'Matrix Reloaded' })).toBeVisible()
})

test('colecao externa -> importar colecao cria entity com os membros', async ({ page }) => {
  await page.goto('/collection/tmdb/2344')
  await expect(page.getByRole('heading', { level: 1, name: 'The Matrix Collection' })).toBeVisible()
  // As partes aparecem no grid.
  await expect(page.getByText('The Matrix', { exact: true })).toBeVisible()
  await expect(page.getByText('The Matrix Reloaded')).toBeVisible()

  await page.getByRole('button', { name: 'Importar coleção' }).click()

  // Vira uma entity type=collection com os filmes membros ligados.
  await expect(page).toHaveURL(/\/entities\//)
  await expect(page.getByRole('heading', { level: 1, name: 'The Matrix Collection' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Itens da coleção' })).toBeVisible()
  // Membros: Matrix e Matrix Reloaded (cada um com o botao de remover da colecao).
  await expect(page.getByRole('button', { name: 'Remover Matrix da coleção' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Remover Matrix Reloaded da coleção' })).toBeVisible()
})
