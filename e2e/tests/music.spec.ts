import { expect, test } from '../fixtures'
import { resetBiblioteca } from '../lib/reset'

// Jornada de MÚSICA (Camada 2, briefing 0115 Parte 2, ADR-0029): importa um
// artista pela API (mesmo padrão de seed usado pelas outras jornadas — a UI
// não tem busca de música própria ainda) e completa o álbum pela tela externa
// de "faixas faltando", dirigida pelo navegador real. Usa o MESMO fake de
// música que o Go E2E da Camada 1 (internal/e2e/journeys_musica_test.go):
// fake.Musicbrainz (internal/provider/fake/musicbrainz.go), registrado no
// binário quando MYMEDIADB_FAKE_PROVIDER=1 — o artista fake tem 1 álbum com 2
// faixas oficiais, mas a árvore importada só traz a 1ª (de propósito, para a
// 2ª aparecer como "faltando").
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

  // Tela do álbum interno: a tracklist mostra a única faixa já importada e o
  // link "Ver faixas faltando" (só aparece com external_ids.mbid, 0115 Parte 2).
  await page.goto(`/entities/${albumId}`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Album Fake' }),
  ).toBeVisible()
  await expect(page.getByText('Faixa Um')).toBeVisible()

  await page
    .getByRole('link', { name: /Ver faixas faltando/ })
    .click()
  await expect(page).toHaveURL(`/external/musicbrainz/album/${ALBUM_MBID}`)

  // A tela externa lista as 2 faixas OFICIAIS do release: a 1ª já importada
  // ("na biblioteca"), a 2ª com botão Importar.
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

  // Importar a faixa avulsa leva ao detalhe INTERNO dela, ligada ao álbum pai.
  await expect(page).toHaveURL(/\/entities\//)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Faixa Dois' }),
  ).toBeVisible()

  // O álbum agora tem as 2 faixas ligadas.
  const membrosDepoisResp = await request.get(
    `/api/v1/entities/${albumId}/members`,
  )
  const membrosDepois = await membrosDepoisResp.json()
  expect(membrosDepois).toHaveLength(2)
})
