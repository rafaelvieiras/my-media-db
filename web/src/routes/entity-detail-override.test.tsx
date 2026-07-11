import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { EntityDetailScreen } from '@/routes/entity-detail'
import { LanguageProvider } from '@/lib/language-context'
import type { OverrideBlob, ResolvedEntity } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { baseEntity, okJson, settingsPadrao } from '@/test/entity-fixtures'

// Mocka o router (o `vi.mock` é içado, então não dá para compartilhar via
// helper): `useParams` fixa o id; `Link` vira um <a> simples.
vi.mock('@tanstack/react-router', () => ({
  useParams: () => ({ id: 'e1' }),
  Link: (props: { children: ReactNode }) => <a href="#">{props.children}</a>,
}))

// Espiões dos verbos de escrita — os testes leem o `lang` e o corpo com que a UI
// bateu na API.
interface Espioes {
  onPatch: ReturnType<
    typeof vi.fn<(lang: string | null, body: unknown) => void>
  >
  onDelete: ReturnType<
    typeof vi.fn<(campo: string | undefined, lang: string | null) => void>
  >
}

interface Opcoes {
  blob?: OverrideBlob
  entityOver?: Partial<ResolvedEntity>
  patchErro?: { status: number; code: string; message: string }
}

// Roteia o fetch por método + caminho: settings, blob de override (GET), entity,
// arquivos, além do PATCH e do DELETE de override (que registram nos espiões).
function fetchRouter(opts: Opcoes, espioes: Espioes) {
  const entity = baseEntity(opts.entityOver)
  const blob = opts.blob ?? {}
  return vi.fn(async (url: string, init?: RequestInit) => {
    const u = new URL(String(url), 'http://localhost')
    const path = u.pathname
    const method = init?.method ?? 'GET'

    if (path === '/api/v1/settings/languages') {
      return okJson(settingsPadrao)
    }
    if (path === '/api/v1/entities/e1/override' && method === 'GET') {
      return okJson(blob)
    }
    if (path === '/api/v1/entities/e1/override' && method === 'PATCH') {
      espioes.onPatch(
        u.searchParams.get('lang'),
        JSON.parse(String(init?.body)),
      )
      if (opts.patchErro) {
        return {
          ok: false,
          status: opts.patchErro.status,
          json: async () => ({
            error: {
              code: opts.patchErro!.code,
              message: opts.patchErro!.message,
            },
          }),
        } as Response
      }
      return okJson(entity)
    }
    if (
      path.startsWith('/api/v1/entities/e1/override/') &&
      method === 'DELETE'
    ) {
      const campo = path.split('/').pop()
      espioes.onDelete(campo, u.searchParams.get('lang'))
      return okJson(entity)
    }
    if (path.startsWith('/api/v1/entities/e1/files')) {
      return okJson({ items: [] })
    }
    if (path.startsWith('/api/v1/entities/e1/assets')) {
      return okJson([])
    }
    if (path.startsWith('/api/v1/entities/e1')) {
      return okJson(entity)
    }
    throw new Error(`rota nao mockada: ${method} ${path}`)
  })
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  vi.restoreAllMocks()
})

function renderDetail(opts: Opcoes = {}): Espioes {
  const espioes: Espioes = {
    onPatch: vi.fn<(lang: string | null, body: unknown) => void>(),
    onDelete: vi.fn<(campo: string | undefined, lang: string | null) => void>(),
  }
  vi.stubGlobal('fetch', fetchRouter(opts, espioes))
  renderWithClient(
    <LanguageProvider>
      <EntityDetailScreen />
    </LanguageProvider>,
  )
  return espioes
}

// Abre o editor: espera a tela carregar e clica em "Editar".
async function abrirEditor(user: ReturnType<typeof userEvent.setup>) {
  await waitFor(() => expect(screen.getByText('Dune')).toBeInTheDocument())
  await user.click(screen.getByRole('button', { name: 'Editar' }))
  await screen.findByRole('button', { name: 'Salvar alterações' })
}

// Renderiza o detalhe e já abre o editor — devolve o `user` e os espiões.
async function editar(opts: Opcoes = {}) {
  const user = userEvent.setup()
  const espioes = renderDetail(opts)
  await abrirEditor(user)
  return { user, espioes }
}

// Clica em "Salvar alterações" e afirma que o PATCH saiu uma vez, no idioma
// principal, com exatamente o corpo esperado.
async function salvarEEsperarPatch(
  user: ReturnType<typeof userEvent.setup>,
  espioes: Espioes,
  corpo: unknown,
) {
  await user.click(screen.getByRole('button', { name: 'Salvar alterações' }))
  await waitFor(() => expect(espioes.onPatch).toHaveBeenCalledTimes(1))
  expect(espioes.onPatch).toHaveBeenCalledWith('pt-BR', corpo)
}

// Espera o editor sumir (voltou ao modo leitura: o botão Salvar some).
async function esperarEditorFechado() {
  await waitFor(() =>
    expect(
      screen.queryByRole('button', { name: 'Salvar alterações' }),
    ).not.toBeInTheDocument(),
  )
}

describe('OverrideEditor — edição por idioma', () => {
  it('(a) editar o título no idioma corrente faz PATCH com o lang e o corpo certos', async () => {
    const { user, espioes } = await editar()

    await user.type(screen.getByLabelText('Título'), 'Duna')
    // Idioma corrente = principal (pt-BR); corpo só com o campo alterado.
    await salvarEEsperarPatch(user, espioes, { title: 'Duna' })
  })

  it('(b) o selo "editado" aparece quando o blob tem override no idioma atual', async () => {
    // title overridado em pt-BR (idioma corrente) → selo; en não conta.
    renderDetail({ blob: { title: { 'pt-BR': 'Duna' } } })

    await waitFor(() => expect(screen.getByText('Dune')).toBeInTheDocument())
    expect(screen.getByText('editado')).toBeInTheDocument()
  })

  it('(b2) sem override no idioma atual não há selo "editado"', async () => {
    // Override só em en; lendo em pt-BR → nenhum selo.
    renderDetail({ blob: { title: { en: 'Dune (EN)' } } })

    await waitFor(() => expect(screen.getByText('Dune')).toBeInTheDocument())
    expect(screen.queryByText('editado')).not.toBeInTheDocument()
  })

  it('(c) "Limpar" chama clearOverrideField com o campo e o lang corrente', async () => {
    const { user, espioes } = await editar({
      blob: { description: { 'pt-BR': 'Sinopse pt' } },
    })

    await user.click(screen.getByRole('button', { name: 'Limpar descrição' }))

    await waitFor(() => expect(espioes.onDelete).toHaveBeenCalledTimes(1))
    expect(espioes.onDelete).toHaveBeenCalledWith('description', 'pt-BR')
  })

  it('(d) campo neutro grava escalar no PATCH (sem dimensão de idioma no corpo)', async () => {
    const { user, espioes } = await editar()

    await user.type(screen.getByLabelText('Título original'), 'Dune Zwei')
    // Escalar direto (o handler ignora o lang p/ neutros); a UI não monta mapa.
    await salvarEEsperarPatch(user, espioes, { original_title: 'Dune Zwei' })
  })

  it('(d2) "Limpar" de campo neutro chama clearOverrideField SEM lang', async () => {
    const { user, espioes } = await editar({
      blob: { original_title: 'Dune' },
    })

    await user.click(
      screen.getByRole('button', { name: 'Limpar título original' }),
    )

    await waitFor(() => expect(espioes.onDelete).toHaveBeenCalledTimes(1))
    // Neutro remove a chave inteira: DELETE sem ?lang= (searchParams devolve null).
    expect(espioes.onDelete).toHaveBeenCalledWith('original_title', null)
  })

  it('(e) editor de tags: adicionar um chip vira operação { add } no PATCH', async () => {
    const { user, espioes } = await editar()

    await user.type(screen.getByLabelText('Adicionar tag'), 'Clássico')
    await user.click(screen.getByRole('button', { name: 'Adicionar' }))
    await salvarEEsperarPatch(user, espioes, {
      tags: { add: ['Clássico'], remove: [] },
    })
  })

  it('(f) esvaziar um campo traduzível e salvar manda null (DEFINIR VAZIO ≠ limpar)', async () => {
    const { user, espioes } = await editar({
      blob: { description: { 'pt-BR': 'Sinopse pt' } },
    })

    // O campo foi semeado com o override do idioma; esvaziá-lo e Salvar = null.
    await user.clear(screen.getByLabelText('Descrição'))
    await salvarEEsperarPatch(user, espioes, { description: null })
    // Não é um DELETE (aquilo seria "limpar/herdar").
    expect(espioes.onDelete).not.toHaveBeenCalled()
  })

  it('(g) remover um chip vira operação { remove } no PATCH', async () => {
    const { user, espioes } = await editar({
      entityOver: { tags: ['Ação', 'Drama'] },
    })

    await user.click(screen.getByRole('button', { name: 'Remover tag Ação' }))
    await salvarEEsperarPatch(user, espioes, {
      tags: { add: [], remove: ['Ação'] },
    })
  })

  it('(h) salvar sem alterações fecha o editor sem chamar a API', async () => {
    const { user, espioes } = await editar()

    await user.click(screen.getByRole('button', { name: 'Salvar alterações' }))

    await esperarEditorFechado()
    expect(espioes.onPatch).not.toHaveBeenCalled()
    // Volta ao modo leitura (o botão Editar reaparece).
    expect(screen.getByRole('button', { name: 'Editar' })).toBeInTheDocument()
  })

  it('(i) erro 422 do PATCH aparece inline e o editor permanece aberto', async () => {
    const { user } = await editar({
      patchErro: {
        status: 422,
        code: 'invalid_override',
        message: 'campo não editorial',
      },
    })

    await user.type(screen.getByLabelText('Título'), 'Duna')
    await user.click(screen.getByRole('button', { name: 'Salvar alterações' }))

    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent(
        'campo não editorial',
      ),
    )
    // Continua em edição (não fechou).
    expect(
      screen.getByRole('button', { name: 'Salvar alterações' }),
    ).toBeInTheDocument()
  })

  it('(k) definir a nota local grava o número neutro no PATCH', async () => {
    const { user, espioes } = await editar()

    await user.type(screen.getByLabelText('Sua nota'), '9.5')
    // Neutra e numérica: escalar direto (sem dimensão de idioma).
    await salvarEEsperarPatch(user, espioes, { local_rating: 9.5 })
  })

  it('(k2) "Limpar sua nota" chama clearOverrideField SEM lang (campo neutro)', async () => {
    const { user, espioes } = await editar({ blob: { local_rating: 8 } })

    await user.click(screen.getByRole('button', { name: 'Limpar sua nota' }))

    await waitFor(() => expect(espioes.onDelete).toHaveBeenCalledTimes(1))
    expect(espioes.onDelete).toHaveBeenCalledWith('local_rating', null)
  })

  it('(k3) esvaziar a nota semeada e salvar manda null (definir vazio)', async () => {
    const { user, espioes } = await editar({ blob: { local_rating: 7 } })

    await user.clear(screen.getByLabelText('Sua nota'))
    await salvarEEsperarPatch(user, espioes, { local_rating: null })
    expect(espioes.onDelete).not.toHaveBeenCalled()
  })

  it('(l) a nota local resolvida aparece no hero (distinta da nota da fonte)', async () => {
    renderDetail({ entityOver: { local_rating: 9.5, vote_average: 8.2 } })

    await waitFor(() => expect(screen.getByText('Dune')).toBeInTheDocument())
    // A nota do dono (acessível por "Sua nota") e a da fonte convivem.
    expect(screen.getByLabelText('Sua nota: 9.5')).toBeInTheDocument()
    expect(screen.getByText('8.2')).toBeInTheDocument()
  })

  it('(j) "Cancelar" fecha o editor sem tocar a API', async () => {
    const { user, espioes } = await editar()

    await user.click(screen.getByRole('button', { name: 'Cancelar' }))

    await esperarEditorFechado()
    expect(espioes.onPatch).not.toHaveBeenCalled()
    expect(espioes.onDelete).not.toHaveBeenCalled()
  })
})
