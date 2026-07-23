import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { MouseEventHandler, ReactNode } from 'react'
import { EntityEditScreen } from '@/routes/entity-edit'
import { LanguageProvider } from '@/lib/language-context'
import type { OverrideBlob, ResolvedEntity } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import {
  baseEntity,
  okFields,
  okJson,
  settingsPadrao,
} from '@/test/entity-fixtures'

// Rota `/entities/$id/edit` (0030): a edição do override dirigida por schema. Cobre
// a semântica por-idioma preservada do editor antigo (0016): definir vazio × Limpar,
// tags {add,remove}, neutro escalar, número, 422 inline e a navegação de volta.
const spies = vi.hoisted(() => ({ navigate: vi.fn() }))

vi.mock('@tanstack/react-router', () => ({
  useParams: () => ({ id: 'e1' }),
  useNavigate: () => spies.navigate,
  // Repassa `onClick` (0073: o guard de "sair sem salvar" intercepta o clique
  // no link "Voltar ao item" via preventDefault + confirm) — antes o mock
  // descartava qualquer prop além de `children`.
  Link: ({
    children,
    onClick,
  }: {
    children: ReactNode
    onClick?: MouseEventHandler<HTMLAnchorElement>
  }) => (
    <a href="#" onClick={onClick}>
      {children}
    </a>
  ),
}))

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
    if (path === '/api/v1/schema/entity-fields') {
      return okFields()
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
      espioes.onDelete(path.split('/').pop(), u.searchParams.get('lang'))
      return okJson(entity)
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
  spies.navigate.mockReset()
})

function renderEdit(opts: Opcoes = {}): Espioes {
  const espioes: Espioes = {
    onPatch: vi.fn<(lang: string | null, body: unknown) => void>(),
    onDelete: vi.fn<(campo: string | undefined, lang: string | null) => void>(),
  }
  vi.stubGlobal('fetch', fetchRouter(opts, espioes))
  renderWithClient(
    <LanguageProvider>
      <EntityEditScreen />
    </LanguageProvider>,
  )
  return espioes
}

// Espera o form carregar (o botão Salvar surge quando entity+blob+descritores chegam).
async function esperarForm() {
  await screen.findByRole('button', { name: 'Salvar alterações' })
}

async function editar(opts: Opcoes = {}) {
  const user = userEvent.setup()
  const espioes = renderEdit(opts)
  await esperarForm()
  return { user, espioes }
}

async function salvarEEsperarPatch(
  user: ReturnType<typeof userEvent.setup>,
  espioes: Espioes,
  corpo: unknown,
) {
  await user.click(screen.getByRole('button', { name: 'Salvar alterações' }))
  await waitFor(() => expect(espioes.onPatch).toHaveBeenCalledTimes(1))
  expect(espioes.onPatch).toHaveBeenCalledWith('pt-BR', corpo)
}

describe('EntityEditScreen — edição por idioma', () => {
  it('editar o título faz PATCH com lang e corpo certos', async () => {
    const { user, espioes } = await editar()
    await user.type(screen.getByLabelText('Título'), 'Duna')
    await salvarEEsperarPatch(user, espioes, { title: 'Duna' })
  })

  it('"Limpar" de campo traduzível chama DELETE com o lang corrente', async () => {
    const { user, espioes } = await editar({
      blob: { description: { 'pt-BR': 'Sinopse pt' } },
    })
    await user.click(screen.getByRole('button', { name: 'Limpar sinopse' }))
    await waitFor(() => expect(espioes.onDelete).toHaveBeenCalledTimes(1))
    expect(espioes.onDelete).toHaveBeenCalledWith('description', 'pt-BR')
  })

  it('campo neutro grava escalar (sem dimensão de idioma)', async () => {
    const { user, espioes } = await editar()
    await user.type(screen.getByLabelText('Título original'), 'Dune Zwei')
    await salvarEEsperarPatch(user, espioes, { original_title: 'Dune Zwei' })
  })

  it('"Limpar" de campo neutro chama DELETE SEM lang', async () => {
    const { user, espioes } = await editar({ blob: { original_title: 'Dune' } })
    await user.click(
      screen.getByRole('button', { name: 'Limpar título original' }),
    )
    await waitFor(() => expect(espioes.onDelete).toHaveBeenCalledTimes(1))
    expect(espioes.onDelete).toHaveBeenCalledWith('original_title', null)
  })

  it('tags: adicionar um chip vira { add } no PATCH', async () => {
    const { user, espioes } = await editar()
    await user.type(screen.getByLabelText('Adicionar tag'), 'Clássico')
    await user.click(screen.getByRole('button', { name: 'Adicionar' }))
    await salvarEEsperarPatch(user, espioes, {
      tags: { add: ['Clássico'], remove: [] },
    })
  })

  it('tags: remover um chip vira { remove } no PATCH', async () => {
    const { user, espioes } = await editar({
      entityOver: { tags: ['Ação', 'Drama'] },
    })
    await user.click(screen.getByRole('button', { name: 'Remover tag Ação' }))
    await salvarEEsperarPatch(user, espioes, {
      tags: { add: [], remove: ['Ação'] },
    })
  })

  it('esvaziar um campo traduzível semeado manda null (definir vazio ≠ limpar)', async () => {
    const { user, espioes } = await editar({
      blob: { description: { 'pt-BR': 'Sinopse pt' } },
    })
    await user.clear(screen.getByLabelText('Sinopse'))
    await salvarEEsperarPatch(user, espioes, { description: null })
    expect(espioes.onDelete).not.toHaveBeenCalled()
  })

  it('definir a nota local grava o número neutro', async () => {
    const { user, espioes } = await editar()
    await user.type(screen.getByLabelText('Sua nota'), '9.5')
    await salvarEEsperarPatch(user, espioes, { local_rating: 9.5 })
  })

  it('esvaziar a nota semeada e salvar manda null', async () => {
    const { user, espioes } = await editar({ blob: { local_rating: 7 } })
    await user.clear(screen.getByLabelText('Sua nota'))
    await salvarEEsperarPatch(user, espioes, { local_rating: null })
    expect(espioes.onDelete).not.toHaveBeenCalled()
  })

  it('erro 422 do PATCH aparece inline e o form permanece', async () => {
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
    expect(spies.navigate).not.toHaveBeenCalled()
  })

  it('salvar sem alterações volta ao detalhe sem chamar a API', async () => {
    const { user, espioes } = await editar()
    await user.click(screen.getByRole('button', { name: 'Salvar alterações' }))
    await waitFor(() =>
      expect(spies.navigate).toHaveBeenCalledWith({
        to: '/entities/$id',
        params: { id: 'e1' },
      }),
    )
    expect(espioes.onPatch).not.toHaveBeenCalled()
  })

  it('Cancelar volta ao detalhe sem gravar', async () => {
    const { user, espioes } = await editar()
    await user.click(screen.getByRole('button', { name: 'Cancelar' }))
    expect(spies.navigate).toHaveBeenCalledWith({
      to: '/entities/$id',
      params: { id: 'e1' },
    })
    expect(espioes.onPatch).not.toHaveBeenCalled()
    expect(espioes.onDelete).not.toHaveBeenCalled()
  })

  it('id inexistente (404) mostra o estado de erro', async () => {
    const espioes: Espioes = { onPatch: vi.fn(), onDelete: vi.fn() }
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        const u = new URL(String(url), 'http://localhost')
        if (u.pathname === '/api/v1/settings/languages') {
          return okJson(settingsPadrao)
        }
        if (u.pathname === '/api/v1/schema/entity-fields') {
          return okFields()
        }
        // entity e override respondem 404.
        return {
          ok: false,
          status: 404,
          json: async () => ({
            error: { code: 'not_found', message: 'não encontrado' },
          }),
        } as Response
      }),
    )
    renderWithClient(
      <LanguageProvider>
        <EntityEditScreen />
      </LanguageProvider>,
    )
    expect(
      await screen.findByText('Não foi possível carregar o item'),
    ).toBeInTheDocument()
    void espioes
  })
})

// Guard de "sair sem salvar" (0073): os 3 blocos (SchemaForm/ExternalIdsEditor/
// CreditsEditor) reportam seu próprio "sujo" via onDirtyChange; a rota combina e
// intercepta o link "Voltar ao item" + o beforeunload nativo.
describe('EntityEditScreen — guard de sair sem salvar (0073)', () => {
  function clicarVoltar() {
    fireEvent.click(screen.getByRole('link', { name: /Voltar ao item/ }))
  }

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('sem alterações, "Voltar ao item" navega direto sem confirmação', async () => {
    const confirmar = vi.fn()
    vi.stubGlobal('confirm', confirmar)
    await editar()
    // Sem sujeira, o guard não intercepta o clique — a navegação segue pelo
    // comportamento padrão do Link (não simulado pelo mock deste arquivo).
    clicarVoltar()
    expect(confirmar).not.toHaveBeenCalled()
    // O guard nunca chama `navigate` explicitamente neste caso (não é ele
    // quem navega quando não há sujeira — é o próprio Link).
    expect(spies.navigate).not.toHaveBeenCalled()
  })

  it('editar o Título dispara confirmação; cancelar mantém a edição', async () => {
    const confirmar = vi.fn(() => false)
    vi.stubGlobal('confirm', confirmar)
    const { user } = await editar()
    await user.type(screen.getByLabelText('Título'), 'Duna')
    clicarVoltar()
    expect(confirmar).toHaveBeenCalledTimes(1)
    expect(spies.navigate).not.toHaveBeenCalled()
    expect((screen.getByLabelText('Título') as HTMLInputElement).value).toBe(
      'Duna',
    )
  })

  it('editar o Título e confirmar a saída navega', async () => {
    vi.stubGlobal(
      'confirm',
      vi.fn(() => true),
    )
    const { user } = await editar()
    await user.type(screen.getByLabelText('Título'), 'Duna')
    clicarVoltar()
    expect(spies.navigate).toHaveBeenCalledWith({
      to: '/entities/$id',
      params: { id: 'e1' },
    })
  })

  it('editar só os IDs externos dispara confirmação', async () => {
    const confirmar = vi.fn(() => false)
    vi.stubGlobal('confirm', confirmar)
    await editar()
    fireEvent.click(screen.getByText('Adicionar fonte'))
    fireEvent.change(screen.getByLabelText('Fonte do id externo'), {
      target: { value: 'tmdb' },
    })
    clicarVoltar()
    expect(confirmar).toHaveBeenCalledTimes(1)
    expect(spies.navigate).not.toHaveBeenCalled()
  })

  it('reordenar só o elenco (sem salvar) dispara confirmação', async () => {
    const confirmar = vi.fn(() => false)
    vi.stubGlobal('confirm', confirmar)
    await editar({
      entityOver: {
        cast: [
          {
            source_id: '1',
            name: 'Ana',
            character: 'Heroína',
            order: 0,
          },
          {
            source_id: '2',
            name: 'Beto',
            character: 'Vilão',
            order: 1,
          },
        ],
      },
    })
    fireEvent.click(screen.getAllByLabelText('Mover para baixo')[0])
    clicarVoltar()
    expect(confirmar).toHaveBeenCalledTimes(1)
    expect(spies.navigate).not.toHaveBeenCalled()
  })

  it('dispara o aviso nativo (beforeunload) só enquanto há alteração pendente', async () => {
    const { user } = await editar()
    const evento = new Event('beforeunload', { cancelable: true })
    const preventDefault = vi.spyOn(evento, 'preventDefault')

    // Pristino: nada registrado, o evento não é interceptado.
    window.dispatchEvent(evento)
    expect(preventDefault).not.toHaveBeenCalled()

    await user.type(screen.getByLabelText('Título'), 'D')
    const eventoSujo = new Event('beforeunload', { cancelable: true })
    const preventDefaultSujo = vi.spyOn(eventoSujo, 'preventDefault')
    window.dispatchEvent(eventoSujo)
    expect(preventDefaultSujo).toHaveBeenCalled()
  })
})
