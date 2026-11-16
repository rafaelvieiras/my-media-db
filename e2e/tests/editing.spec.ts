import { expect, test } from '../fixtures'
import { resetBiblioteca } from '../lib/reset'

// Journeys of EDITING the INTERNAL item. The focus is the core guarantee of
// ADR-0012: the owner override is a diff and survives a re-enrichment; plus the
// external identity cycle: pin `external_ids` and pull the source data by the
// id just pinned. Each test starts with the library cleared (reset through the
// API), seeds its own state and navigates to the internal detail.

test.beforeEach(async ({ request }) => {
  await resetBiblioteca(request)
})

test('editar override nao e destrutivo apos re-enriquecer', async ({
  page,
  request,
}) => {
  // Seeds the movie 603 (Matrix) by importing through the API and goes to the internal detail.
  const res = await request.post('/api/v1/import', {
    data: { source: 'tmdb', source_id: '603', type: 'movie' },
  })
  expect(res.ok()).toBeTruthy()
  const { id } = await res.json()
  await page.goto(`/entities/${id}`)
  await expect(page.getByRole('heading', { level: 1, name: 'Matrix' })).toBeVisible()

  // "Editar" is now a LINK to the `/entities/$id/edit` route, no longer an inline
  // editor, and it lives inside the "Gerenciar" dropdown. On the edit page, edit in
  // pt-BR: new title, remove "Acao" and add "Favorito".
  await page.getByRole('button', { name: 'Gerenciar' }).click()
  await page.getByRole('menuitem', { name: 'Editar' }).click()
  await expect(page).toHaveURL(new RegExp(`/entities/${id}/edit$`))
  await page.getByRole('textbox', { name: 'Título', exact: true }).fill('Matrix EDITADO')
  await page.getByRole('button', { name: 'Remover tag Acao' }).click()
  const campoTag = page.getByRole('textbox', { name: 'Adicionar tag' })
  await campoTag.fill('Favorito')
  await campoTag.press('Enter')
  await page.getByRole('button', { name: 'Salvar alterações' }).click()

  // Confirms that the override round-tripped (the save finished) before re-enriching.
  await expect(
    page.getByRole('heading', { level: 1, name: 'Matrix EDITADO' }),
  ).toBeVisible()

  // There is no direct enrich button on the detail (only "Salvar e re-enriquecer", which
  // is coupled to the external IDs). Triggers the re-enrichment through the API and reloads.
  const enrich = await request.post(`/api/v1/entities/${id}/enrich`, { data: {} })
  expect(enrich.ok()).toBeTruthy()
  await page.reload()

  // The override survived the enrich (ADR-0012): title preserved; "Favorito"
  // (owner add) and "Ficcao cientifica" (external base) present; "Acao"
  // (owner remove) is gone — the source would offer "Acao" again, but the remove wins.
  await expect(
    page.getByRole('heading', { level: 1, name: 'Matrix EDITADO' }),
  ).toBeVisible()
  await expect(page.getByText('Favorito', { exact: true })).toBeVisible()
  await expect(page.getByText('Ficcao cientifica', { exact: true })).toBeVisible()
  await expect(page.getByText('Acao', { exact: true })).toHaveCount(0)
})

test('item manual: fixar external-ids e re-enriquecer puxa dados da fonte', async ({
  page,
  request,
}) => {
  // Creates a manual item (no external_ids and no source data) and goes to the detail.
  const res = await request.post('/api/v1/entities', {
    data: { type: 'movie', title: 'Meu Filme Local' },
  })
  expect(res.ok()).toBeTruthy()
  const { id } = await res.json()
  await page.goto(`/entities/${id}`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Meu Filme Local' }),
  ).toBeVisible()

  // "Editar" is a LINK to the `/entities/$id/edit` route, now with a sidebar of
  // sections: the external IDs editor lives in the "IDs de terceiros" subroute
  // (`/entities/$id/edit/ids`). It lives inside the "Gerenciar" dropdown. It
  // pins the source tmdb=603.
  await page.getByRole('button', { name: 'Gerenciar' }).click()
  await page.getByRole('menuitem', { name: 'Editar' }).click()
  await expect(page).toHaveURL(new RegExp(`/entities/${id}/edit$`))
  await page.getByRole('link', { name: 'IDs de terceiros' }).click()
  await expect(page).toHaveURL(new RegExp(`/entities/${id}/edit/ids$`))
  await page.getByRole('button', { name: 'Adicionar fonte' }).click()
  await page.getByRole('textbox', { name: 'Fonte do id externo' }).fill('tmdb')
  // The aria-label of the id becomes "Id na fonte tmdb" after the source is typed.
  await page.getByRole('textbox', { name: /^Id na fonte/ }).fill('603')
  await page.getByRole('button', { name: 'Salvar e re-enriquecer' }).click()

  // Waits for the enrich to round-trip: goes back to "Informações básicas" and checks that
  // the form reflects the new title resolved from the source in the placeholder of the
  // Título field (proof that the data of source 603 was pulled).
  await page.getByRole('link', { name: 'Informações básicas' }).click()
  await expect(
    page.getByRole('textbox', { name: 'Título', exact: true }),
  ).toHaveAttribute('placeholder', 'Matrix')
  await page.goto(`/entities/${id}`)

  // The enrich through the id just pinned pulls the data of source 603 (Matrix): the title
  // and the genre now reflect the movie from the source.
  await expect(page.getByRole('heading', { level: 1, name: 'Matrix' })).toBeVisible()
  await expect(page.getByText('Ficcao cientifica', { exact: true })).toBeVisible()
})
