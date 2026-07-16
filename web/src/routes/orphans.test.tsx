import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { UserEvent } from '@testing-library/user-event'
import { OrphansScreen } from '@/routes/orphans'
import type { EntityCard } from '@/lib/api'
import { okJson } from '@/test/entity-fixtures'
import {
  criarFetch,
  erroJson,
  esperarChaves,
  renderComEspioes,
} from '@/test/harness'
import type { LinkMock } from '@/test/link-mock'

const CHAVES_ORFAS = ['["orphans"]', '["entities"]']

vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock as typeof LinkMock,
}))

function orfa(over: Partial<EntityCard> = {}): EntityCard {
  return {
    id: 'o1',
    type: 'movie',
    title: 'Órfã Um',
    status: 'cataloged',
    owned: false,
    file_count: 1,
    match_confidence: null,
    poster_url: null,
    catalog_completeness: null,
    ...over,
  }
}

// Par de órfãs (o1/o2) — cenário reusado pelos casos de seleção e lote.
const DUAS: EntityCard[] = [
  orfa({ id: 'o1', title: 'Órfã Um' }),
  orfa({ id: 'o2', title: 'Órfã Dois' }),
]

interface Espioes {
  onBatch: ReturnType<typeof vi.fn<(ids: string[]) => void>>
  onDelete: ReturnType<typeof vi.fn<(id: string) => void>>
}

interface Opcoes {
  cards?: EntityCard[]
  listaErro?: boolean
  batchErro?: { status: number; code: string; message: string }
}

// Handler de rotas das órfãs: GET /orphans, POST /entities/purge (lote), DELETE
// /entities/{id} (individual). A ordem checa a purga em lote ANTES do DELETE.
function montarFetch(opts: Opcoes, espioes: Espioes) {
  return criarFetch(({ path, method, init }) => {
    if (path === '/api/v1/orphans' && method === 'GET') {
      return opts.listaErro
        ? erroJson(500, 'x', 'falhou')
        : okJson(opts.cards ?? [])
    }
    if (path === '/api/v1/entities/purge' && method === 'POST') {
      const ids = (JSON.parse(String(init?.body)) as { ids: string[] }).ids
      espioes.onBatch(ids)
      return opts.batchErro
        ? erroJson(
            opts.batchErro.status,
            opts.batchErro.code,
            opts.batchErro.message,
          )
        : okJson({ removed: ids.length })
    }
    const del = path.match(/^\/api\/v1\/entities\/(.+)$/)
    if (del && method === 'DELETE') {
      espioes.onDelete(del[1])
      return okJson({ purged: true })
    }
    throw new Error(`rota nao mockada: ${method} ${path}`)
  })
}

function renderOrphans(opts: Opcoes = {}) {
  const espioes: Espioes = {
    onBatch: vi.fn<(ids: string[]) => void>(),
    onDelete: vi.fn<(id: string) => void>(),
  }
  vi.stubGlobal('fetch', montarFetch(opts, espioes))
  return { espioes, ...renderComEspioes(<OrphansScreen />) }
}

// Espera a fila aparecer (primeiro card renderizado).
function esperarLista() {
  return waitFor(() => expect(screen.getByText('Órfã Um')).toBeInTheDocument())
}

// Seleciona todas e clica em "Purgar selecionadas" (abre a confirmação em lote).
async function abrirConfirmacaoLote(user: UserEvent) {
  await user.click(screen.getByLabelText('Selecionar todas'))
  await user.click(screen.getByRole('button', { name: /Purgar selecionadas/ }))
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('OrphansScreen — fila de órfãs (0018)', () => {
  it('lista as órfãs (§5: lista as órfãs)', async () => {
    renderOrphans({ cards: DUAS })
    await esperarLista()
    expect(screen.getByText('Órfã Dois')).toBeInTheDocument()
  })

  it('mostra o estado vazio "Nenhuma órfã" (§5: estados vazio)', async () => {
    renderOrphans({ cards: [] })
    await waitFor(() =>
      expect(screen.getByText('Nenhuma órfã')).toBeInTheDocument(),
    )
  })

  it('mostra o estado de erro', async () => {
    renderOrphans({ listaErro: true })
    await waitFor(() =>
      expect(
        screen.getByText('Não foi possível carregar as órfãs'),
      ).toBeInTheDocument(),
    )
  })

  it('permite seleção individual e "selecionar todas" (§5: seleção em lote)', async () => {
    const user = userEvent.setup()
    renderOrphans({ cards: DUAS })
    await esperarLista()

    await user.click(screen.getByLabelText('Selecionar Órfã Um'))
    expect(screen.getByText('1 selecionada(s)')).toBeInTheDocument()

    await user.click(screen.getByLabelText('Selecionar todas'))
    expect(screen.getByText('2 selecionada(s)')).toBeInTheDocument()
  })

  it('purga em lote exige confirmação explícita: 1º clique não purga (§5: purga sempre explícita)', async () => {
    const user = userEvent.setup()
    const { espioes } = renderOrphans({ cards: [orfa()] })
    await esperarLista()

    // 1º clique só ABRE a confirmação — não chama a API.
    await abrirConfirmacaoLote(user)
    expect(screen.getByText(/Tem certeza\?/)).toBeInTheDocument()
    expect(espioes.onBatch).not.toHaveBeenCalled()
  })

  it('após confirmar, purga em lote chama purgeBatch e invalida orphans/entities', async () => {
    const user = userEvent.setup()
    const { espioes, invalidateSpy } = renderOrphans({ cards: DUAS })
    await esperarLista()

    await abrirConfirmacaoLote(user)
    // 2º passo: confirma de fato.
    await user.click(screen.getByRole('button', { name: 'Confirmar purga' }))

    await waitFor(() =>
      expect(espioes.onBatch).toHaveBeenCalledWith(['o1', 'o2']),
    )
    await esperarChaves(invalidateSpy, CHAVES_ORFAS)
  })

  it('cancelar a confirmação em lote não purga', async () => {
    const user = userEvent.setup()
    const { espioes } = renderOrphans({ cards: [orfa()] })
    await esperarLista()

    await abrirConfirmacaoLote(user)
    await user.click(screen.getByRole('button', { name: 'Cancelar' }))

    expect(screen.queryByText(/Tem certeza\?/)).not.toBeInTheDocument()
    expect(espioes.onBatch).not.toHaveBeenCalled()
  })

  it('purga individual exige confirmação: 1º clique não purga, 2º chama purgeEntity (§5: purga individual explícita)', async () => {
    const user = userEvent.setup()
    const { espioes, invalidateSpy } = renderOrphans({
      cards: [orfa({ id: 'o1' })],
    })
    await esperarLista()

    // 1º clique no card só abre a confirmação.
    await user.click(screen.getByRole('button', { name: 'Purgar Órfã Um' }))
    expect(espioes.onDelete).not.toHaveBeenCalled()
    expect(
      screen.getByText('Remove este item e não pode ser desfeito.'),
    ).toBeInTheDocument()

    // 2º passo: confirma.
    await user.click(screen.getByRole('button', { name: 'Confirmar purga' }))
    await waitFor(() => expect(espioes.onDelete).toHaveBeenCalledWith('o1'))
    await esperarChaves(invalidateSpy, CHAVES_ORFAS)
  })

  it('erro do backend na purga em lote aparece inline', async () => {
    const user = userEvent.setup()
    renderOrphans({
      cards: [orfa()],
      batchErro: {
        status: 422,
        code: 'invalid_body',
        message: 'Corpo inválido.',
      },
    })
    await esperarLista()

    await abrirConfirmacaoLote(user)
    await user.click(screen.getByRole('button', { name: 'Confirmar purga' }))

    await waitFor(() =>
      expect(screen.getByText('Corpo inválido.')).toBeInTheDocument(),
    )
  })
})
