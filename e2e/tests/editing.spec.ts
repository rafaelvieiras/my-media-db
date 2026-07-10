import { expect, test } from '../fixtures'
import { resetBiblioteca } from '../lib/reset'

// Jornadas de EDICAO do INTERNO. O foco e a garantia central do ADR-0012: o
// override do dono e diferencial e sobrevive a um re-enriquecimento; e o ciclo
// de identidade externa (0027): fixar `external_ids` e puxar os dados da fonte
// pelo id recem-fixado. Cada teste comeca com a biblioteca zerada (reset por
// API), semeia o proprio estado e navega para o detalhe interno.

test.beforeEach(async ({ request }) => {
  await resetBiblioteca(request)
})

test('editar override nao e destrutivo apos re-enriquecer', async ({
  page,
  request,
}) => {
  // Semeia o filme 603 (Matrix) importando pela API e vai ao detalhe interno.
  const res = await request.post('/api/v1/import', {
    data: { source: 'tmdb', source_id: '603', type: 'movie' },
  })
  expect(res.ok()).toBeTruthy()
  const { id } = await res.json()
  await page.goto(`/entities/${id}`)
  await expect(page.getByRole('heading', { level: 1, name: 'Matrix' })).toBeVisible()

  // Abre o editor de override e edita em pt-BR: novo titulo, remove "Acao" e
  // adiciona "Favorito".
  await page.getByRole('button', { name: 'Editar' }).click()
  await page.getByRole('textbox', { name: 'Título', exact: true }).fill('Matrix EDITADO')
  await page.getByRole('button', { name: 'Remover tag Acao' }).click()
  const campoTag = page.getByRole('textbox', { name: 'Adicionar tag' })
  await campoTag.fill('Favorito')
  await campoTag.press('Enter')
  await page.getByRole('button', { name: 'Salvar alterações' }).click()

  // Confirma que o override round-tripou (o save terminou) antes de re-enriquecer.
  await expect(
    page.getByRole('heading', { level: 1, name: 'Matrix EDITADO' }),
  ).toBeVisible()

  // Nao ha botao de enrich direto no detalhe (so "Salvar e re-enriquecer", que e
  // acoplado aos IDs externos). Dispara o re-enriquecimento pela API e recarrega.
  const enrich = await request.post(`/api/v1/entities/${id}/enrich`, { data: {} })
  expect(enrich.ok()).toBeTruthy()
  await page.reload()

  // O override sobreviveu ao enrich (ADR-0012): titulo preservado; "Favorito"
  // (add do dono) e "Ficcao cientifica" (base externa) presentes; "Acao"
  // (remove do dono) sumiu — a fonte reofereceria "Acao", mas o remove vence.
  await expect(
    page.getByRole('heading', { level: 1, name: 'Matrix EDITADO' }),
  ).toBeVisible()
  await expect(page.getByText('Favorito', { exact: true })).toBeVisible()
  await expect(page.getByText('Ficcao cientifica', { exact: true })).toBeVisible()
  await expect(page.getByText('Acao', { exact: true })).toHaveCount(0)
})

test('item manual: fixar external-ids e re-enriquecer puxa dados da fonte', async ({
  page,
  request,
}) => {
  // Cria um item manual (sem external_ids nem dados de fonte) e vai ao detalhe.
  const res = await request.post('/api/v1/entities', {
    data: { type: 'movie', title: 'Meu Filme Local' },
  })
  expect(res.ok()).toBeTruthy()
  const { id } = await res.json()
  await page.goto(`/entities/${id}`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Meu Filme Local' }),
  ).toBeVisible()

  // Abre a edicao e o editor de IDs externos: fixa a fonte tmdb=603.
  await page.getByRole('button', { name: 'Editar' }).click()
  await page.getByRole('button', { name: 'Adicionar fonte' }).click()
  await page.getByRole('textbox', { name: 'Fonte do id externo' }).fill('tmdb')
  // O aria-label do id passa a "Id na fonte tmdb" depois que a fonte e digitada.
  await page.getByRole('textbox', { name: /^Id na fonte/ }).fill('603')
  await page.getByRole('button', { name: 'Salvar e re-enriquecer' }).click()

  // Espera o enrich round-tripar (o heading ja reflete a fonte) e recarrega para
  // a view limpa de leitura — fora do modo de edicao o genero aparece so em
  // "Gêneros" (o editor aberto tambem o mostraria como chip de tag).
  await expect(page.getByRole('heading', { level: 1, name: 'Matrix' })).toBeVisible()
  await page.reload()

  // O enrich pelo id recem-fixado puxa os dados da fonte 603 (Matrix): o titulo
  // e o genero passam a refletir o filme da fonte.
  await expect(page.getByRole('heading', { level: 1, name: 'Matrix' })).toBeVisible()
  await expect(page.getByText('Ficcao cientifica', { exact: true })).toBeVisible()
})
