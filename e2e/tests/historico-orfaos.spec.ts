import { expect, test } from '../fixtures'

// Global LIST (outside /entities/{id}) of webhook events that did not match any
// cataloged item -- read-only (the automatic backfill is the backend's
// responsibility, with no manual action on this screen). It does not depend on
// resetBiblioteca: actually populating an orphan event would require simulating a
// webhook with no match, which this spec does not try to reproduce -- it only
// validates that the screen renders without error from the sidebar link,
// against whatever real orphan state the test server has.

test('pagina de eventos sem item relacionado: navega pelo sidebar e carrega sem erro', async ({
  page,
}) => {
  await page.goto('/dashboard')
  await page.getByRole('link', { name: 'Eventos sem item' }).click()
  await expect(page).toHaveURL(/\/dashboard\/historico-orfaos$/)
  await expect(
    page.getByRole('heading', {
      level: 1,
      name: 'Eventos sem item relacionado',
    }),
  ).toBeVisible()

  // The query resolves to one of the three possible states: empty, list with
  // items, or error -- never an eternal loading.
  const vazio = page.getByText('Nenhum evento sem item relacionado')
  const erro = page.getByText(
    'Não foi possível carregar os eventos sem item relacionado',
  )
  const listaComItens = page.getByRole('button', { name: 'Próxima' })
  await expect(vazio.or(erro).or(listaComItens)).toBeVisible()

  // An error is not an acceptable state here -- if the query fails, the test fails
  // explicitly instead of silently passing through the "or" above.
  await expect(erro).not.toBeVisible()
})

test('sidebar do dashboard marca o link Eventos sem item como ativo na rota /dashboard/historico-orfaos', async ({
  page,
}) => {
  await page.goto('/dashboard/historico-orfaos')
  const link = page
    .getByRole('navigation', { name: 'Administração' })
    .getByRole('link', { name: 'Eventos sem item' })
  await expect(link).toHaveAttribute('aria-current', 'page')
})
