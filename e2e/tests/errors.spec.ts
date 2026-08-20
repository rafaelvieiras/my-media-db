import { expect, test } from '../fixtures'
import { resetBiblioteca } from '../lib/reset'

// Jornadas de CAMINHO DE ERRO (briefing 0028 §7 item 7): garantem que a UI mostra
// o erro do envelope INLINE (em pt-BR) e que a entity NAO e corrompida quando a
// fonte nao entrega. Cada teste comeca com a biblioteca zerada (reset por API),
// semeia o proprio estado e navega ao detalhe interno.
//
// Sobre o 502 ("fonte indisponivel"): o provider fake do harness esta SEMPRE de pe
// e NAO tem injecao de indisponibilidade (nem env flag nem id "magico" — ver
// internal/provider/fake/fake.go). Mas o 502 e alcancavel HOJE, SEM tocar o fake:
// o handler de enrich transforma QUALQUER falha da fonte em 502 (respondFonteIndisponivel,
// internal/server/enrich.go), e o fake devolve ErrNaoEncontrado para um source_id que
// nao esta no catalogo. Entao "fixar tmdb=<id inexistente> e re-enriquecer" exercita
// o status 502 + o envelope external_source_error de verdade — a fonte nao consegue
// entregar o detalhe daquele id. O que fica de FOLLOW-UP e a indisponibilidade de
// TRANSPORTE (timeout/conexao recusada / fonte fora do ar): isso exigiria um hook de
// fault-injection no fake (env flag ou id que forca 5xx) + rebuild do servidor, o que
// este spec deliberadamente NAO faz (outro agente depende do :8793).

test.beforeEach(async ({ request }) => {
  await resetBiblioteca(request)
})

test('re-enriquecer sem id da fonte mostra 422 inline e nao corrompe a entity', async ({
  page,
  request,
}) => {
  // Item manual: nasce SEM external_ids nem dados de fonte.
  const res = await request.post('/api/v1/entities', {
    data: { type: 'movie', title: 'Item Sem Fonte' },
  })
  expect(res.ok()).toBeTruthy()
  const { id } = await res.json()
  await page.goto(`/entities/${id}`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Item Sem Fonte' }),
  ).toBeVisible()

  // "Editar" e um LINK para `/entities/$id/edit` (0030), agora com uma sidebar de
  // secoes (0080): o editor de IDs externos vive na subrota "IDs de terceiros".
  // Mora dentro do dropdown "Gerenciar" (0086). Dispara "Salvar e re-enriquecer"
  // SEM fixar nenhum id externo: salva external_ids (vazio) e tenta enriquecer;
  // sem source_id o backend responde 422 (invalid_body) e a UI mostra a mensagem
  // do envelope inline.
  await page.getByRole('button', { name: 'Gerenciar' }).click()
  await page.getByRole('menuitem', { name: 'Editar' }).click()
  await expect(page).toHaveURL(new RegExp(`/entities/${id}/edit$`))
  await page.getByRole('link', { name: 'IDs de terceiros' }).click()
  await expect(page).toHaveURL(new RegExp(`/entities/${id}/edit/ids$`))
  await expect(page.getByText('Nenhum id externo definido.')).toBeVisible()
  await page.getByRole('button', { name: 'Salvar e re-enriquecer' }).click()

  // Erro INLINE (role=alert) com a mensagem pt-BR real do envelope 422.
  await expect(page.getByRole('alert')).toContainText(
    'sem source_id: informe no corpo ou tenha external_ids[tmdb] na entity',
  )

  // A entity NAO foi corrompida: o titulo manual permanece e nenhum dado de fonte
  // vazou. Volta ao detalhe para provar que ela segue existindo (o 422 nao a apagou).
  await page.goto(`/entities/${id}`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Item Sem Fonte' }),
  ).toBeVisible()

  // E de fato nenhum external_id foi gravado (o enrich falhou ANTES de qualquer
  // dado de fonte; o item continua puramente local).
  const depois = await request.get(`/api/v1/entities/${id}`)
  expect(depois.ok()).toBeTruthy()
  const body = await depois.json()
  expect(body.external_ids).toEqual({})
  expect(body.title).toBe('Item Sem Fonte')
})

test('re-enriquecer com id que a fonte nao entrega mostra 502 inline e preserva o titulo', async ({
  page,
  request,
}) => {
  // Item manual; fixamos tmdb=999999 (fora do catalogo do fake). O enrich puxa o
  // detalhe, o fake devolve "nao encontrado" e o handler mapeia a falha da fonte
  // para 502 (external_source_error) — a jornada "fonte nao entrega o detalhe".
  const res = await request.post('/api/v1/entities', {
    data: { type: 'movie', title: 'Item Fonte Indisponivel' },
  })
  expect(res.ok()).toBeTruthy()
  const { id } = await res.json()
  await page.goto(`/entities/${id}`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Item Fonte Indisponivel' }),
  ).toBeVisible()

  // "Editar" e um LINK para `/entities/$id/edit` (0030), agora com uma sidebar de
  // secoes (0080): o editor de IDs externos vive na subrota "IDs de terceiros".
  // Mora dentro do dropdown "Gerenciar" (0086).
  await page.getByRole('button', { name: 'Gerenciar' }).click()
  await page.getByRole('menuitem', { name: 'Editar' }).click()
  await expect(page).toHaveURL(new RegExp(`/entities/${id}/edit$`))
  await page.getByRole('link', { name: 'IDs de terceiros' }).click()
  await expect(page).toHaveURL(new RegExp(`/entities/${id}/edit/ids$`))
  await page.getByRole('button', { name: 'Adicionar fonte' }).click()
  await page.getByRole('textbox', { name: 'Fonte do id externo' }).fill('tmdb')
  // O aria-label do id passa a "Id na fonte tmdb" depois que a fonte e digitada.
  await page.getByRole('textbox', { name: /^Id na fonte/ }).fill('999999')
  await page.getByRole('button', { name: 'Salvar e re-enriquecer' }).click()

  // Erro INLINE (role=alert) com a mensagem pt-BR real do envelope 502.
  await expect(page.getByRole('alert')).toContainText(
    'a fonte externa falhou ao entregar o detalhe',
  )

  // A entity NAO foi corrompida: o titulo manual permanece (a fonte nao entregou
  // nada para sobrescrever). Volta ao detalhe para provar que ela segue existindo.
  await page.goto(`/entities/${id}`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Item Fonte Indisponivel' }),
  ).toBeVisible()
})

test('rota de detalhe com id inexistente mostra 404 inline', async ({ page }) => {
  // Navega direto para uma entity que nao existe: o GET responde 404 e a tela de
  // detalhe cai no estado de erro (role=alert) com a mensagem do envelope.
  await page.goto('/entities/00000000-0000-0000-0000-000000000000')

  const alerta = page.getByRole('alert')
  await expect(alerta).toContainText('Não foi possível carregar o item')
  await expect(alerta).toContainText('entidade não encontrada')
})

test('import com type invalido responde 422 com envelope pt-BR', async ({
  request,
}) => {
  // Caminho de erro de VALIDACAO sem tela dedicada (a UI de import nunca manda um
  // type invalido): garante o contrato do envelope 422 direto na API. source_id
  // valido, type fora de movie|tv|collection|artista|faixa (0115 Parte 2
  // acrescentou artista/faixa ao enum aceito e a mensagem correspondente).
  const res = await request.post('/api/v1/import', {
    data: { source: 'tmdb', source_id: '603', type: 'banana' },
  })
  expect(res.status()).toBe(422)
  const body = await res.json()
  expect(body.error.code).toBe('invalid_body')
  expect(body.error.message).toBe(
    'type inválido (use movie, tv, collection, artista ou faixa)',
  )
})
