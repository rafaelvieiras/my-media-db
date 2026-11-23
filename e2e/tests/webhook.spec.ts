import { expect, test } from '../fixtures'

// Block B: journey for configuring the token of the Jellyfin inbound webhook -- it
// closes the hole the consumption history work left behind (neither of the
// two built this UI, each one pushed it to the other). It does not depend on the
// library state (the token lives in app_settings, outside the entity
// domain), so it does not call resetBiblioteca; it is idempotent regarding the
// initial state of the token (it works both on the first visit, with no token
// generated at all, and when re-running against a server already used in fast
// iteration mode through E2E_BASE_URL).

test('webhook page: generates, shows, copies and regenerates the Jellyfin token', async ({
  page,
  context,
}) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])

  await page.goto('/dashboard')
  await page.getByRole('link', { name: 'Webhook' }).click()
  await expect(page).toHaveURL(/\/dashboard\/webhook$/)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Webhook' }),
  ).toBeVisible()

  // Waits for the query to resolve: either the empty state or the field with a token appears.
  const semToken = page.getByText('No token generated yet.')
  const campoToken = page.getByLabel('Webhook token')
  await expect(semToken.or(campoToken)).toBeVisible()

  if (await semToken.isVisible()) {
    await page.getByRole('button', { name: 'Generate token' }).click()
  }
  await expect(campoToken).toBeVisible()

  // A JUST-generated token starts visible on purpose (the owner needs to be
  // able to copy it without an extra click on "Show"); a
  // pre-existing token loaded from the backend starts masked. Normalizes to the
  // masked state before testing the toggle, so that the test holds in both
  // cases.
  const botaoOcultar = page.getByRole('button', { name: 'Hide token' })
  if (await botaoOcultar.isVisible()) {
    await botaoOcultar.click()
  }
  await expect(campoToken).toHaveAttribute('type', 'password')
  const tokenAtual = await campoToken.inputValue()
  expect(tokenAtual.length).toBeGreaterThan(0)

  // Show/hide.
  await page.getByRole('button', { name: 'Show token' }).click()
  await expect(campoToken).toHaveAttribute('type', 'text')
  await page.getByRole('button', { name: 'Hide token' }).click()
  await expect(campoToken).toHaveAttribute('type', 'password')

  // Copy token: checks that the right value really landed in the
  // clipboard (permission granted above).
  await page.getByRole('button', { name: 'Copy token' }).click()
  await expect(page.getByRole('button', { name: 'Copied!' })).toBeVisible()
  const tokenCopiado = await page.evaluate(() => navigator.clipboard.readText())
  expect(tokenCopiado).toBe(tokenAtual)

  // Copy webhook URL: it contains the token in the querystring.
  await page.getByRole('button', { name: 'Copy webhook URL' }).click()
  const urlCopiada = await page.evaluate(() => navigator.clipboard.readText())
  expect(urlCopiada).toContain(`/api/v1/webhooks/jellyfin?token=${tokenAtual}`)

  // Regenerate: the confirmation is the product's own AlertDialog (a `window.confirm`
  // can be suppressed by the browser and cannot say which token gets invalidated).
  // Cancelling first proves nothing is written; then it confirms and the token changes.
  await page.getByRole('button', { name: 'Generate new token' }).click()
  const confirmacao = page.getByRole('alertdialog')
  await expect(confirmacao).toBeVisible()
  await expect(confirmacao).toContainText(
    'Generating a new token invalidates the current one',
  )
  await confirmacao.getByRole('button', { name: 'Cancel' }).click()
  await expect(confirmacao).toBeHidden()
  await expect(campoToken).toHaveValue(tokenAtual)

  await page.getByRole('button', { name: 'Generate new token' }).click()
  await confirmacao.getByRole('button', { name: 'Regenerate token' }).click()
  await expect(campoToken).not.toHaveValue(tokenAtual)

  // Outbound placeholder always visible, without depending on any API.
  await expect(
    page.getByRole('heading', { name: 'Outbound webhooks' }),
  ).toBeVisible()
  await expect(
    page.getByText('No outbound webhook available yet.'),
  ).toBeVisible()
})

test('dashboard sidebar marks the Webhook link as active on the /dashboard/webhook route', async ({
  page,
}) => {
  await page.goto('/dashboard/webhook')
  const link = page
    .getByRole('navigation', { name: 'Administration' })
    .getByRole('link', { name: 'Webhook' })
  await expect(link).toHaveAttribute('aria-current', 'page')
})
