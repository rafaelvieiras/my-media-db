import { expect, test } from '../fixtures'
import { resetBiblioteca } from '../lib/reset'

// Jornada do HISTORICO DE CONSUMO (briefings 0092/0093/0094, ADR-0022) pelo
// caminho MANUAL do dropdown "Gerenciar" (0086): item nunca assistido nao
// mostra selo nem secao (estado vazio) -> "Marcar como assistido" acende o
// selo no hero e populada a timeline -> apagar o evento da timeline reverte
// os dois. Antes desta spec so havia cobertura Vitest de unidade
// (historico-section.test.tsx) e a Camada 1 Go (internal/e2e); nenhuma
// jornada de navegador real passava por `entity-manage-menu.tsx`/
// `historico-section.tsx`.

test.beforeEach(async ({ request }) => {
  await resetBiblioteca(request)
})

test('marcar assistido pelo Gerenciar acende o selo e a timeline; apagar reverte', async ({
  page,
  request,
}) => {
  // Semeia o filme 603 (Matrix) via API e abre o detalhe interno.
  const res = await request.post('/api/v1/import', {
    data: { source: 'tmdb', source_id: '603', type: 'movie' },
  })
  expect(res.ok()).toBeTruthy()
  const { id } = await res.json()
  await page.goto(`/entities/${id}`)
  const main = page.getByRole('main')
  await expect(
    page.getByRole('heading', { level: 1, name: 'Matrix' }),
  ).toBeVisible()

  // (1) Item nunca assistido: nem o selo no hero nem a secao "Historico"
  // aparecem (estado vazio, briefing 0094).
  await expect(main.getByText(/Assistido em/)).toHaveCount(0)
  await expect(page.getByRole('heading', { name: 'Histórico' })).toHaveCount(0)

  // (2) "Marcar como assistido" mora dentro do dropdown "Gerenciar" (0086),
  // junto das demais acoes do item.
  await page.getByRole('button', { name: 'Gerenciar' }).click()
  await page.getByRole('menuitem', { name: 'Marcar como assistido' }).click()
  await expect(page.getByText('Marcado como assistido.')).toBeVisible()

  // O selo "Assistido em ..." acende no hero e a secao "Historico" passa a
  // listar o evento manual recem-criado (status "Assistiu", origem
  // "Registrado manualmente").
  await expect(main.getByText(/Assistido em/)).toBeVisible()
  await expect(
    page.getByRole('heading', { name: 'Histórico' }),
  ).toBeVisible()
  await expect(page.getByText('Assistiu', { exact: true })).toBeVisible()
  await expect(page.getByText('Registrado manualmente')).toBeVisible()

  // (3) Apaga o evento pela propria timeline: confirmacao de DUAS etapas
  // (x -> "Apagar?"), mesmo padrao de `MembroEditavel`.
  await page.getByRole('button', { name: 'Apagar evento' }).click()
  await page.getByRole('button', { name: 'Apagar?' }).click()

  // Selo e secao voltam a sumir por inteiro (mesmo estado vazio do inicio).
  await expect(main.getByText(/Assistido em/)).toHaveCount(0)
  await expect(page.getByRole('heading', { name: 'Histórico' })).toHaveCount(0)
})
