import { expect, test } from '../fixtures'
import { resetBiblioteca } from '../lib/reset'

// Jornadas da CRIACAO LOCAL (0025/0027): criar item do zero na home e o ciclo de
// vida de uma colecao local (criar -> adicionar/remover membro -> excluir). Cada
// teste comeca com a biblioteca zerada (reset por API), entao os UUIDs
// deterministicos reiniciam e a busca local nao arrasta estado de outra jornada.

test.beforeEach(async ({ request }) => {
  await resetBiblioteca(request)
})

test('criar item manual aparece na biblioteca', async ({ page }) => {
  await page.goto('/')

  // "Novo item" agora e um LINK para a rota dedicada `/new/item` (0030); type ja
  // vem = movie (Filme). O rotulo do titulo perdeu o sufixo "do item" (vem do
  // descritor: so "Título").
  await page.getByRole('link', { name: 'Novo item' }).click()
  await expect(page).toHaveURL(/\/new\/item$/)
  await page
    .getByRole('textbox', { name: 'Título', exact: true })
    .fill('Meu Filme Manual')
  await page.getByRole('button', { name: 'Criar item' }).click()

  // Criar do zero navega ao detalhe INTERNO da entity recem-criada.
  await expect(page).toHaveURL(/\/entities\//)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Meu Filme Manual' }),
  ).toBeVisible()
})

test('nova colecao: criar, adicionar/remover membro e excluir', async ({
  page,
  request,
}) => {
  // Semeia o filme membro (603 -> "Matrix" em pt-BR) via API; a busca local do
  // picker so o encontra depois de importado.
  const imp = await request.post('/api/v1/import', {
    data: { source: 'tmdb', source_id: '603', type: 'movie' },
  })
  expect(imp.ok()).toBeTruthy()

  // Cria a colecao local pela home: "Nova coleção" e um LINK para `/new/collection`
  // (0030). O rotulo do titulo vem do descritor (so "Título", sem sufixo).
  await page.goto('/')
  await page.getByRole('link', { name: 'Nova coleção' }).click()
  await expect(page).toHaveURL(/\/new\/collection$/)
  await page
    .getByRole('textbox', { name: 'Título', exact: true })
    .fill('Minha Coleção')
  await page.getByRole('button', { name: 'Criar coleção' }).click()

  await expect(page).toHaveURL(/\/entities\//)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Minha Coleção' }),
  ).toBeVisible()
  await expect(page.getByText('Esta coleção ainda não tem itens.')).toBeVisible()

  // Adiciona o membro: abre o picker, busca "Matrix" e escolhe o candidato local
  // (rotulado "titulo + tipo" -> "Matrix Filme"). A espera pelo candidato cobre o
  // debounce da busca (sem sleep).
  await page.getByRole('button', { name: 'Adicionar item' }).click()
  await page
    .getByRole('searchbox', { name: 'Buscar item para adicionar' })
    .fill('Matrix')
  await page.getByRole('button', { name: 'Matrix Filme' }).click()

  // O membro entra na lista, com o controle de remover.
  const removerMatrix = page.getByRole('button', {
    name: 'Remover Matrix da coleção',
  })
  await expect(removerMatrix).toBeVisible()

  // Fecha o picker e remove o membro (fluxo de DUAS etapas: x -> "Remover?").
  await page.getByRole('button', { name: 'Concluir' }).click()
  await removerMatrix.click()
  await page.getByRole('button', { name: 'Remover?' }).click()

  await expect(removerMatrix).toHaveCount(0)
  await expect(page.getByText('Esta coleção ainda não tem itens.')).toBeVisible()

  // Exclui a colecao (DUAS etapas: "Excluir coleção" -> "Excluir coleção?").
  await page.getByRole('button', { name: 'Excluir coleção', exact: true }).click()
  await page.getByRole('button', { name: 'Excluir coleção?' }).click()

  // Sai da tela da colecao e volta para a biblioteca (home).
  await expect(
    page.getByRole('heading', { level: 1, name: 'Biblioteca' }),
  ).toBeVisible()

  // Excluir a colecao NAO apaga os membros: o filme "Matrix" continua existindo.
  const res = await request.get('/api/v1/entities?limit=1000')
  expect(res.ok()).toBeTruthy()
  const body = await res.json()
  const itens: Array<{ title: string; type: string }> = body.items ?? body ?? []
  expect(itens.some((e) => e.title === 'Matrix' && e.type === 'movie')).toBeTruthy()
})
