import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { expect, test } from '../fixtures'
import { resetBiblioteca } from '../lib/reset'

// Ciclo de arte da entity (briefing 0017, atualizado p/ galeria em lightbox do
// 0044/0049 e posse do 0062): o dono envia um poster e remove. Aqui a entity so'
// existe via import por API (sem media_file real), entao "Definir como
// principal"/"Salvar na pasta" ficam desabilitados (owned=false) -- o fluxo de
// tornar principal com pasta real ja e' coberto pelo teste seguinte (0062).
// Cada teste comeca com a biblioteca zerada; so asseracoes de conteudo,
// web-first (sem sleeps).

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
  await expect(arte.getByText('Nenhuma arte disponível ainda.')).toBeVisible()

  // Envia um poster. O tipo ja vem em "Poster" por padrao; o botao "Enviar arte"
  // dispara um <input type=file> escondido, entao alimentamos o input direto
  // (setInputFiles funciona em input oculto e exercita o mesmo onChange/upload).
  await expect(arte.getByRole('combobox', { name: 'Tipo de arte' })).toHaveValue(
    'poster',
  )
  await arte
    .getByLabel('Arquivo de arte')
    .setInputFiles({ name: 'poster.png', mimeType: 'image/png', buffer: PNG })

  // A galeria deixa de estar vazia e mostra 1 card do poster recem-enviado.
  await expect(arte.getByText('Nenhuma arte disponível ainda.')).toHaveCount(0)
  const cardEnviado = arte.getByRole('button', { name: /^Ver Pôster \d+$/ })
  await expect(cardEnviado).toHaveCount(1)

  // Abre o lightbox: sem media_file real (owned=false), "Definir como principal"
  // fica desabilitado com tooltip; "Remover" (acao do dono) fica habilitado.
  await cardEnviado.click()
  const definirPrincipal = page.getByRole('button', {
    name: 'Definir como principal',
  })
  await expect(definirPrincipal).toBeVisible()
  await expect(definirPrincipal).toBeDisabled()
  await expect(page.getByText('principal', { exact: true })).toHaveCount(0)

  // Remover: acao direta (sem confirmacao). Fecha o lightbox e a galeria volta
  // ao estado vazio.
  await page.getByRole('button', { name: 'Remover arte' }).click()
  await expect(arte.getByText('Nenhuma arte disponível ainda.')).toBeVisible()
  await expect(
    arte.getByRole('button', { name: /^Ver Pôster \d+$/ }),
  ).toHaveCount(0)
})

// leSrcDoPosterDoTopo le o `src` do poster do HERO do RichDetailView, ou `null`
// quando so ha o placeholder (sem arte principal ainda) — helper local porque o
// teste abaixo precisa comparar o ANTES/DEPOIS independente de o fake provider ja
// ter uma arte inicial (MYMEDIADB_FAKE_IMAGE_BASE nao esta setado no e2e, entao o
// poster do TMDB fake normalmente nem existe — ver global-setup.ts).
async function leSrcDoPosterDoTopo(
  page: import('@playwright/test').Page,
): Promise<string | null> {
  const img = page.getByRole('img', { name: /^Pôster de/ })
  if ((await img.count()) === 0) {
    return null
  }
  return img.getAttribute('src')
}

test('definir uma arte como principal materializa na pasta e atualiza o poster do topo (briefing 0062)', async ({
  page,
  request,
}) => {
  // Materializar/definir-principal exige um media_file 'present' de verdade (a
  // API grava na pasta REAL) -- semeia uma pasta /tmp com um filme parseavel e
  // escaneia, como em library.spec.ts (0028), em vez de importar so por API.
  const dir = mkdtempSync(join(tmpdir(), 'mmdb-e2e-art-'))
  let id = ''
  try {
    writeFileSync(join(dir, 'The Matrix (1999).mkv'), Buffer.alloc(2048, 7))

    await page.goto('/dashboard/library')
    await page.getByRole('link', { name: 'Adicionar datasource' }).click()
    await page.getByLabel('Caminho do datasource').fill(dir)
    await page.getByRole('button', { name: 'Adicionar datasource' }).click()
    await expect(page).toHaveURL('/dashboard/library')
    await expect(page.getByText(dir)).toBeVisible()
    await page.getByRole('button', { name: 'Escanear' }).click()

    await expect
      .poll(
        async () => {
          const body = await (
            await request.get('/api/v1/entities?limit=1000')
          ).json()
          const itens: Array<{ id: string; title: string }> =
            body.items ?? body ?? []
          const achado = itens.find((e) => e.title === 'The Matrix')
          id = achado?.id ?? ''
          return !!achado
        },
        { timeout: 20_000 },
      )
      .toBe(true)

    await page.goto(`/entities/${id}`)
    const srcAntes = await leSrcDoPosterDoTopo(page)

    // Envia um poster do dono (kind ja vem em "Poster" por padrao) -- vira um
    // candidato NAO principal na galeria, distinto de qualquer poster do fake
    // provider.
    const arte = page
      .locator('section')
      .filter({ has: page.getByRole('heading', { name: 'Artes', level: 2 }) })
    await arte
      .getByLabel('Arquivo de arte')
      .setInputFiles({ name: 'poster.png', mimeType: 'image/png', buffer: PNG })
    await expect(
      arte.getByRole('button', { name: /^Ver Pôster \d+$/ }).last(),
    ).toBeVisible()

    // Abre o card recem-enviado e define como principal -- PUT prefer direto (ja
    // e um asset, sem precisar do encadeamento de criar via /artwork/select
    // primeiro; esse encadeamento e' exercitado nos testes unitarios do
    // componente).
    await arte.getByRole('button', { name: /^Ver Pôster \d+$/ }).last().click()
    await page.getByRole('button', { name: 'Definir como principal' }).click()
    await expect(page.getByText('principal', { exact: true })).toBeVisible()
    await expect(page.getByText('salvo na pasta', { exact: true })).toBeVisible()
    await page.keyboard.press('Escape')

    // O poster do TOPO do RichDetailView reflete a nova arte principal, sem
    // reload de pagina (criterio de aceite do briefing 0062).
    await expect
      .poll(() => leSrcDoPosterDoTopo(page))
      .not.toBe(srcAntes)
  } finally {
    if (id) {
      await request.delete(`/api/v1/entities/${id}`).catch(() => {})
    }
    const dss = await (await request.get('/api/v1/library')).json()
    for (const s of dss.items ?? []) {
      await request.delete(`/api/v1/library/${s.id}`)
    }
    rmSync(dir, { recursive: true, force: true })
  }
})
