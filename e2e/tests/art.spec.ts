import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { expect, test } from '../fixtures'
import { resetBiblioteca } from '../lib/reset'

// Art life cycle of the entity (later updated for the lightbox gallery and for
// ownership): the owner uploads a poster and removes it. Here the entity only
// exists through an API import (no real media_file), so "Set as
// principal"/"Save to folder" stay disabled (owned=false) -- the flow of
// making it primary with a real folder is already covered by the next test.
// Each test starts with the library cleared; only content assertions,
// web-first (no sleeps).

test.beforeEach(async ({ request }) => {
  await resetBiblioteca(request)
})

// Valid 1x1 PNG (magic bytes the backend accepts) for the upload.
const PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==',
  'base64',
)

test('artwork: upload, prefer and remove in the entity gallery', async ({
  page,
  request,
}) => {
  // Imports 603 through the API (POST /import returns the entity with .id) and opens the detail.
  const imp = await request.post('/api/v1/import', {
    data: { source: 'tmdb', source_id: '603', type: 'movie' },
  })
  expect(imp.ok()).toBeTruthy()
  const { id } = (await imp.json()) as { id: string }

  await page.goto(`/entities/${id}`)

  // Scope: the "Artwork" section (the chrome follows the interface language, which is English).
  const arte = page
    .locator('section')
    .filter({ has: page.getByRole('heading', { name: 'Artwork', level: 2 }) })
  await expect(arte.getByRole('heading', { name: 'Artwork' })).toBeVisible()

  // The gallery starts empty.
  await expect(arte.getByText('No artwork available yet.')).toBeVisible()

  // Uploads a poster. The kind already comes as "Poster" by default; the
  // "Upload artwork" button triggers a hidden <input type=file>, so we feed the input
  // directly (setInputFiles works on a hidden input and exercises the same
  // onChange/upload).
  await expect(
    arte.getByRole('combobox', { name: 'Artwork type' }),
  ).toHaveValue('poster')
  await arte
    .getByLabel('Artwork file')
    .setInputFiles({ name: 'poster.png', mimeType: 'image/png', buffer: PNG })

  // The gallery is no longer empty and shows 1 card for the poster just uploaded.
  await expect(arte.getByText('No artwork available yet.')).toHaveCount(0)
  const cardEnviado = arte.getByRole('button', { name: /^View Poster \d+$/ })
  await expect(cardEnviado).toHaveCount(1)

  // Opens the lightbox: with no real media_file (owned=false), "Set as
  // principal" is disabled with a tooltip; "Remove artwork" (owner action) is enabled.
  await cardEnviado.click()
  const definirPrincipal = page.getByRole('button', {
    name: 'Set as principal',
  })
  await expect(definirPrincipal).toBeVisible()
  await expect(definirPrincipal).toBeDisabled()
  await expect(page.getByText('principal', { exact: true })).toHaveCount(0)

  // Remove: direct action (no confirmation). Closes the lightbox and the gallery
  // goes back to the empty state.
  await page.getByRole('button', { name: 'Remove artwork' }).click()
  await expect(arte.getByText('No artwork available yet.')).toBeVisible()
  await expect(
    arte.getByRole('button', { name: /^View Poster \d+$/ }),
  ).toHaveCount(0)
})

// leSrcDoPosterDoTopo reads the `src` of the HERO poster of RichDetailView, or
// `null` when there is only the placeholder (no primary art yet) — a local helper
// because the test below needs to compare BEFORE/AFTER regardless of whether the
// fake provider already has an initial art (MYMEDIADB_FAKE_IMAGE_BASE is not set
// in e2e, so the fake TMDB poster usually does not even exist — see global-setup.ts).
async function leSrcDoPosterDoTopo(
  page: import('@playwright/test').Page,
): Promise<string | null> {
  const img = page.getByRole('img', { name: /^Poster of/ })
  if ((await img.count()) === 0) {
    return null
  }
  return img.getAttribute('src')
}

test('setting an artwork as principal materializes it in the folder and updates the top poster', async ({
  page,
  request,
}) => {
  // Materialize/make-primary requires a real 'present' media_file (the API writes
  // into the REAL folder) -- so it seeds a /tmp folder with a parseable movie and
  // scans it, like in library.spec.ts, instead of importing only through the API.
  const dir = mkdtempSync(join(tmpdir(), 'mmdb-e2e-art-'))
  let id = ''
  try {
    writeFileSync(join(dir, 'The Matrix (1999).mkv'), Buffer.alloc(2048, 7))

    await page.goto('/dashboard/library')
    await page.getByRole('link', { name: 'Add datasource' }).click()
    await page.getByLabel('Datasource path').fill(dir)
    await page.getByRole('button', { name: 'Add datasource' }).click()
    await expect(page).toHaveURL('/dashboard/library')
    await expect(page.getByText(dir)).toBeVisible()
    await page.getByRole('button', { name: 'Scan' }).click()

    await expect
      .poll(
        async () => {
          const body = await (
            await request.get('/api/v1/entities?limit=1000')
          ).json()
          const itens: Array<{ id: string; title: string }> =
            body.items ?? body ?? []
          const achado = itens.find((e) => e.title === 'The Matrix')
          id = achado?.id ?? ''
          return !!achado
        },
        { timeout: 20_000 },
      )
      .toBe(true)

    await page.goto(`/entities/${id}`)
    const srcAntes = await leSrcDoPosterDoTopo(page)

    // Uploads an owner poster (the kind already comes as "Poster" by default) -- it
    // becomes a NON-primary candidate in the gallery, distinct from any poster of the
    // fake provider.
    const arte = page
      .locator('section')
      .filter({ has: page.getByRole('heading', { name: 'Artwork', level: 2 }) })
    await arte
      .getByLabel('Artwork file')
      .setInputFiles({ name: 'poster.png', mimeType: 'image/png', buffer: PNG })
    await expect(
      arte.getByRole('button', { name: /^View Poster \d+$/ }).last(),
    ).toBeVisible()

    // Opens the card just uploaded and sets it as primary -- direct prefer PUT (it
    // already is an asset, without needing the chaining of creating it through
    // /artwork/select first; that chaining is exercised in the unit tests of the
    // component).
    await arte.getByRole('button', { name: /^View Poster \d+$/ }).last().click()
    await page.getByRole('button', { name: 'Set as principal' }).click()
    await expect(page.getByText('principal', { exact: true })).toBeVisible()
    await expect(
      page.getByText('saved in the folder', { exact: true }),
    ).toBeVisible()
    await page.keyboard.press('Escape')

    // The TOP poster of RichDetailView reflects the new primary art, with no
    // page reload (acceptance criterion of the artwork cycle).
    await expect
      .poll(() => leSrcDoPosterDoTopo(page))
      .not.toBe(srcAntes)
  } finally {
    if (id) {
      await request.delete(`/api/v1/entities/${id}`).catch(() => {})
    }
    const dss = await (await request.get('/api/v1/library')).json()
    for (const s of dss.items ?? []) {
      await request.delete(`/api/v1/library/${s.id}`)
    }
    rmSync(dir, { recursive: true, force: true })
  }
})
