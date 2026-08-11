import { expect, test } from '../fixtures'

// Briefing 0099: lista GLOBAL (fora de /entities/{id}) de eventos de webhook
// que não bateram com nenhum item catalogado -- somente leitura (o backfill
// automático é responsabilidade do backend, sem ação manual nesta tela). Não
// depende de resetBiblioteca: popular um evento orfao de verdade exigiria
// simular um webhook sem match, o que este spec não tenta reproduzir --
// valida só que a tela renderiza sem erro a partir do link do sidebar,
// contra qualquer estado real de órfãos do servidor de teste.

test('pagina de eventos sem item relacionado: navega pelo sidebar e carrega sem erro', async ({
  page,
}) => {
  await page.goto('/dashboard')
  await page.getByRole('link', { name: 'Eventos sem item' }).click()
  await expect(page).toHaveURL(/\/dashboard\/historico-orfaos$/)
  await expect(
    page.getByRole('heading', {
      level: 1,
      name: 'Eventos sem item relacionado',
    }),
  ).toBeVisible()

  // A query resolve para um dos três estados possíveis: vazio, lista com
  // itens, ou erro -- nunca um loading eterno.
  const vazio = page.getByText('Nenhum evento sem item relacionado')
  const erro = page.getByText(
    'Não foi possível carregar os eventos sem item relacionado',
  )
  const listaComItens = page.getByRole('button', { name: 'Próxima' })
  await expect(vazio.or(erro).or(listaComItens)).toBeVisible()

  // Erro não é um estado aceitável aqui -- se a query falhar, o teste falha
  // explicitamente em vez de passar silenciosamente no "or" acima.
  await expect(erro).not.toBeVisible()
})

test('sidebar do dashboard marca o link Eventos sem item como ativo na rota /dashboard/historico-orfaos', async ({
  page,
}) => {
  await page.goto('/dashboard/historico-orfaos')
  const link = page
    .getByRole('navigation', { name: 'Administração' })
    .getByRole('link', { name: 'Eventos sem item' })
  await expect(link).toHaveAttribute('aria-current', 'page')
})
