import { expect, test } from '../fixtures'

// Second half of the Integrations epic (ADR-0023): full journey of
// `/dashboard/integrations` -- list with the `configurada` badge + form per TMDB
// schema. The e2e server starts with the FAKE provider
// (MYMEDIADB_FAKE_PROVIDER=1, global-setup.ts) and WITHOUT
// MYMEDIADB_TMDB_TOKEN/MYMEDIADB_TMDB_API_KEY -- so the 2 TMDB fields
// start with `origem:"nenhum"` (no caption), never `"env"`; the origin caption
// "env" (with the variable name) is covered by the component test
// (`integration-form.test.tsx`), not here. It does not depend on `resetBiblioteca`
// (integration_config is a separate domain, outside `entity`) -- it restores the
// state at the end (clears the token) in the same spirit as the retroactive
// enrichment test in settings.spec.ts, since the single worker shares the
// database with the other journeys of the file.

test('full journey: list, configure, clear and the badge reflects each step', async ({
  page,
}) => {
  await page.goto('/dashboard')
  await page.getByRole('link', { name: 'Integrations' }).click()
  await expect(page).toHaveURL(/\/dashboard\/integrations$/)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Integrations' }),
  ).toBeVisible()

  // TMDB card, initial badge "Not configured" (no token/api_key
  // saved yet -- the fake provider does not populate integration_config).
  const cardTmdb = page.getByRole('link', { name: /TMDB/ })
  await expect(cardTmdb).toBeVisible()
  await expect(cardTmdb.getByText('Not configured')).toBeVisible()

  await cardTmdb.click()
  await expect(page).toHaveURL(/\/dashboard\/integrations\/tmdb$/)
  await expect(
    page.getByRole('heading', { level: 1, name: 'TMDB' }),
  ).toBeVisible()

  // exact:true -- by default Playwright's getByLabel matches by
  // substring, and the show/hide button ("Show Read Access Token
  // (v4)") also contains the field label as a substring.
  const campoToken = page.getByLabel('Read Access Token (v4)', {
    exact: true,
  })
  const campoApiKey = page.getByLabel('API Key (v3)', { exact: true })
  await expect(campoToken).toBeVisible()
  await expect(campoApiKey).toBeVisible()

  // origem:"nenhum" -- empty field, no "Set in the panel." caption and no Clear
  // button (nothing saved in the database yet to remove).
  await expect(campoToken).toHaveValue('')
  await expect(page.getByText(/^Set (?:in the panel|by )/)).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Clear' })).toHaveCount(0)

  // Secret field: masked by default, the show/hide toggle works.
  await expect(campoToken).toHaveAttribute('type', 'password')
  await page
    .getByRole('button', { name: 'Show Read Access Token (v4)' })
    .click()
  await expect(campoToken).toHaveAttribute('type', 'text')
  await page
    .getByRole('button', { name: 'Hide Read Access Token (v4)' })
    .click()
  await expect(campoToken).toHaveAttribute('type', 'password')

  // Fills the token and saves -- static restart toast.
  await campoToken.fill('token-de-teste-e2e')
  await page.getByRole('button', { name: 'Save' }).click()
  await expect(
    page.getByText('Saved. Restart the server (docker restart) to apply.'),
  ).toBeVisible()

  // After the write: it NEVER rehydrates the secret field with the typed value --
  // it comes back empty, but now with origem:"banco" (caption + Clear button appear).
  await expect(campoToken).toHaveValue('')
  await expect(page.getByText('Set in the panel.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Clear' })).toBeVisible()
  // api_key stays untouched (origem:"nenhum" -- the PUT was partial, token only).
  await expect(campoApiKey).toHaveValue('')

  // Back to the list: the badge already reflects configurada:true, without
  // restarting the server (only the PROVIDER itself depends on the restart,
  // not the reading of the listing).
  await page.getByRole('link', { name: 'Back to integrations' }).click()
  await expect(page).toHaveURL(/\/dashboard\/integrations$/)
  await expect(page.getByRole('link', { name: /TMDB/ }).getByText('Configured')).toBeVisible()

  // Back to the detail and click Clear: it sends null, the field goes back to
  // origem:"nenhum" (no env var set in the e2e process) -- the caption and the
  // Clear button disappear, and the badge in the list goes back to "Not configured".
  await page.getByRole('link', { name: /TMDB/ }).click()
  await expect(page).toHaveURL(/\/dashboard\/integrations\/tmdb$/)
  await page.getByRole('button', { name: 'Clear' }).click()
  await page.getByRole('button', { name: 'Save' }).click()
  await expect(
    page.getByText('Saved. Restart the server (docker restart) to apply.'),
  ).toBeVisible()
  await expect(page.getByText(/^Set (?:in the panel|by )/)).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Clear' })).toHaveCount(0)

  await page.getByRole('link', { name: 'Back to integrations' }).click()
  await expect(
    page.getByRole('link', { name: /TMDB/ }).getByText('Not configured'),
  ).toBeVisible()
})

test('unknown slug shows the error state', async ({ page }) => {
  await page.goto('/dashboard/integrations/nao-existe')
  await expect(
    page.getByText('Could not load the integration'),
  ).toBeVisible()
  await expect(page.getByText('integração desconhecida')).toBeVisible()
})

test('dashboard sidebar marks the Integrations link as active on the /dashboard/integrations route', async ({
  page,
}) => {
  await page.goto('/dashboard/integrations')
  const link = page
    .getByRole('navigation', { name: 'Administration' })
    .getByRole('link', { name: 'Integrations' })
  await expect(link).toHaveAttribute('aria-current', 'page')
})
