import { expect, test } from '../fixtures'
import { resetBiblioteca } from '../lib/reset'

// Ciclo de arte da entity (briefing 0017): o dono envia um poster, marca como
// preferida (desempata entre as suas) e remove. Cada teste comeca com a
// biblioteca zerada; aqui importamos 603 (Matrix) por API e cobrimos a galeria
// pela UI. So asseracoes de conteudo, web-first (sem sleeps).

test.beforeEach(async ({ request }) => {
  await resetBiblioteca(request)
})

// PNG 1x1 valido (magic bytes que o backend aceita) para o upload.
const PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==',
  'base64',
)

test('arte: enviar, preferir e remover na galeria da entity', async ({
  page,
  request,
}) => {
  // Importa 603 por API (POST /import devolve a entity com .id) e abre o detalhe.
  const imp = await request.post('/api/v1/import', {
    data: { source: 'tmdb', source_id: '603', type: 'movie' },
  })
  expect(imp.ok()).toBeTruthy()
  const { id } = (await imp.json()) as { id: string }

  await page.goto(`/entities/${id}`)

  // Escopo: a secao "Arte" (chrome sempre em pt-BR, independe do idioma de leitura).
  const arte = page
    .locator('section')
    .filter({ has: page.getByRole('heading', { name: 'Arte', level: 2 }) })
  await expect(arte.getByRole('heading', { name: 'Arte' })).toBeVisible()

  // Galeria comeca vazia.
  await expect(arte.getByText('Nenhuma arte ainda')).toBeVisible()

  // Envia um poster. O tipo ja vem em "Poster" por padrao; o botao "Enviar arte"
  // dispara um <input type=file> escondido, entao alimentamos o input direto
  // (setInputFiles funciona em input oculto e exercita o mesmo onChange/upload).
  await expect(arte.getByRole('combobox', { name: 'Tipo de arte' })).toHaveValue(
    'poster',
  )
  await arte
    .getByLabel('Arquivo de arte')
    .setInputFiles({ name: 'poster.png', mimeType: 'image/png', buffer: PNG })

  // A galeria deixa de estar vazia e mostra 1 item do dono (selo "sua") com as
  // acoes "Preferir" e "Remover".
  await expect(arte.getByText('Nenhuma arte ainda')).toHaveCount(0)
  await expect(arte.getByRole('img', { name: 'poster sua' })).toBeVisible()
  await expect(arte.getByText('sua')).toBeVisible()
  await expect(
    arte.getByRole('button', { name: 'Remover arte poster' }),
  ).toBeVisible()

  // Preferir: o item vira o preferido do seu kind. Aparece o selo "preferida" e o
  // botao "Preferir" some (nao ha o que desempatar quando ele ja venceu).
  await arte.getByRole('button', { name: 'Preferir' }).click()
  await expect(arte.getByText('preferida')).toBeVisible()
  await expect(arte.getByRole('button', { name: 'Preferir' })).toHaveCount(0)

  // Remover: acao direta (sem confirmacao). A galeria volta ao estado vazio.
  await arte.getByRole('button', { name: 'Remover arte poster' }).click()
  await expect(arte.getByText('Nenhuma arte ainda')).toBeVisible()
  await expect(arte.getByRole('img', { name: 'poster sua' })).toHaveCount(0)
})
