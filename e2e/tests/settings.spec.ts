import { expect, test } from '../fixtures'
import { resetBiblioteca } from '../lib/reset'

// Bloco B (briefing 0028): jornadas de IDIOMA. O seletor "Idioma de leitura" do
// header re-resolve titulo/tags do item exibido (leitura, client-side), enquanto
// /settings expoe a config do catalogo (principal/fallback/permitidos). Cada teste
// comeca com a biblioteca zerada (reset por API) e nao muda a config global.

test.beforeEach(async ({ request }) => {
  await resetBiblioteca(request)
})

test('trocar o idioma de leitura re-resolve o titulo exibido', async ({ page, request }) => {
  // Semeia Matrix (tmdb 603) e abre o detalhe interno.
  const res = await request.post('/api/v1/import', {
    data: { source: 'tmdb', source_id: '603', type: 'movie' },
  })
  expect(res.ok()).toBeTruthy()
  const { id } = await res.json()
  await page.goto(`/entities/${id}`)

  const idioma = page.getByLabel('Idioma de leitura')

  // pt-BR (default): titulo e tags resolvidos em portugues.
  await expect(page.getByRole('heading', { level: 1, name: 'Matrix' })).toBeVisible()
  await expect(page.getByText('Ficcao cientifica')).toBeVisible()

  // Ingles: re-resolve para o titulo/tag em ingles.
  await idioma.selectOption('en')
  await expect(page.getByRole('heading', { level: 1, name: 'The Matrix' })).toBeVisible()
  await expect(page.getByText('Science Fiction')).toBeVisible()

  // Volta para pt-BR: titulo/tag voltam ao portugues.
  await idioma.selectOption('pt-BR')
  await expect(page.getByRole('heading', { level: 1, name: 'Matrix' })).toBeVisible()
  await expect(page.getByText('Ficcao cientifica')).toBeVisible()
})

test('pagina de configuracoes de idioma', async ({ page }) => {
  // Configurações mora na sidebar da área administrativa (briefing 0083),
  // não mais no header de "/" -- rota real é /dashboard/settings.
  await page.goto('/dashboard')
  await page.getByRole('link', { name: 'Configurações' }).click()
  await expect(page).toHaveURL(/\/dashboard\/settings$/)

  await expect(page.getByRole('heading', { level: 1, name: 'Configurações' })).toBeVisible()

  // Escopo: o form de idiomas (a pagina tambem tem o form de completude de
  // catalogacao do briefing 0057, que tem seu proprio botao "Salvar").
  const formIdiomas = page.locator('form').filter({ hasText: 'Idioma principal' })

  // Os controles refletem a config atual do catalogo: pt-BR principal, en fallback.
  await expect(page.getByLabel('Idioma principal')).toHaveValue('pt-BR')
  await expect(page.getByLabel('Idioma de fallback')).toHaveValue('en')

  // Idiomas permitidos: pt-BR e en marcados; um nao-permitido (es) desmarcado.
  await expect(page.getByRole('checkbox', { name: 'Português (BR) pt-BR' })).toBeChecked()
  await expect(page.getByRole('checkbox', { name: 'Inglês en' })).toBeChecked()
  await expect(page.getByRole('checkbox', { name: 'Espanhol es' })).not.toBeChecked()

  await expect(formIdiomas.getByRole('button', { name: 'Salvar' })).toBeVisible()
})
