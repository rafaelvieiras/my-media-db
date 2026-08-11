import { expect, test } from '../fixtures'

// Bloco B (briefing 0097): jornada de configuração do token do webhook de
// entrada do Jellyfin -- fecha o buraco que o 0093/0094 deixaram (nenhum dos
// dois construiu essa UI, cada briefing empurrou pro outro). Não depende do
// estado da biblioteca (o token vive em app_settings, fora do domínio de
// entidades), então não chama resetBiblioteca; é idempotente quanto ao
// estado inicial do token (funciona tanto na primeira visita, sem token
// nenhum gerado, quanto reexecutando contra um servidor já usado no modo de
// iteração rápida via E2E_BASE_URL).

test('pagina de webhook: gera, mostra, copia e regenera o token do Jellyfin', async ({
  page,
  context,
}) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])

  await page.goto('/dashboard')
  await page.getByRole('link', { name: 'Webhook' }).click()
  await expect(page).toHaveURL(/\/dashboard\/webhook$/)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Webhook' }),
  ).toBeVisible()

  // Espera a query resolver: ou o estado vazio ou o campo com token aparecem.
  const semToken = page.getByText('Nenhum token gerado ainda.')
  const campoToken = page.getByLabel('Token do webhook')
  await expect(semToken.or(campoToken)).toBeVisible()

  if (await semToken.isVisible()) {
    await page.getByRole('button', { name: 'Gerar token' }).click()
  }
  await expect(campoToken).toBeVisible()

  // Um token RECÉM-gerado começa visível de propósito (o dono precisa
  // conseguir copiá-lo sem um clique extra em "Mostrar"); um token
  // pré-existente carregado do backend começa mascarado. Normaliza para o
  // estado mascarado antes de testar o toggle, para o teste valer nos dois
  // casos.
  const botaoOcultar = page.getByRole('button', { name: 'Ocultar token' })
  if (await botaoOcultar.isVisible()) {
    await botaoOcultar.click()
  }
  await expect(campoToken).toHaveAttribute('type', 'password')
  const tokenAtual = await campoToken.inputValue()
  expect(tokenAtual.length).toBeGreaterThan(0)

  // Mostrar/ocultar.
  await page.getByRole('button', { name: 'Mostrar token' }).click()
  await expect(campoToken).toHaveAttribute('type', 'text')
  await page.getByRole('button', { name: 'Ocultar token' }).click()
  await expect(campoToken).toHaveAttribute('type', 'password')

  // Copiar token: verifica que o valor certo foi parar na área de
  // transferência de verdade (permissão concedida acima).
  await page.getByRole('button', { name: 'Copiar token' }).click()
  await expect(page.getByRole('button', { name: 'Copiado!' })).toBeVisible()
  const tokenCopiado = await page.evaluate(() => navigator.clipboard.readText())
  expect(tokenCopiado).toBe(tokenAtual)

  // Copiar URL do webhook: contém o token na querystring.
  await page.getByRole('button', { name: 'Copiar URL do webhook' }).click()
  const urlCopiada = await page.evaluate(() => navigator.clipboard.readText())
  expect(urlCopiada).toContain(`/api/v1/webhooks/jellyfin?token=${tokenAtual}`)

  // Regenerar: confirma o dialog nativo e verifica que o token exibido mudou.
  page.once('dialog', (dialog) => dialog.accept())
  await page.getByRole('button', { name: 'Gerar novo token' }).click()
  await expect(campoToken).not.toHaveValue(tokenAtual)

  // Placeholder de saída sempre visível, sem depender de nenhuma API.
  await expect(
    page.getByRole('heading', { name: 'Webhooks de saída' }),
  ).toBeVisible()
  await expect(
    page.getByText('Nenhum webhook de saída disponível ainda.'),
  ).toBeVisible()
})

test('sidebar do dashboard marca o link Webhook como ativo na rota /dashboard/webhook', async ({
  page,
}) => {
  await page.goto('/dashboard/webhook')
  const link = page
    .getByRole('navigation', { name: 'Administração' })
    .getByRole('link', { name: 'Webhook' })
  await expect(link).toHaveAttribute('aria-current', 'page')
})
