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

// Pair of orphans (o1/o2) — scenario reused by the selection and batch cases.
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

// Route handler for the orphans: GET /orphans, POST /entities/purge (batch), DELETE
// /entities/{id} (individual). The order checks the batch purge BEFORE the DELETE.
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

// Waits for the queue to appear (first card rendered).
function esperarLista() {
  return waitFor(() => expect(screen.getByText('Órfã Um')).toBeInTheDocument())
}

// Selects all and clicks "Purge selected" (opens the batch confirmation).
async function abrirConfirmacaoLote(user: UserEvent) {
  await user.click(screen.getByLabelText('Select all'))
  await user.click(screen.getByRole('button', { name: /Purge selected/ }))
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('OrphansScreen — orphans queue (0018)', () => {
  it('lists the orphans (§5: lists the orphans)', async () => {
    renderOrphans({ cards: DUAS })
    await esperarLista()
    expect(screen.getByText('Órfã Dois')).toBeInTheDocument()
  })

  it('shows the empty state "No orphans" (§5: empty states)', async () => {
    renderOrphans({ cards: [] })
    await waitFor(() =>
      expect(screen.getByText('No orphans')).toBeInTheDocument(),
    )
  })

  it('shows the error state', async () => {
    renderOrphans({ listaErro: true })
    await waitFor(() =>
      expect(
        screen.getByText('Could not load the orphans'),
      ).toBeInTheDocument(),
    )
  })

  it('allows individual selection and "select all" (§5: batch selection)', async () => {
    const user = userEvent.setup()
    renderOrphans({ cards: DUAS })
    await esperarLista()

    await user.click(screen.getByLabelText('Select Órfã Um'))
    expect(screen.getByText('1 selected')).toBeInTheDocument()

    await user.click(screen.getByLabelText('Select all'))
    expect(screen.getByText('2 selected')).toBeInTheDocument()
  })

  it('batch purge requires explicit confirmation: the 1st click does not purge (§5: purge is always explicit)', async () => {
    const user = userEvent.setup()
    const { espioes } = renderOrphans({ cards: [orfa()] })
    await esperarLista()

    // The 1st click only OPENS the confirmation — it does not call the API.
    await abrirConfirmacaoLote(user)
    expect(screen.getByText(/Are you sure\?/)).toBeInTheDocument()
    expect(espioes.onBatch).not.toHaveBeenCalled()
  })

  it('after confirming, batch purge calls purgeBatch and invalidates orphans/entities', async () => {
    const user = userEvent.setup()
    const { espioes, invalidateSpy } = renderOrphans({ cards: DUAS })
    await esperarLista()

    await abrirConfirmacaoLote(user)
    // 2nd step: it actually confirms.
    await user.click(screen.getByRole('button', { name: 'Confirm purge' }))

    await waitFor(() =>
      expect(espioes.onBatch).toHaveBeenCalledWith(['o1', 'o2']),
    )
    await esperarChaves(invalidateSpy, CHAVES_ORFAS)
  })

  it('cancelling the batch confirmation does not purge', async () => {
    const user = userEvent.setup()
    const { espioes } = renderOrphans({ cards: [orfa()] })
    await esperarLista()

    await abrirConfirmacaoLote(user)
    await user.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(screen.queryByText(/Are you sure\?/)).not.toBeInTheDocument()
    expect(espioes.onBatch).not.toHaveBeenCalled()
  })

  it('individual purge requires confirmation: the 1st click does not purge, the 2nd calls purgeEntity (§5: individual purge is explicit)', async () => {
    const user = userEvent.setup()
    const { espioes, invalidateSpy } = renderOrphans({
      cards: [orfa({ id: 'o1' })],
    })
    await esperarLista()

    // The 1st click on the card only opens the confirmation.
    await user.click(screen.getByRole('button', { name: 'Purge Órfã Um' }))
    expect(espioes.onDelete).not.toHaveBeenCalled()
    expect(
      screen.getByText('Removes this item and cannot be undone.'),
    ).toBeInTheDocument()

    // 2nd step: it confirms.
    await user.click(screen.getByRole('button', { name: 'Confirm purge' }))
    await waitFor(() => expect(espioes.onDelete).toHaveBeenCalledWith('o1'))
    await esperarChaves(invalidateSpy, CHAVES_ORFAS)
  })

  it('a backend error on the batch purge shows inline', async () => {
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
    await user.click(screen.getByRole('button', { name: 'Confirm purge' }))

    await waitFor(() =>
      expect(screen.getByText('Corpo inválido.')).toBeInTheDocument(),
    )
  })
})
