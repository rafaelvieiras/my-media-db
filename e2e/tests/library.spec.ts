import { mkdtempSync, renameSync, rmSync, unlinkSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { expect, test } from '../fixtures'
import { resetBiblioteca } from '../lib/reset'

// Bloco F (briefing 0028): jornadas de BIBLIOTECA (datasource + scan) e CURADORIA
// (revisao de drafts / orfas). O binario valida o path do datasource em disco e
// escaneia arquivos REAIS, entao cada teste semeia uma pasta temporaria de /tmp
// (via node:fs, no proprio processo do teste — o binario le o mesmo /tmp) com os
// arquivos que produzem o cenario desejado:
//   - nome puramente tecnico ("2160p.mkv") -> Title vazio, sem id -> DRAFT (revisao)
//   - nome parseavel ("The Matrix (1999).mkv") -> entity 'cataloged' auto; apagar o
//     arquivo + rescan deixa a entity orfa (todos os media_file 'missing').
//
// O scan e ASSINCRONO (POST 202 + polling GET .../scan). Para nao depender do
// timing do polling do front, disparamos o scan pela UI (cobre DatasourceList) e
// esperamos a CONCLUSAO por `expect.poll` contra a API da fila-alvo (web-first,
// sem sleep). resetBiblioteca zera ENTITIES, mas nao datasources nem /tmp: cada
// teste limpa os datasources (API) no beforeEach e remove suas pastas no afterEach.

// Pastas temporarias criadas no teste, removidas no afterEach.
const pastas: string[] = []

// semeiaPasta cria uma pasta unica em /tmp (nome gerado pelo SO, sem Date.now) e
// grava os arquivos pedidos com algum conteudo (o scanner faz hash do arquivo).
function semeiaPasta(arquivos: string[]): string {
  const dir = mkdtempSync(join(tmpdir(), 'mmdb-e2e-'))
  pastas.push(dir)
  for (const nome of arquivos) {
    writeFileSync(join(dir, nome), Buffer.alloc(2048, 7))
  }
  return dir
}

// limparDatasources remove todos os datasources via API (o reset nao os toca).
async function limparDatasources(request: import('@playwright/test').APIRequestContext) {
  const res = await request.get('/api/v1/library')
  const body = await res.json()
  for (const s of body.items ?? []) {
    await request.delete(`/api/v1/library/${s.id}`)
  }
}

test.beforeEach(async ({ request }) => {
  await resetBiblioteca(request)
  await limparDatasources(request)
})

test.afterEach(() => {
  for (const dir of pastas.splice(0)) {
    rmSync(dir, { recursive: true, force: true })
  }
})

test('adicionar e remover datasource', async ({ page }) => {
  const dir = semeiaPasta(['1080p.mkv'])

  await page.goto('/dashboard/library')
  await expect(page.getByRole('heading', { level: 1, name: 'Bibliotecas' })).toBeVisible()

  // Adicionar saiu do form inline: navega para a rota dedicada /dashboard/library/new
  // (briefing 0071), digita o caminho direto no campo (sem abrir o PathBrowser,
  // que e so um atalho visual opcional) e escolhe o tipo (Filmes ja e o default).
  await page.getByRole('link', { name: 'Adicionar datasource' }).click()
  await expect(page.getByRole('heading', { level: 1, name: 'Adicionar datasource' })).toBeVisible()
  await page.getByLabel('Caminho do datasource').fill(dir)
  // Override de reentativa de enriquecimento (briefing 0085): opcional, sobrepoe
  // o throttle global (default 15 dias) so para esta biblioteca.
  await page.getByLabel('Reentativa de enriquecimento (dias) do datasource').fill('20')
  await page.getByRole('button', { name: 'Adicionar datasource' }).click()

  // Volta para /dashboard/library; a linha aparece com o path e o estado online; o botao
  // de scan fica disponivel.
  await expect(page).toHaveURL('/dashboard/library')
  await expect(page.getByText(dir)).toBeVisible()
  await expect(page.getByText('online')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Escanear' })).toBeEnabled()

  // Remove: navega pela linha ate /dashboard/library/$id/edit, usa a "Zona de perigo" com
  // confirmacao EXPLICITA em 2 passos (nao existe mais remocao direta).
  await page.getByRole('link', { name: 'Editar' }).click()
  await expect(page.getByRole('heading', { level: 1, name: 'Editar datasource' })).toBeVisible()
  // O override de reentativa (0085) gravado na criacao pre-carrega aqui.
  await expect(
    page.getByLabel('Reentativa de enriquecimento (dias) do datasource'),
  ).toHaveValue('20')
  await page.getByRole('button', { name: 'Remover datasource' }).click()
  const dialogo = page.getByRole('alertdialog', { name: 'Confirmar remoção do datasource' })
  await expect(dialogo).toBeVisible()
  await dialogo.getByRole('button', { name: 'Confirmar remoção' }).click()

  // Volta para /dashboard/library; a lista fica vazia de novo.
  await expect(page).toHaveURL('/dashboard/library')
  await expect(page.getByText('Nenhum datasource cadastrado')).toBeVisible()
})

test('scan de arquivo nao-identificado vira draft e a correcao o tira da revisao', async ({
  page,
  request,
}) => {
  // "2160p" e so token tecnico: Title vazio, sem id externo -> DRAFT no scan.
  const dir = semeiaPasta(['2160p.mkv'])

  await page.goto('/dashboard/library')
  await page.getByRole('link', { name: 'Adicionar datasource' }).click()
  await page.getByLabel('Caminho do datasource').fill(dir)
  await page.getByRole('button', { name: 'Adicionar datasource' }).click()
  await expect(page).toHaveURL('/dashboard/library')
  await expect(page.getByText(dir)).toBeVisible()

  // Dispara o scan pela UI e espera ele PRODUZIR o draft (fila de revisao != vazia).
  await page.getByRole('button', { name: 'Escanear' }).click()
  await expect
    .poll(async () => (await (await request.get('/api/v1/review')).json()).length, {
      timeout: 20_000,
    })
    .toBeGreaterThan(0)

  // Na revisao, o draft aparece como "nao identificado".
  await page.goto('/dashboard/review')
  await expect(page.getByRole('heading', { level: 1, name: 'Revisão' })).toBeVisible()
  await expect(page.getByText('não identificado', { exact: true })).toBeVisible()

  // Corrigir: expande o painel, busca "matrix" (fonte externa fake) e usa o 1o.
  await page.getByRole('button', { name: 'Corrigir' }).click()
  await page.getByLabel('Buscar o título correto').fill('matrix')
  await page.getByRole('button', { name: 'Usar este' }).first().click()

  // Re-enriquecido e re-resolvido, o item sai da fila -> revisao vazia.
  await expect(page.getByText('Nada para revisar')).toBeVisible()
})

test('arquivo catalogado que some vira orfa e pode ser purgado', async ({
  page,
  request,
}) => {
  // Nome parseavel -> entity 'cataloged' auto com o arquivo 'present'.
  const dir = semeiaPasta(['The Matrix (1999).mkv'])

  await page.goto('/dashboard/library')
  await page.getByRole('link', { name: 'Adicionar datasource' }).click()
  await page.getByLabel('Caminho do datasource').fill(dir)
  await page.getByRole('button', { name: 'Adicionar datasource' }).click()
  await expect(page).toHaveURL('/dashboard/library')
  await expect(page.getByText(dir)).toBeVisible()

  // 1o scan: espera a entity catalogada aparecer.
  const botaoEscanear = page.getByRole('button', { name: 'Escanear' })
  await botaoEscanear.click()
  await expect
    .poll(
      async () => {
        const body = await (await request.get('/api/v1/entities?limit=1000')).json()
        const itens: Array<{ title: string }> = body.items ?? body ?? []
        return itens.some((e) => e.title === 'The Matrix')
      },
      { timeout: 20_000 },
    )
    .toBe(true)

  // O arquivo some do disco; o rescan marca o media_file 'missing' -> a entity
  // (auto-criada, sem edicao do dono) fica orfa.
  unlinkSync(join(dir, 'The Matrix (1999).mkv'))
  await expect(botaoEscanear).toBeEnabled()
  await botaoEscanear.click()
  await expect
    .poll(async () => (await (await request.get('/api/v1/orphans')).json()).length, {
      timeout: 20_000,
    })
    .toBeGreaterThan(0)

  // Na tela de orfas, purga com confirmacao EXPLICITA em 2 passos.
  await page.goto('/dashboard/orphans')
  await expect(page.getByRole('heading', { level: 1, name: 'Órfãs' })).toBeVisible()
  await expect(page.getByRole('link', { name: 'The Matrix' })).toBeVisible()

  await page.getByRole('button', { name: 'Purgar The Matrix' }).click()
  const dialogo = page.getByRole('alertdialog', { name: 'Confirmar purga' })
  await expect(dialogo).toBeVisible()
  await dialogo.getByRole('button', { name: 'Confirmar purga' }).click()

  // Purgada: a fila fica vazia.
  await expect(page.getByText('Nenhuma órfã')).toBeVisible()
})

test('editar caminho de datasource cadastrado recasca sem novo scan', async ({
  page,
  request,
}) => {
  // Semeia com um arquivo catalogavel para provar que o media_file recascado
  // (briefing 0070) continua resolvendo a mesma entity, SEM disparar scan.
  const dirAntigo = semeiaPasta(['The Matrix (1999).mkv'])

  await page.goto('/dashboard/library')
  await page.getByRole('link', { name: 'Adicionar datasource' }).click()
  await page.getByLabel('Caminho do datasource').fill(dirAntigo)
  await page.getByRole('button', { name: 'Adicionar datasource' }).click()
  await expect(page).toHaveURL('/dashboard/library')
  await expect(page.getByText(dirAntigo)).toBeVisible()

  // 1o scan: a entity catalogada aparece.
  await page.getByRole('button', { name: 'Escanear' }).click()
  await expect
    .poll(
      async () => {
        const body = await (await request.get('/api/v1/entities?limit=1000')).json()
        const itens: Array<{ title: string }> = body.items ?? body ?? []
        return itens.some((e) => e.title === 'The Matrix')
      },
      { timeout: 20_000 },
    )
    .toBe(true)

  // O disco "muda de lugar": renomeia a pasta fisica para um novo caminho
  // (mesmo conteudo, novo mount) e edita o datasource para apontar pra la --
  // SEM rodar scan de novo.
  const dirNovo = `${dirAntigo}-remontado`
  renameSync(dirAntigo, dirNovo)
  pastas.splice(pastas.indexOf(dirAntigo), 1, dirNovo)

  // Editar saiu da lista (icone de lapis com form inline) para a rota dedicada
  // /dashboard/library/$id/edit (briefing 0071).
  await page.getByRole('link', { name: 'Editar' }).click()
  await expect(page.getByRole('heading', { level: 1, name: 'Editar datasource' })).toBeVisible()
  await page.getByLabel('Caminho do datasource').fill(dirNovo)
  await page.getByRole('button', { name: 'Salvar' }).click()

  // Volta para /dashboard/library; a linha passa a mostrar o caminho novo, online, sem
  // precisar de scan -- o vinculo com a entity ja catalogada continua intacto.
  await expect(page).toHaveURL('/dashboard/library')
  await expect(page.getByText(dirNovo)).toBeVisible()
  await expect(page.getByText('online')).toBeVisible()
  await page.goto('/')
  await expect(page.getByRole('link', { name: /Matrix/ }).first()).toBeVisible()
})
