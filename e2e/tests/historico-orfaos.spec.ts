import { expect, test } from '../fixtures'

// Global LIST (outside /entities/{id}) of webhook events that did not match any
// cataloged item -- read-only (the automatic backfill is the backend's
// responsibility, with no manual action on this screen). It does not depend on
// resetBiblioteca: actually populating an orphan event would require simulating a
// webhook with no match, which this spec does not try to reproduce -- it only
// validates that the screen renders without error from the sidebar link,
// against whatever real orphan state the test server has.

test('events without a related item page: navigate through the sidebar and load without error', async ({
  page,
}) => {
  await page.goto('/dashboard')
  await page.getByRole('link', { name: 'Events without item' }).click()
  await expect(page).toHaveURL(/\/dashboard\/historico-orfaos$/)
  await expect(
    page.getByRole('heading', {
      level: 1,
      name: 'Events without a related item',
    }),
  ).toBeVisible()

  // The query resolves to one of the three possible states: empty, list with
  // items, or error -- never an eternal loading.
  const vazio = page.getByText('No event without a related item')
  const erro = page.getByText(
    'Could not load the events without a related item',
  )
  const listaComItens = page.getByRole('button', { name: 'Next' })
  await expect(vazio.or(erro).or(listaComItens)).toBeVisible()

  // An error is not an acceptable state here -- if the query fails, the test fails
  // explicitly instead of silently passing through the "or" above.
  await expect(erro).not.toBeVisible()
})

test('dashboard sidebar marks the Events without item link as active on the /dashboard/historico-orfaos route', async ({
  page,
}) => {
  await page.goto('/dashboard/historico-orfaos')
  const link = page
    .getByRole('navigation', { name: 'Administration' })
    .getByRole('link', { name: 'Events without item' })
  await expect(link).toHaveAttribute('aria-current', 'page')
})
