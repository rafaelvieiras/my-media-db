import { expect, test } from '../fixtures'
import { resetBiblioteca } from '../lib/reset'

// MUSIC journey (Layer 2, ADR-0029): imports an
// artist through the API (the same seed pattern used by the other journeys — the UI
// has no music search of its own yet) and completes the album through the external
// "missing tracks" screen, driven by the real browser. It uses the SAME music
// fake as the Layer 1 Go E2E (internal/e2e/journeys_musica_test.go):
// fake.Musicbrainz (internal/provider/fake/musicbrainz.go), registered in the
// binary when MYMEDIADB_FAKE_PROVIDER=1 — the fake artist has 1 album with 2
// official tracks, but the imported tree brings only the 1st one (on purpose, so the
// 2nd one shows up as "missing").
const ARTISTA_MBID = 'artista-fake-1'
const ALBUM_MBID = 'album-fake-1'

test.beforeEach(async ({ request }) => {
  await resetBiblioteca(request)
})

test('album: tela de faixas faltando importa a faixa que ficou de fora da arvore', async ({
  page,
  request,
}) => {
  const impArtista = await request.post('/api/v1/import', {
    data: { source: 'musicbrainz', source_id: ARTISTA_MBID, type: 'artista' },
  })
  expect(impArtista.ok()).toBeTruthy()
  const artista = await impArtista.json()

  const membrosResp = await request.get(
    `/api/v1/entities/${artista.id}/members`,
  )
  const membros = await membrosResp.json()
  expect(membros).toHaveLength(1)
  const albumId = membros[0].id as string

  // Internal album screen: the tracklist shows the only track already imported and the
  // "Ver faixas faltando" link (it only appears with external_ids.mbid).
  await page.goto(`/entities/${albumId}`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Album Fake' }),
  ).toBeVisible()
  await expect(page.getByText('Faixa Um')).toBeVisible()

  await page
    .getByRole('link', { name: /Ver faixas faltando/ })
    .click()
  await expect(page).toHaveURL(`/external/musicbrainz/album/${ALBUM_MBID}`)

  // The external screen lists the 2 OFFICIAL tracks of the release: the 1st one already
  // imported ("na biblioteca"), the 2nd one with an Importar button.
  await expect(
    page.getByRole('heading', { level: 1, name: 'Album Fake' }),
  ).toBeVisible()
  const faixaUm = page.getByText('Faixa Um').locator('..')
  await expect(faixaUm.getByText('na biblioteca')).toBeVisible()
  const faixaDois = page.getByText('Faixa Dois').locator('..')
  await expect(
    faixaDois.getByRole('button', { name: 'Importar' }),
  ).toBeVisible()

  await faixaDois.getByRole('button', { name: 'Importar' }).click()

  // Importing the standalone track leads to its INTERNAL detail, linked to the parent album.
  await expect(page).toHaveURL(/\/entities\//)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Faixa Dois' }),
  ).toBeVisible()

  // The album now has the 2 tracks linked.
  const membrosDepoisResp = await request.get(
    `/api/v1/entities/${albumId}/members`,
  )
  const membrosDepois = await membrosDepoisResp.json()
  expect(membrosDepois).toHaveLength(2)
})
