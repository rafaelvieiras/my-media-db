import { expect, test } from '../fixtures'

// Briefing 0096 (segunda metade do épico de Integrações, ADR-0023): jornada
// completa de `/dashboard/integrations` -- lista com badge `configurada` +
// formulário por schema do TMDB (0095). O servidor de e2e sobe com o
// provider FAKE (MYMEDIADB_FAKE_PROVIDER=1, global-setup.ts) e SEM
// MYMEDIADB_TMDB_TOKEN/MYMEDIADB_TMDB_API_KEY -- então os 2 campos do TMDB
// começam com `origem:"nenhum"` (sem legenda), nunca `"env"`; a legenda de
// origem "env" (com o nome da variável) é coberta pelo teste de componente
// (`integration-form.test.tsx`), não aqui. Não depende de `resetBiblioteca`
// (integration_config é um domínio à parte, fora de `entity`) -- restaura o
// estado ao fim (limpa o token) pelo mesmo espírito do teste de
// enriquecimento retroativo em settings.spec.ts, já que o worker único
// compartilha o banco com as demais jornadas do arquivo.

test('jornada completa: lista, configura, limpa e o badge reflete cada passo', async ({
  page,
}) => {
  await page.goto('/dashboard')
  await page.getByRole('link', { name: 'Integrações' }).click()
  await expect(page).toHaveURL(/\/dashboard\/integrations$/)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Integrações' }),
  ).toBeVisible()

  // Card do TMDB, badge inicial "Não configurada" (nenhum token/api_key
  // gravado ainda -- fake provider não popula integration_config).
  const cardTmdb = page.getByRole('link', { name: /TMDB/ })
  await expect(cardTmdb).toBeVisible()
  await expect(cardTmdb.getByText('Não configurada')).toBeVisible()

  await cardTmdb.click()
  await expect(page).toHaveURL(/\/dashboard\/integrations\/tmdb$/)
  await expect(
    page.getByRole('heading', { level: 1, name: 'TMDB' }),
  ).toBeVisible()

  // exact:true -- por padrão o getByLabel do Playwright faz match por
  // substring, e o botão de mostrar/ocultar ("Mostrar Read Access Token
  // (v4)") também contém o rótulo do campo como substring.
  const campoToken = page.getByLabel('Read Access Token (v4)', {
    exact: true,
  })
  const campoApiKey = page.getByLabel('API Key (v3)', { exact: true })
  await expect(campoToken).toBeVisible()
  await expect(campoApiKey).toBeVisible()

  // origem:"nenhum" -- campo vazio, sem legenda "Definido..." e sem botão
  // Limpar (nada gravado no banco ainda pra remover).
  await expect(campoToken).toHaveValue('')
  await expect(page.getByText(/Definido/)).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Limpar' })).toHaveCount(0)

  // Campo secreto: mascarado por padrão, toggle mostrar/ocultar funciona.
  await expect(campoToken).toHaveAttribute('type', 'password')
  await page
    .getByRole('button', { name: 'Mostrar Read Access Token (v4)' })
    .click()
  await expect(campoToken).toHaveAttribute('type', 'text')
  await page
    .getByRole('button', { name: 'Ocultar Read Access Token (v4)' })
    .click()
  await expect(campoToken).toHaveAttribute('type', 'password')

  // Preenche o token e salva -- toast estático de restart (0096 §2 item 4).
  await campoToken.fill('token-de-teste-e2e')
  await page.getByRole('button', { name: 'Salvar' }).click()
  await expect(
    page.getByText('Salvo. Reinicie o servidor (docker restart) para aplicar.'),
  ).toBeVisible()

  // Pós-gravação: NUNCA reidrata o campo secreto com o valor digitado -- volta
  // vazio, mas agora com origem:"banco" (legenda + botão Limpar aparecem).
  await expect(campoToken).toHaveValue('')
  await expect(page.getByText('Definido no painel.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Limpar' })).toBeVisible()
  // api_key continua intocado (origem:"nenhum" -- PUT foi parcial, só token).
  await expect(campoApiKey).toHaveValue('')

  // Volta à lista: o badge já reflete configurada:true, sem precisar
  // reiniciar o servidor (só o PROVIDER em si depende do restart, não a
  // leitura da listagem).
  await page.getByRole('link', { name: 'Voltar às integrações' }).click()
  await expect(page).toHaveURL(/\/dashboard\/integrations$/)
  await expect(page.getByRole('link', { name: /TMDB/ }).getByText('Configurada')).toBeVisible()

  // Volta ao detalhe e clica Limpar: manda null, o campo volta a
  // origem:"nenhum" (sem env var setada no processo de e2e) -- a legenda e o
  // botão Limpar somem, e o badge da lista volta a "Não configurada".
  await page.getByRole('link', { name: /TMDB/ }).click()
  await expect(page).toHaveURL(/\/dashboard\/integrations\/tmdb$/)
  await page.getByRole('button', { name: 'Limpar' }).click()
  await page.getByRole('button', { name: 'Salvar' }).click()
  await expect(
    page.getByText('Salvo. Reinicie o servidor (docker restart) para aplicar.'),
  ).toBeVisible()
  await expect(page.getByText(/Definido/)).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Limpar' })).toHaveCount(0)

  await page.getByRole('link', { name: 'Voltar às integrações' }).click()
  await expect(
    page.getByRole('link', { name: /TMDB/ }).getByText('Não configurada'),
  ).toBeVisible()
})

test('slug desconhecido mostra o estado de erro', async ({ page }) => {
  await page.goto('/dashboard/integrations/nao-existe')
  await expect(
    page.getByText('Não foi possível carregar a integração'),
  ).toBeVisible()
  await expect(page.getByText('integração desconhecida')).toBeVisible()
})

test('sidebar do dashboard marca o link Integrações como ativo na rota /dashboard/integrations', async ({
  page,
}) => {
  await page.goto('/dashboard/integrations')
  const link = page
    .getByRole('navigation', { name: 'Administração' })
    .getByRole('link', { name: 'Integrações' })
  await expect(link).toHaveAttribute('aria-current', 'page')
})
