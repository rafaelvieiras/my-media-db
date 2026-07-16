import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ReviewScreen } from '@/routes/review'
import type {
  EntityCard,
  ExternalResult,
  MatchPreview,
  SearchResult,
} from '@/lib/api'
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

// Prévia (0060) sem candidato — o default do backend quando nada bate em
// nenhuma fonte.
const previewVazia: MatchPreview = {
  found: false,
  method: 'none',
  score: 0,
  meets_auto_threshold: false,
  target: null,
}

// Prévia (0060) com candidato do CATÁLOGO LOCAL, abaixo do limiar automático —
// o caso central do briefing: o dono vê e decide aplicar mesmo assim.
const previewLocal: MatchPreview = {
  found: true,
  method: 'local',
  score: 87.3,
  meets_auto_threshold: false,
  target: {
    title: 'Duna',
    year: 2021,
    poster_url: '/api/v1/entities/8f2a1c/assets/poster',
    local_id: '8f2a1c',
    source: null,
    source_id: null,
  },
}

// Prévia (0060) com candidato EXTERNO, batendo o limiar automático.
const previewExterna: MatchPreview = {
  found: true,
  method: 'external',
  score: 95.1,
  meets_auto_threshold: true,
  target: {
    title: 'Duna: Parte Dois',
    year: 2024,
    poster_url: 'https://image.tmdb.org/t/p/w500/poster.jpg',
    local_id: null,
    source: 'tmdb',
    source_id: '603',
  },
}

interface Opcoes {
  fila?: EntityCard[]
  filaErro?: boolean
  search?: SearchResult
  confirmErro?: { status: number; code: string; message: string }
  correctErro?: { status: number; code: string; message: string }
  confirmCandidateErro?: { status: number; code: string; message: string }
  // Prévia de candidato (0060): resultado devolvido por GET .../match-preview
  // para QUALQUER card da fila (os testes usam um único card por vez).
  preview?: MatchPreview
  previewErro?: boolean
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
  onConfirmCandidate: ReturnType<
    typeof vi.fn<(id: string, body: unknown) => void>
  >
  onAutoMatchLote: ReturnType<typeof vi.fn<() => void>>
}

// Handler de rotas da tela de revisão: GET /review (a fila), GET /search (a
// correção), GET .../match-preview (0060, a prévia), POST /entities/{id}/confirm,
// POST /entities/{id}/correct, POST /entities/{id}/confirm-candidate (0060) e o
// endpoint do auto-match em LOTE (0050), POST /review/auto-match.
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
    const preview = path.match(/^\/api\/v1\/entities\/(.+)\/match-preview$/)
    if (preview && method === 'GET') {
      return opts.previewErro
        ? erroJson(502, 'upstream_error', 'a fonte externa falhou')
        : okJson(opts.preview ?? previewVazia)
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
    const confirmCandidate = path.match(
      /^\/api\/v1\/entities\/(.+)\/confirm-candidate$/,
    )
    if (confirmCandidate && method === 'POST') {
      espioes.onConfirmCandidate(
        confirmCandidate[1],
        JSON.parse(String(init?.body)),
      )
      return opts.confirmCandidateErro
        ? erroJson(
            opts.confirmCandidateErro.status,
            opts.confirmCandidateErro.code,
            opts.confirmCandidateErro.message,
          )
        : okJson(baseEntity({ id: confirmCandidate[1] }))
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
    onConfirmCandidate: vi.fn<(id: string, body: unknown) => void>(),
    onAutoMatchLote: vi.fn<() => void>(),
  }
  vi.stubGlobal('fetch', montarFetch(opts, espioes))
  return { espioes, ...renderComEspioes(<ReviewScreen />) }
}

// Cenário base de um card de baixa confiança já renderizado (o setup comum dos
// casos de confirmar/corrigir): monta o fetch, renderiza e espera o card. Por
// padrão a prévia (0060) não acha candidato (`previewVazia`) — o mesmo
// comportamento "sem metadados externos" de antes do briefing 0060 — a menos
// que o cenário informe outra.
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

  it('"Manter como está" (sem candidato na prévia) chama confirmMatch e invalida review/entities/entity (§5: comportamento antigo do "Confirmar")', async () => {
    const { user, espioes, invalidateSpy } = await renderBaixa({
      fila: [baixaCard],
      preview: previewVazia,
    })
    await waitFor(() =>
      expect(
        screen.getByText(
          'nenhum candidato encontrado — nem no catálogo local, nem na fonte externa',
        ),
      ).toBeInTheDocument(),
    )
    await user.click(
      screen.getByRole('button', {
        name: 'Manter como está (sem metadados externos)',
      }),
    )

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

describe('ReviewScreen — prévia de candidato (0060)', () => {
  it('prévia com candidato local renderiza pôster/título/ano/score/fonte e o botão "Confirmar candidato"', async () => {
    await renderBaixa({ fila: [baixaCard], preview: previewLocal })

    await waitFor(() => expect(screen.getByText('Duna')).toBeInTheDocument())
    expect(screen.getByAltText('Pôster de Duna')).toBeInTheDocument()
    expect(screen.getByText('2021')).toBeInTheDocument()
    expect(screen.getByText('score 87.3')).toBeInTheDocument()
    expect(screen.getByText('catálogo local')).toBeInTheDocument()
    expect(screen.getByText('confira antes de confirmar')).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Confirmar candidato' }),
    ).toBeInTheDocument()
  })

  it('prévia com candidato externo acima do limiar mostra o selo "match forte" e fonte "provedor externo"', async () => {
    await renderBaixa({ fila: [baixaCard], preview: previewExterna })

    await waitFor(() =>
      expect(screen.getByText('Duna: Parte Dois')).toBeInTheDocument(),
    )
    expect(screen.getByText('provedor externo')).toBeInTheDocument()
    expect(screen.getByText('match forte')).toBeInTheDocument()
  })

  it('prévia sem candidato renderiza a mensagem vazia e o botão "Manter como está" (0060)', async () => {
    await renderBaixa({ fila: [baixaCard], preview: previewVazia })

    await waitFor(() =>
      expect(
        screen.getByText(
          'nenhum candidato encontrado — nem no catálogo local, nem na fonte externa',
        ),
      ).toBeInTheDocument(),
    )
    expect(
      screen.getByRole('button', {
        name: 'Manter como está (sem metadados externos)',
      }),
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Confirmar candidato' }),
    ).not.toBeInTheDocument()
  })

  it('falha ao buscar a prévia mostra erro curto e degrada para "Manter como está" sem travar o card', async () => {
    await renderBaixa({ fila: [baixaCard], previewErro: true })

    await waitFor(() =>
      expect(
        screen.getByText(
          'Não foi possível buscar um candidato — tente novamente mais tarde.',
        ),
      ).toBeInTheDocument(),
    )
    expect(
      screen.getByRole('button', {
        name: 'Manter como está (sem metadados externos)',
      }),
    ).toBeInTheDocument()
    // "Corrigir" continua disponível mesmo com a prévia em erro.
    expect(screen.getByRole('button', { name: /Corrigir/ })).toBeInTheDocument()
  })

  it('clicar em "Confirmar candidato" (local) chama confirmCandidate com o payload local_id e invalida a fila', async () => {
    const { user, espioes, invalidateSpy } = await renderBaixa({
      fila: [baixaCard],
      preview: previewLocal,
    })
    await user.click(
      await screen.findByRole('button', { name: 'Confirmar candidato' }),
    )

    await waitFor(() =>
      expect(espioes.onConfirmCandidate).toHaveBeenCalledWith('e-baixa', {
        method: 'local',
        local_id: '8f2a1c',
      }),
    )
    await esperarChaves(invalidateSpy, CHAVES_REVISAO)
  })

  it('clicar em "Confirmar candidato" (externo) chama confirmCandidate com o payload source/source_id', async () => {
    const { user, espioes } = await renderBaixa({
      fila: [baixaCard],
      preview: previewExterna,
    })
    await user.click(
      await screen.findByRole('button', { name: 'Confirmar candidato' }),
    )

    await waitFor(() =>
      expect(espioes.onConfirmCandidate).toHaveBeenCalledWith('e-baixa', {
        method: 'external',
        source: 'tmdb',
        source_id: '603',
      }),
    )
  })

  it('o botão "Auto-sincronizar" individual não existe mais no card, mas o de lote no topo continua', async () => {
    await renderBaixa({ fila: [baixaCard], preview: previewVazia })

    expect(
      screen.queryByRole('button', { name: /^Auto-sincronizar$/ }),
    ).not.toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: /Auto-sincronizar elegíveis/ }),
    ).toBeInTheDocument()
  })
})

describe('ReviewScreen — auto-match em lote (0050)', () => {
  it('"Auto-sincronizar elegíveis" (lote) mostra o resumo agregado e invalida a fila', async () => {
    const user = userEvent.setup()
    const { espioes, invalidateSpy } = renderReview({
      fila: [baixaCard],
      preview: previewVazia,
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
