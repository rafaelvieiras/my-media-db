import { expect, test } from '../fixtures'
import { resetBiblioteca } from '../lib/reset'

// ERROR PATH journeys: they guarantee that the UI shows the error from the envelope
// INLINE (in pt-BR) and that the entity is NOT corrupted when the source does not
// deliver. Each test starts with the library cleared (reset through the API),
// seeds its own state and navigates to the internal detail.
//
// About the 502 ("source unavailable"): the fake provider of the harness is ALWAYS
// up and has NO unavailability injection (neither an env flag nor a "magic" id — see
// internal/provider/fake/fake.go). But the 502 is reachable TODAY, WITHOUT touching
// the fake: the enrich handler turns ANY source failure into 502 (respondFonteIndisponivel,
// internal/server/enrich.go), and the fake returns ErrNaoEncontrado for a source_id that
// is not in the catalog. So "pin tmdb=<nonexistent id> and re-enrich" exercises
// the 502 status + the external_source_error envelope for real — the source cannot
// deliver the detail of that id. What remains as a FOLLOW-UP is TRANSPORT
// unavailability (timeout/connection refused / source down): that would require a
// fault-injection hook in the fake (env flag or an id that forces 5xx) + a server
// rebuild, which this spec deliberately does NOT do (another agent depends on :8793).

test.beforeEach(async ({ request }) => {
  await resetBiblioteca(request)
})

test('re-enriquecer sem id da fonte mostra 422 inline e nao corrompe a entity', async ({
  page,
  request,
}) => {
  // Manual item: it is born WITHOUT external_ids and without source data.
  const res = await request.post('/api/v1/entities', {
    data: { type: 'movie', title: 'Item Sem Fonte' },
  })
  expect(res.ok()).toBeTruthy()
  const { id } = await res.json()
  await page.goto(`/entities/${id}`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Item Sem Fonte' }),
  ).toBeVisible()

  // "Editar" is a LINK to `/entities/$id/edit`, now with a sidebar of
  // sections: the external IDs editor lives in the "IDs de terceiros" subroute.
  // It lives inside the "Gerenciar" dropdown. Triggers "Salvar e re-enriquecer"
  // WITHOUT pinning any external id: it saves external_ids (empty) and tries to enrich;
  // without source_id the backend responds 422 (invalid_body) and the UI shows the message
  // from the envelope inline.
  await page.getByRole('button', { name: 'Gerenciar' }).click()
  await page.getByRole('menuitem', { name: 'Editar' }).click()
  await expect(page).toHaveURL(new RegExp(`/entities/${id}/edit$`))
  await page.getByRole('link', { name: 'IDs de terceiros' }).click()
  await expect(page).toHaveURL(new RegExp(`/entities/${id}/edit/ids$`))
  await expect(page.getByText('Nenhum id externo definido.')).toBeVisible()
  await page.getByRole('button', { name: 'Salvar e re-enriquecer' }).click()

  // INLINE error (role=alert) with the real pt-BR message of the 422 envelope.
  await expect(page.getByRole('alert')).toContainText(
    'sem source_id: informe no corpo ou tenha external_ids[tmdb] na entity',
  )

  // The entity was NOT corrupted: the manual title remains and no source data
  // leaked. Goes back to the detail to prove it still exists (the 422 did not delete it).
  await page.goto(`/entities/${id}`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Item Sem Fonte' }),
  ).toBeVisible()

  // And in fact no external_id was written (the enrich failed BEFORE any
  // source data; the item remains purely local).
  const depois = await request.get(`/api/v1/entities/${id}`)
  expect(depois.ok()).toBeTruthy()
  const body = await depois.json()
  expect(body.external_ids).toEqual({})
  expect(body.title).toBe('Item Sem Fonte')
})

test('re-enriquecer com id que a fonte nao entrega mostra 502 inline e preserva o titulo', async ({
  page,
  request,
}) => {
  // Manual item; we pin tmdb=999999 (outside the fake catalog). The enrich pulls the
  // detail, the fake returns "not found" and the handler maps the source failure
  // to 502 (external_source_error) — the "source does not deliver the detail" journey.
  const res = await request.post('/api/v1/entities', {
    data: { type: 'movie', title: 'Item Fonte Indisponivel' },
  })
  expect(res.ok()).toBeTruthy()
  const { id } = await res.json()
  await page.goto(`/entities/${id}`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Item Fonte Indisponivel' }),
  ).toBeVisible()

  // "Editar" is a LINK to `/entities/$id/edit`, now with a sidebar of
  // sections: the external IDs editor lives in the "IDs de terceiros" subroute.
  // It lives inside the "Gerenciar" dropdown.
  await page.getByRole('button', { name: 'Gerenciar' }).click()
  await page.getByRole('menuitem', { name: 'Editar' }).click()
  await expect(page).toHaveURL(new RegExp(`/entities/${id}/edit$`))
  await page.getByRole('link', { name: 'IDs de terceiros' }).click()
  await expect(page).toHaveURL(new RegExp(`/entities/${id}/edit/ids$`))
  await page.getByRole('button', { name: 'Adicionar fonte' }).click()
  await page.getByRole('textbox', { name: 'Fonte do id externo' }).fill('tmdb')
  // The aria-label of the id becomes "Id na fonte tmdb" after the source is typed.
  await page.getByRole('textbox', { name: /^Id na fonte/ }).fill('999999')
  await page.getByRole('button', { name: 'Salvar e re-enriquecer' }).click()

  // INLINE error (role=alert) with the real pt-BR message of the 502 envelope.
  await expect(page.getByRole('alert')).toContainText(
    'a fonte externa falhou ao entregar o detalhe',
  )

  // The entity was NOT corrupted: the manual title remains (the source delivered
  // nothing to overwrite). Goes back to the detail to prove it still exists.
  await page.goto(`/entities/${id}`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Item Fonte Indisponivel' }),
  ).toBeVisible()
})

test('rota de detalhe com id inexistente mostra 404 inline', async ({ page }) => {
  // Navigates straight to an entity that does not exist: the GET responds 404 and the
  // detail screen falls into the error state (role=alert) with the envelope message.
  await page.goto('/entities/00000000-0000-0000-0000-000000000000')

  const alerta = page.getByRole('alert')
  await expect(alerta).toContainText('Não foi possível carregar o item')
  await expect(alerta).toContainText('entidade não encontrada')
})

test('import com type invalido responde 422 com envelope pt-BR', async ({
  request,
}) => {
  // VALIDATION error path with no dedicated screen (the import UI never sends an
  // invalid type): it guarantees the contract of the 422 envelope straight on the API. Valid
  // source_id, type outside movie|tv|collection|artista|faixa (the music work added
  // artista/faixa to the accepted enum and the corresponding message).
  const res = await request.post('/api/v1/import', {
    data: { source: 'tmdb', source_id: '603', type: 'banana' },
  })
  expect(res.status()).toBe(422)
  const body = await res.json()
  expect(body.error.code).toBe('invalid_body')
  expect(body.error.message).toBe(
    'type inválido (use movie, tv, collection, artista ou faixa)',
  )
})
