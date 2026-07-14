import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ReviewScreen } from '@/routes/review'
import type { EntityCard, ExternalResult, SearchResult } from '@/lib/api'
import { baseEntity, okJson } from '@/test/entity-fixtures'
import {
  criarFetch,
  erroJson,
  esperarChaves,
  renderComEspioes,
} from '@/test/harness'
import type { LinkMock } from '@/test/link-mock'

const CHAVES_REVISAO = ['["review"]', '["entities"]', '["entity","e-baixa"]']

vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock as typeof LinkMock,
}))

// Idioma fixo (pt-BR): o painel de correção usa useLanguage para passar o idioma
// ativo à busca; isola do LanguageProvider real (sem tocar o endpoint de settings).
vi.mock('@/lib/language-context', () => ({
  useLanguage: () => ({
    lang: 'pt-BR',
    allowed: ['pt-BR'],
    primary: 'pt-BR',
    setLang: () => {},
  }),
}))

const draftCard: EntityCard = {
  id: 'e-draft',
  type: 'movie',
  title: 'Fita Sem Nome',
  status: 'draft',
  owned: true,
  file_count: 1,
  match_confidence: null,
  poster_url: null,
}

const baixaCard: EntityCard = {
  id: 'e-baixa',
  type: 'tv',
  title: 'Palpite Fraco',
  status: 'cataloged',
  owned: true,
  file_count: 1,
  match_confidence: 40,
  poster_url: null,
}

const externo: ExternalResult = {
  source: 'tmdb',
  source_id: '1234',
  type: 'movie',
  title: 'O Título Certo',
  year: '2021',
  overview: 'A identidade correta.',
  poster_url: null,
  saved: false,
}

interface Opcoes {
  fila?: EntityCard[]
  filaErro?: boolean
  search?: SearchResult
  confirmErro?: { status: number; code: string; message: string }
  correctErro?: { status: number; code: string; message: string }
  // Auto-match individual (0050): resultado devolvido por POST .../auto-match.
  autoMatchResultado?: {
    resolved: boolean
    method: 'local' | 'external' | 'none'
    score: number
    target: { id: string; title: string; year: number | null } | null
  }
  // Auto-match em lote (0050): resumo devolvido por POST /review/auto-match.
  autoMatchLoteResultado?: {
    resolved_count: number
    pending_count: number
    items: Array<{
      id: string
      resolved: boolean
      method: 'local' | 'external' | 'none'
      score: number
    }>
  }
}

interface Espioes {
  onConfirm: ReturnType<typeof vi.fn<(id: string) => void>>
  onCorrect: ReturnType<typeof vi.fn<(id: string, body: unknown) => void>>
  onAutoMatch: ReturnType<typeof vi.fn<(id: string) => void>>
  onAutoMatchLote: ReturnType<typeof vi.fn<() => void>>
}

// Handler de rotas da tela de revisão: GET /review (a fila), GET /search (a
// correção), POST /entities/{id}/confirm e POST /entities/{id}/correct, e os
// dois endpoints do auto-match (0050): POST /entities/{id}/auto-match (item) e
// POST /review/auto-match (lote).
function montarFetch(opts: Opcoes, espioes: Espioes) {
  return criarFetch(({ path, method, init, u }) => {
    if (path === '/api/v1/review' && method === 'GET') {
      return opts.filaErro
        ? erroJson(500, 'x', 'falhou')
        : okJson(opts.fila ?? [])
    }
    if (path === '/api/v1/search' && method === 'GET') {
      return okJson(
        opts.search ?? {
          query: u.searchParams.get('q') ?? '',
          local: [],
          external: [externo],
          external_available: true,
        },
      )
    }
    const confirm = path.match(/^\/api\/v1\/entities\/(.+)\/confirm$/)
    if (confirm && method === 'POST') {
      espioes.onConfirm(confirm[1])
      return opts.confirmErro
        ? erroJson(
            opts.confirmErro.status,
            opts.confirmErro.code,
            opts.confirmErro.message,
          )
        : okJson(baseEntity({ id: confirm[1] }))
    }
    const correct = path.match(/^\/api\/v1\/entities\/(.+)\/correct$/)
    if (correct && method === 'POST') {
      espioes.onCorrect(correct[1], JSON.parse(String(init?.body)))
      return opts.correctErro
        ? erroJson(
            opts.correctErro.status,
            opts.correctErro.code,
            opts.correctErro.message,
          )
        : okJson(baseEntity({ id: correct[1] }))
    }
    const autoMatch = path.match(/^\/api\/v1\/entities\/(.+)\/auto-match$/)
    if (autoMatch && method === 'POST') {
      espioes.onAutoMatch(autoMatch[1])
      return okJson(
        opts.autoMatchResultado ?? {
          resolved: false,
          method: 'none',
          score: 0,
          target: null,
        },
      )
    }
    if (path === '/api/v1/review/auto-match' && method === 'POST') {
      espioes.onAutoMatchLote()
      return okJson(
        opts.autoMatchLoteResultado ?? {
          resolved_count: 0,
          pending_count: 0,
          items: [],
        },
      )
    }
    throw new Error(`rota nao mockada: ${method} ${path}`)
  })
}

function renderReview(opts: Opcoes = {}) {
  const espioes: Espioes = {
    onConfirm: vi.fn<(id: string) => void>(),
    onCorrect: vi.fn<(id: string, body: unknown) => void>(),
    onAutoMatch: vi.fn<(id: string) => void>(),
    onAutoMatchLote: vi.fn<() => void>(),
  }
  vi.stubGlobal('fetch', montarFetch(opts, espioes))
  return { espioes, ...renderComEspioes(<ReviewScreen />) }
}

// Cenário base de um card de baixa confiança já renderizado (o setup comum dos
// casos de confirmar/corrigir): monta o fetch, renderiza e espera o card.
async function renderBaixa(opts: Opcoes = { fila: [baixaCard] }) {
  const user = userEvent.setup()
  const r = renderReview(opts)
  await waitFor(() =>
    expect(screen.getByText('Palpite Fraco')).toBeInTheDocument(),
  )
  return { user, ...r }
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('ReviewScreen — fila de revisão (0018)', () => {
  it('lista draft e baixa confiança com o motivo certo (§5: draft + baixa confiança)', async () => {
    renderReview({ fila: [draftCard, baixaCard] })

    await waitFor(() =>
      expect(screen.getByText('Fita Sem Nome')).toBeInTheDocument(),
    )
    expect(screen.getByText('não identificado')).toBeInTheDocument()
    expect(screen.getByText('Palpite Fraco')).toBeInTheDocument()
    expect(screen.getByText('baixa confiança (40%)')).toBeInTheDocument()
  })

  it('mostra o estado vazio "Nada para revisar" (§5: estados vazio)', async () => {
    renderReview({ fila: [] })
    await waitFor(() =>
      expect(screen.getByText('Nada para revisar')).toBeInTheDocument(),
    )
  })

  it('mostra o estado de erro', async () => {
    renderReview({ filaErro: true })
    await waitFor(() =>
      expect(
        screen.getByText('Não foi possível carregar a revisão'),
      ).toBeInTheDocument(),
    )
  })

  it('Confirmar chama confirmMatch e invalida review/entities/entity (§5: confirmar sobe a confiança)', async () => {
    const { user, espioes, invalidateSpy } = await renderBaixa()
    await user.click(screen.getByRole('button', { name: /Confirmar/ }))

    await waitFor(() =>
      expect(espioes.onConfirm).toHaveBeenCalledWith('e-baixa'),
    )
    await esperarChaves(invalidateSpy, CHAVES_REVISAO)
  })

  it('Corrigir busca e chama correctMatch com o source_id escolhido (§5: corrigir grava external_ids + re-enriquece)', async () => {
    const { user, espioes, invalidateSpy } = await renderBaixa()
    // Abre o painel de correção e busca o título certo.
    await user.click(screen.getByRole('button', { name: /Corrigir/ }))
    await user.type(screen.getByLabelText('Buscar o título correto'), 'certo')
    // Escolhe o resultado externo → CORRIGE com o source_id dele.
    await user.click(await screen.findByRole('button', { name: 'Usar este' }))

    await waitFor(() =>
      expect(espioes.onCorrect).toHaveBeenCalledWith('e-baixa', {
        source: 'tmdb',
        source_id: '1234',
      }),
    )
    await esperarChaves(invalidateSpy, CHAVES_REVISAO)
  })

  it('erro do backend na correção aparece inline', async () => {
    const { user } = await renderBaixa({
      fila: [baixaCard],
      correctErro: {
        status: 502,
        code: 'upstream_error',
        message: 'A fonte externa falhou.',
      },
    })
    await user.click(screen.getByRole('button', { name: /Corrigir/ }))
    await user.type(screen.getByLabelText('Buscar o título correto'), 'certo')
    await user.click(await screen.findByRole('button', { name: 'Usar este' }))

    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent(
        'A fonte externa falhou.',
      ),
    )
  })
})

describe('ReviewScreen — auto-match (0050)', () => {
  it('"Auto-sincronizar" individual resolve o item e some da fila (§4: resolved true invalida a fila)', async () => {
    const { user, espioes, invalidateSpy } = await renderBaixa({
      fila: [baixaCard],
      autoMatchResultado: {
        resolved: true,
        method: 'local',
        score: 96.4,
        target: { id: 'e-alvo', title: 'Palpite Certo', year: 2020 },
      },
    })

    await user.click(screen.getByRole('button', { name: /Auto-sincronizar$/ }))

    await waitFor(() =>
      expect(espioes.onAutoMatch).toHaveBeenCalledWith('e-baixa'),
    )
    await waitFor(() =>
      expect(
        screen.getByText(/Resolvido automaticamente via catálogo local/),
      ).toBeInTheDocument(),
    )
    expect(screen.getByText(/score 96\.4/)).toBeInTheDocument()
    await esperarChaves(invalidateSpy, CHAVES_REVISAO)
  })

  it('"Auto-sincronizar" sem candidato mostra aviso neutro e o item continua na fila (§4: resolved false não aplica nada)', async () => {
    const { user, espioes } = await renderBaixa({
      fila: [baixaCard],
      autoMatchResultado: {
        resolved: false,
        method: 'none',
        score: 0,
        target: null,
      },
    })

    await user.click(screen.getByRole('button', { name: /Auto-sincronizar$/ }))

    await waitFor(() =>
      expect(espioes.onAutoMatch).toHaveBeenCalledWith('e-baixa'),
    )
    await waitFor(() =>
      expect(
        screen.getByText(
          'Nenhum candidato confiável encontrado — revise manualmente.',
        ),
      ).toBeInTheDocument(),
    )
    // O card continua na fila (não some) — o item permanece intacto.
    expect(screen.getByText('Palpite Fraco')).toBeInTheDocument()
  })

  it('"Auto-sincronizar elegíveis" (lote) mostra o resumo agregado e invalida a fila', async () => {
    const user = userEvent.setup()
    const { espioes, invalidateSpy } = renderReview({
      fila: [baixaCard],
      autoMatchLoteResultado: {
        resolved_count: 22,
        pending_count: 8,
        items: [
          { id: 'e-baixa', resolved: true, method: 'local', score: 95.1 },
        ],
      },
    })
    await waitFor(() =>
      expect(screen.getByText('Palpite Fraco')).toBeInTheDocument(),
    )

    await user.click(
      screen.getByRole('button', { name: /Auto-sincronizar elegíveis/ }),
    )

    await waitFor(() => expect(espioes.onAutoMatchLote).toHaveBeenCalled())
    await waitFor(() =>
      expect(
        screen.getByText(
          '22 resolvidos automaticamente, 8 continuam para revisão manual.',
        ),
      ).toBeInTheDocument(),
    )
    await esperarChaves(invalidateSpy, ['["review"]', '["entities"]'])
  })
})
