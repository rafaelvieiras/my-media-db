import { expect, test } from '../fixtures'
import { resetBiblioteca } from '../lib/reset'

// Seções RICAS da RichDetailView (0022/0023/0024, backend internal/richfields) que as
// demais jornadas ainda não asseram: o título ORIGINAL da fonte (distinto do traduzido),
// o IDIOMA original resolvido para rótulo pt-BR, o LINK à fonte (TMDB) e a DEGRADAÇÃO
// graciosa das seções sem dado. O fake da franquia Matrix não traz vídeo/similar/coleção
// no detalhe do filme (só título/sinopse/gêneros/idioma/link), então este bloco mira os
// campos ricos que EXISTEM de fato e trava o comportamento das seções vazias.
//
// A tela é COMPARTILHADA entre o externo (antes de importar) e a entity importada; o
// último teste verifica essa PARIDADE, exercitando o adapter da entity resolvida
// (deResolvedEntity) além do detalhe externo (deExternalDetail).
//
// Idioma de leitura padrão do contexto Playwright é pt-BR (como nas jornadas irmãs),
// então o título traduzido ("Matrix Reloaded") difere do original ("The Matrix
// Reloaded") — condição para a linha de título original renderizar.

test.beforeEach(async ({ request }) => {
  await resetBiblioteca(request)
})

test('detalhe externo rico: titulo original, idioma da fonte e link a fonte', async ({
  page,
}) => {
  await page.goto('/external/tmdb/movie/604')
  const main = page.getByRole('main')

  await expect(
    page.getByRole('heading', { level: 1, name: 'Matrix Reloaded' }),
  ).toBeVisible()
  // Titulo ORIGINAL (en) renderiza distinto do titulo traduzido (pt-BR) no hero.
  await expect(
    main.getByText('The Matrix Reloaded', { exact: true }),
  ).toBeVisible()
  // Idioma da fonte ("en") resolvido para o rotulo pt-BR "Inglês" (fora do seletor
  // de idioma, que fica no cabecalho — por isso a busca e' escopada ao <main>).
  await expect(main.getByText('Inglês', { exact: true })).toBeVisible()
  // Link para a FONTE, com o href real da TMDB (nao um texto solto).
  await expect(page.getByRole('link', { name: 'TMDB' })).toHaveAttribute(
    'href',
    'https://www.themoviedb.org/movie/604',
  )
  // Sem poster na fixture (imageBase vazio) => placeholder, nunca imagem quebrada.
  await expect(page.getByTestId('poster-placeholder').first()).toBeVisible()
})

test('detalhe externo: secoes sem dado nao renderizam (degradacao graciosa)', async ({
  page,
}) => {
  await page.goto('/external/tmdb/movie/603')

  await expect(
    page.getByRole('heading', { level: 1, name: 'Matrix' }),
  ).toBeVisible()
  // As secoes ricas COM dado aparecem.
  await expect(page.getByRole('heading', { name: 'Sinopse' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Gêneros' })).toBeVisible()
  // As sem dado no fake (videos/similares/colecao/palavras-chave) nao emitem cabecalho.
  for (const secao of ['Vídeos', 'Similares', 'Coleção', 'Palavras-chave']) {
    await expect(page.getByRole('heading', { name: secao })).toHaveCount(0)
  }
})

test('paridade externo<->interno: a entity importada mostra a mesma tela rica', async ({
  page,
  request,
}) => {
  // Semeia o import via API e abre o detalhe INTERNO (mesma RichDetailView).
  const res = await request.post('/api/v1/import', {
    data: { source: 'tmdb', source_id: '604', type: 'movie' },
  })
  expect(res.ok()).toBeTruthy()
  const entity: { id: string } = await res.json()

  await page.goto(`/entities/${entity.id}`)
  const main = page.getByRole('main')

  await expect(
    page.getByRole('heading', { level: 1, name: 'Matrix Reloaded' }),
  ).toBeVisible()
  // Os MESMOS campos ricos do externo, agora servidos pela entity resolvida.
  await expect(
    main.getByText('The Matrix Reloaded', { exact: true }),
  ).toBeVisible()
  await expect(main.getByText('Inglês', { exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: 'TMDB' })).toHaveAttribute(
    'href',
    'https://www.themoviedb.org/movie/604',
  )
  // Genero exclusivo do Reloaded (pt-BR), confirmando a resolucao no idioma ativo.
  await expect(page.getByText('Aventura')).toBeVisible()
})
