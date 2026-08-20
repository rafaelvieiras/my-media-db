import { expect, test } from '../fixtures'
import { resetBiblioteca } from '../lib/reset'

// Jornadas de NAVEGADOR (Camada 2, briefing 0028): um Chromium real dirige a SPA
// servida pelo binario com o provider fake. Poucas jornadas de valor, web-first
// assertions (sem sleep), seletores por papel/aria/texto. workers=1: compartilham
// um banco; 603 (The Matrix) e importado, 604 (Reloaded) fica de fora de proposito
// para provar o interno-first (importado x nao-importado lado a lado).

// matrix localiza o card do The Matrix (1999) na lista de resultados; reloaded, o
// do Reloaded (2003). O ano desambigua ("The Matrix" e prefixo de "The Matrix
// Reloaded").
const seletores = {
  busca: { name: 'Buscar título' as const },
}

// Cada jornada comeca com a biblioteca zerada (independencia de ordem).
test.beforeEach(async ({ request }) => {
  await resetBiblioteca(request)
})

test('importa um filme pela busca e abre o detalhe interno resolvido', async ({ page }) => {
  await page.goto('/search')
  await page.getByRole('searchbox', seletores.busca).fill('matrix')

  const matrix = page
    .getByRole('listitem')
    .filter({ hasText: 'The Matrix' })
    .filter({ hasText: '1999' })
  await matrix.getByRole('button', { name: 'Importar' }).click()

  // Importar leva ao detalhe INTERNO (nunca ao externo) e resolve em pt-BR.
  await expect(page).toHaveURL(/\/entities\//)
  await expect(page.getByRole('heading', { level: 1, name: 'Matrix', exact: true })).toBeVisible()
  await expect(page.getByText('1999')).toBeVisible()
  await expect(page.getByText('Ficcao cientifica')).toBeVisible()
})

test('item ja importado aparece como "Na biblioteca" e linka ao interno', async ({ page, request }) => {
  // Semeia o import por API (independente da ordem das jornadas): reusa a mesma
  // entity se ja existir (dedup), garantindo 603 na biblioteca.
  const imp = await request.post('/api/v1/import', {
    data: { source: 'tmdb', source_id: '603', type: 'movie' },
  })
  expect(imp.ok()).toBeTruthy()

  await page.goto('/search')
  await page.getByRole('searchbox', seletores.busca).fill('matrix')

  const matrix = page.getByRole('listitem').filter({ hasText: '1999' })
  await expect(matrix.getByText('Na biblioteca')).toBeVisible()
  await expect(matrix.getByRole('button', { name: 'Importar' })).toHaveCount(0)
  await expect(matrix.getByRole('link')).toHaveAttribute('href', /\/entities\//)

  // O nao-importado (Reloaded, 2003) segue oferecendo Importar e linkando ao externo.
  const reloaded = page.getByRole('listitem').filter({ hasText: '2003' })
  await expect(reloaded.getByRole('button', { name: 'Importar' })).toBeVisible()
})

test('a biblioteca lista o filme importado', async ({ page, request }) => {
  await request.post('/api/v1/import', { data: { source_id: '603', type: 'movie' } })

  // Desde o briefing 0114/ADR-0030 a home (`/`) só mostra os carrosséis de
  // "adicionados recentemente" (owned=true, ver RecentlyAddedRail) — o item
  // deste teste é só CATALOGADO (import sem arquivo/posse real), então não
  // aparece ali por desenho. A listagem completa (owned + não-owned, mesmo
  // comportamento que a home tinha antes do 0114) virou a rota própria
  // `/library/:entitySlug`; "movies" é o slug de `movie` (lib/library-slugs.ts).
  await page.goto('/library/movies')
  await expect(page.getByRole('heading', { level: 1, name: 'Filmes' })).toBeVisible()
  // O card do Matrix aparece no catalogo, linkando ao detalhe interno.
  await expect(page.getByRole('link', { name: /Matrix/ }).first()).toBeVisible()
})
