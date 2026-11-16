import { expect, test } from '../fixtures'
import { resetBiblioteca } from '../lib/reset'

// LOCAL CREATION journeys: creating an item from scratch on the home page and the life
// cycle of a local collection (create -> add/remove member -> delete). Each
// test starts with the library cleared (reset through the API), so the deterministic
// UUIDs restart and the local search does not drag state from another journey.

test.beforeEach(async ({ request }) => {
  await resetBiblioteca(request)
})

test('criar item manual aparece na biblioteca', async ({ page }) => {
  await page.goto('/')

  // "Novo item" is now a LINK to the dedicated route `/new/item`; type already
  // comes = movie (Filme). The title label lost the "do item" suffix (it comes from the
  // descriptor: just "Título").
  await page.getByRole('link', { name: 'Novo item' }).click()
  await expect(page).toHaveURL(/\/new\/item$/)
  await page
    .getByRole('textbox', { name: 'Título', exact: true })
    .fill('Meu Filme Manual')
  await page.getByRole('button', { name: 'Criar item' }).click()

  // Creating from scratch navigates to the INTERNAL detail of the entity just created.
  await expect(page).toHaveURL(/\/entities\//)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Meu Filme Manual' }),
  ).toBeVisible()
})

test('nova colecao: criar, adicionar/remover membro e excluir', async ({
  page,
  request,
}) => {
  // Seeds the member movie (603 -> "Matrix" in pt-BR) through the API; the local search of the
  // picker only finds it after it is imported.
  const imp = await request.post('/api/v1/import', {
    data: { source: 'tmdb', source_id: '603', type: 'movie' },
  })
  expect(imp.ok()).toBeTruthy()

  // Creates the local collection from the home page: "Nova coleção" is a LINK to `/new/collection`.
  // The title label comes from the descriptor (just "Título", no suffix).
  await page.goto('/')
  await page.getByRole('link', { name: 'Nova coleção' }).click()
  await expect(page).toHaveURL(/\/new\/collection$/)
  await page
    .getByRole('textbox', { name: 'Título', exact: true })
    .fill('Minha Coleção')
  await page.getByRole('button', { name: 'Criar coleção' }).click()

  await expect(page).toHaveURL(/\/entities\//)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Minha Coleção' }),
  ).toBeVisible()
  await expect(page.getByText('Esta coleção ainda não tem itens.')).toBeVisible()

  // Adds the member: opens the picker, searches "Matrix" and chooses the local candidate
  // (labeled "title + type" -> "Matrix Filme"). The wait for the candidate covers the
  // search debounce (no sleep).
  await page.getByRole('button', { name: 'Adicionar item' }).click()
  await page
    .getByRole('searchbox', { name: 'Buscar item para adicionar' })
    .fill('Matrix')
  await page.getByRole('button', { name: 'Matrix Filme' }).click()

  // The member joins the list, with the remove control.
  const removerMatrix = page.getByRole('button', {
    name: 'Remover Matrix da coleção',
  })
  await expect(removerMatrix).toBeVisible()

  // Closes the picker and removes the member (TWO-step flow: x -> "Remover?").
  await page.getByRole('button', { name: 'Concluir' }).click()
  await removerMatrix.click()
  await page.getByRole('button', { name: 'Remover?' }).click()

  await expect(removerMatrix).toHaveCount(0)
  await expect(page.getByText('Esta coleção ainda não tem itens.')).toBeVisible()

  // Deletes the collection: a destructive item inside the "Gerenciar" dropdown opens
  // a confirmation AlertDialog — it does not delete right away when clicking the menu item.
  await page.getByRole('button', { name: 'Gerenciar' }).click()
  await page.getByRole('menuitem', { name: 'Excluir coleção' }).click()
  await page.getByRole('button', { name: 'Excluir coleção' }).click()

  // Leaves the collection screen and goes back to the library (home).
  await expect(
    page.getByRole('heading', { level: 1, name: 'Biblioteca' }),
  ).toBeVisible()

  // Deleting the collection does NOT delete the members: the movie "Matrix" still exists.
  const res = await request.get('/api/v1/entities?limit=1000')
  expect(res.ok()).toBeTruthy()
  const body = await res.json()
  const itens: Array<{ title: string; type: string }> = body.items ?? body ?? []
  expect(itens.some((e) => e.title === 'Matrix' && e.type === 'movie')).toBeTruthy()
})
