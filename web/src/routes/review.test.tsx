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

// The external result card (`components/external-result-card.tsx`) belongs to another
// domain and is not extracted yet: these two queries accept both labels so this test
// does not break when that extraction lands.
const RESULTADO_USAR = /Usar este|Use this/
const RESULTADO_NA_BIBLIOTECA = /Na biblioteca|In the library/

vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock as typeof LinkMock,
}))

// Fixed language (pt-BR): the correction panel uses useLanguage to pass the active
// language to the search; it isolates from the real LanguageProvider (without touching the settings endpoint).
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
  catalog_completeness: null,
  // Group annotated by the backend: folder of the movie.
  review_group: {
    kind: 'pasta',
    key: 'pasta:/Filmes/Fita Sem Nome',
    label: 'Pasta Fita Sem Nome',
    size: 1,
  },
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
  catalog_completeness: null,
  review_group: {
    kind: 'serie',
    key: 'serie:/Series/Palpite',
    label: 'Série de Teste',
    size: 1,
  },
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

// Preview without a candidate — the backend default when nothing matches in
// any source.
const previewVazia: MatchPreview = {
  found: false,
  method: 'none',
  score: 0,
  meets_auto_threshold: false,
  target: null,
}

// Preview with a LOCAL CATALOG candidate, below the automatic threshold —
// the central case: the owner sees it and decides to apply it anyway.
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

// Preview with an EXTERNAL candidate, matching the automatic threshold.
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
  // Candidate preview: result returned by GET .../match-preview
  // for ANY card of the queue (the tests use a single card at a time).
  preview?: MatchPreview
  previewErro?: boolean
  // Batch auto-match: summary returned by POST /review/auto-match.
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
  onAutoMatchLote: ReturnType<typeof vi.fn<(body: unknown) => void>>
}

// Route handler of the review screen: GET /review (the queue), GET /search (the
// correction), GET .../match-preview (the preview), POST /entities/{id}/confirm,
// POST /entities/{id}/correct, POST /entities/{id}/confirm-candidate and the
// endpoint of the BATCH auto-match, POST /review/auto-match.
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
      espioes.onAutoMatchLote(
        init?.body ? JSON.parse(String(init.body)) : undefined,
      )
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
    onAutoMatchLote: vi.fn<(body: unknown) => void>(),
  }
  vi.stubGlobal('fetch', montarFetch(opts, espioes))
  return { espioes, ...renderComEspioes(<ReviewScreen />) }
}

// abrirGrupo clicks the header of the SECTION and waits for the rows to appear.
// The queue opens closed on purpose: the candidate preview is only fetched when the
// owner opens the ROW ("Show candidate"), so the cases that check the preview/actions
// go through here and through `abrirLinha`.
async function abrirGrupo(
  user: ReturnType<typeof userEvent.setup>,
  rotulo: string,
  tituloLinha: string,
) {
  const cabecalho = await screen.findByRole('button', {
    name: new RegExp(rotulo),
  })
  await user.click(cabecalho)
  await waitFor(() => expect(screen.getByText(tituloLinha)).toBeInTheDocument())
}

// abrirLinha expands the ROW of the item, which is what fires GET match-preview.
async function abrirLinha(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: /Show candidate/ }))
}

// Base scenario of a low-confidence card already rendered (the common setup of the
// confirm/correct cases): it mounts the fetch, renders, opens the section and the row.
// By default the preview finds no candidate (`previewVazia`) — the
// same "no external metadata" behavior as before — unless the scenario
// informs another one.
async function renderBaixa(opts: Opcoes = { fila: [baixaCard] }) {
  const user = userEvent.setup()
  const r = renderReview(opts)
  await abrirGrupo(user, 'Série de Teste', 'Palpite Fraco')
  await abrirLinha(user)
  return { user, ...r }
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('ReviewScreen — review queue (0018)', () => {
  it('shows ONE SECTION per group, with count and reason, closed by default (0137)', async () => {
    renderReview({ fila: [draftCard, baixaCard] })

    await waitFor(() =>
      expect(screen.getByText('Série de Teste')).toBeInTheDocument(),
    )
    expect(screen.getByText('Pasta Fita Sem Nome')).toBeInTheDocument()
    // summary of the header: quantity + dominant reason
    expect(screen.getByText('1 item · not identified')).toBeInTheDocument()
    expect(
      screen.getByText('1 item · low confidence (40%)'),
    ).toBeInTheDocument()
    // the row itself only appears with the section open
    expect(screen.queryByText('Palpite Fraco')).not.toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: /Série de Teste/ }),
    ).toHaveAttribute('aria-expanded', 'false')
  })

  it('does NOT fetch the candidate preview while the queue is closed (no N+1; 0137)', async () => {
    renderReview({ fila: [draftCard, baixaCard] })
    await waitFor(() =>
      expect(screen.getByText('Série de Teste')).toBeInTheDocument(),
    )
    // No match-preview call: the query only exists when the row is expanded.
    const chamadas = (
      globalThis.fetch as unknown as { mock: { calls: Array<[string]> } }
    ).mock.calls
    expect(
      chamadas.filter(([url]) => String(url).includes('/match-preview')),
    ).toHaveLength(0)
  })

  it('opening the section shows the compact rows and the group button', async () => {
    const user = userEvent.setup()
    renderReview({ fila: [draftCard, baixaCard] })
    await abrirGrupo(user, 'Série de Teste', 'Palpite Fraco')

    expect(screen.getByText('Palpite Fraco')).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: /Auto-sync this group/ }),
    ).toBeInTheDocument()
  })

  it('shows the empty state "Nothing to review" (§5: empty states)', async () => {
    renderReview({ fila: [] })
    await waitFor(() =>
      expect(screen.getByText('Nothing to review')).toBeInTheDocument(),
    )
  })

  it('shows the error state', async () => {
    renderReview({ filaErro: true })
    await waitFor(() =>
      expect(screen.getByText('Could not load the review')).toBeInTheDocument(),
    )
  })

  it('"Keep as is" (no candidate in the preview) calls confirmMatch and invalidates review/entities/entity (§5: the old "Confirm" behavior)', async () => {
    const { user, espioes, invalidateSpy } = await renderBaixa({
      fila: [baixaCard],
      preview: previewVazia,
    })
    await waitFor(() =>
      expect(
        screen.getByText(
          'no candidate found — neither in the local catalog nor in the external source',
        ),
      ).toBeInTheDocument(),
    )
    await user.click(
      screen.getByRole('button', {
        name: 'Keep as is (no external metadata)',
      }),
    )

    await waitFor(() =>
      expect(espioes.onConfirm).toHaveBeenCalledWith('e-baixa'),
    )
    await esperarChaves(invalidateSpy, CHAVES_REVISAO)
  })

  it('Correct searches and calls correctMatch with the chosen source_id (§5: correcting writes external_ids + re-enriches)', async () => {
    const { user, espioes, invalidateSpy } = await renderBaixa()
    // It opens the correction panel and searches for the right title.
    await user.click(screen.getByRole('button', { name: /Correct/ }))
    await user.type(
      screen.getByLabelText('Search for the correct title'),
      'certo',
    )
    // It chooses the external result → CORRECTS with its source_id.
    await user.click(
      await screen.findByRole('button', { name: RESULTADO_USAR }),
    )

    await waitFor(() =>
      expect(espioes.onCorrect).toHaveBeenCalledWith('e-baixa', {
        source: 'tmdb',
        source_id: '1234',
      }),
    )
    await esperarChaves(invalidateSpy, CHAVES_REVISAO)
  })

  it('a backend error on the correction shows inline', async () => {
    const { user } = await renderBaixa({
      fila: [baixaCard],
      correctErro: {
        status: 502,
        code: 'upstream_error',
        message: 'A fonte externa falhou.',
      },
    })
    await user.click(screen.getByRole('button', { name: /Correct/ }))
    await user.type(
      screen.getByLabelText('Search for the correct title'),
      'certo',
    )
    await user.click(
      await screen.findByRole('button', { name: RESULTADO_USAR }),
    )

    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent(
        'A fonte externa falhou.',
      ),
    )
  })

  it('the correction result shows the truncated synopsis and, when it already is an entity, the "In the library" badge NEXT TO "Use this" (0074)', async () => {
    // Unlike the Importar flow: internalId here does NOT hide the action — it is
    // precisely the duplicate merge case. The internal_id goes defensively
    // (cast) because the generated type only gains it after the backend regeneration.
    const externoNaBiblioteca = {
      ...externo,
      internal_id: 'ent-9',
    } as ExternalResult
    const { user, espioes } = await renderBaixa({
      fila: [baixaCard],
      search: {
        query: 'certo',
        local: [],
        external: [externoNaBiblioteca],
        external_available: true,
      },
    })
    await user.click(screen.getByRole('button', { name: /Correct/ }))
    await user.type(
      screen.getByLabelText('Search for the correct title'),
      'certo',
    )

    await waitFor(() =>
      expect(screen.getByText('A identidade correta.')).toBeInTheDocument(),
    )
    expect(screen.getByText(RESULTADO_NA_BIBLIOTECA)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: RESULTADO_USAR }))

    await waitFor(() =>
      expect(espioes.onCorrect).toHaveBeenCalledWith('e-baixa', {
        source: 'tmdb',
        source_id: '1234',
      }),
    )
  })
})

describe('ReviewScreen — candidate preview (0060)', () => {
  it('a preview with a local candidate renders poster/title/year/score/source and the "Confirm candidate" button', async () => {
    await renderBaixa({ fila: [baixaCard], preview: previewLocal })

    await waitFor(() => expect(screen.getByText('Duna')).toBeInTheDocument())
    expect(screen.getByAltText('Poster for Duna')).toBeInTheDocument()
    expect(screen.getByText('2021')).toBeInTheDocument()
    expect(screen.getByText('score 87.3')).toBeInTheDocument()
    expect(screen.getByText('local catalog')).toBeInTheDocument()
    expect(screen.getByText('check before confirming')).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Confirm candidate' }),
    ).toBeInTheDocument()
  })

  it('a preview with an external candidate above the threshold shows the "strong match" badge and the "external provider" source', async () => {
    await renderBaixa({ fila: [baixaCard], preview: previewExterna })

    await waitFor(() =>
      expect(screen.getByText('Duna: Parte Dois')).toBeInTheDocument(),
    )
    expect(screen.getByText('external provider')).toBeInTheDocument()
    expect(screen.getByText('strong match')).toBeInTheDocument()
  })

  it('a preview without a candidate renders the empty message and the "Keep as is" button (0060)', async () => {
    await renderBaixa({ fila: [baixaCard], preview: previewVazia })

    await waitFor(() =>
      expect(
        screen.getByText(
          'no candidate found — neither in the local catalog nor in the external source',
        ),
      ).toBeInTheDocument(),
    )
    expect(
      screen.getByRole('button', {
        name: 'Keep as is (no external metadata)',
      }),
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Confirmar candidato' }),
    ).not.toBeInTheDocument()
  })

  it('a failed preview fetch shows a short error and degrades to "Keep as is" without breaking the card', async () => {
    await renderBaixa({ fila: [baixaCard], previewErro: true })

    await waitFor(() =>
      expect(
        screen.getByText('Could not look up a candidate — try again later.'),
      ).toBeInTheDocument(),
    )
    expect(
      screen.getByRole('button', {
        name: 'Keep as is (no external metadata)',
      }),
    ).toBeInTheDocument()
    // "Correct" stays available even with the preview in error.
    expect(screen.getByRole('button', { name: /Correct/ })).toBeInTheDocument()
  })

  it('clicking "Confirm candidate" (local) calls confirmCandidate with the local_id payload and invalidates the queue', async () => {
    const { user, espioes, invalidateSpy } = await renderBaixa({
      fila: [baixaCard],
      preview: previewLocal,
    })
    await user.click(
      await screen.findByRole('button', { name: 'Confirm candidate' }),
    )

    await waitFor(() =>
      expect(espioes.onConfirmCandidate).toHaveBeenCalledWith('e-baixa', {
        method: 'local',
        local_id: '8f2a1c',
      }),
    )
    await esperarChaves(invalidateSpy, CHAVES_REVISAO)
  })

  it('clicking "Confirm candidate" (external) calls confirmCandidate with the source/source_id payload', async () => {
    const { user, espioes } = await renderBaixa({
      fila: [baixaCard],
      preview: previewExterna,
    })
    await user.click(
      await screen.findByRole('button', { name: 'Confirm candidate' }),
    )

    await waitFor(() =>
      expect(espioes.onConfirmCandidate).toHaveBeenCalledWith('e-baixa', {
        method: 'external',
        source: 'tmdb',
        source_id: '603',
      }),
    )
  })

  it('the individual "Auto-sync" button no longer exists on the card, but the batch one on top does', async () => {
    await renderBaixa({ fila: [baixaCard], preview: previewVazia })

    expect(
      screen.queryByRole('button', { name: /^Auto-sync$/ }),
    ).not.toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: /Auto-sync eligible/ }),
    ).toBeInTheDocument()
  })
})

describe('ReviewScreen — batch auto-match (0050)', () => {
  it('"Auto-sync eligible" (batch) shows the aggregated summary and invalidates the queue', async () => {
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
      expect(screen.getByText('Série de Teste')).toBeInTheDocument(),
    )

    await user.click(screen.getByRole('button', { name: /Auto-sync eligible/ }))

    await waitFor(() => expect(espioes.onAutoMatchLote).toHaveBeenCalled())
    await waitFor(() =>
      expect(
        screen.getByText(
          '22 resolved automatically, 8 still need manual review.',
        ),
      ).toBeInTheDocument(),
    )
    await esperarChaves(invalidateSpy, ['["review"]', '["entities"]'])
  })
})

describe('ReviewScreen — group auto-match (0137)', () => {
  it('"Auto-sync this group" sends the ids of the group and shows the summary', async () => {
    const user = userEvent.setup()
    const { espioes } = renderReview({
      fila: [baixaCard, { ...baixaCard, id: 'e-baixa-2', title: 'Outro' }],
      preview: previewVazia,
      autoMatchLoteResultado: {
        resolved_count: 1,
        pending_count: 1,
        items: [{ id: 'e-baixa', resolved: true, method: 'local', score: 96 }],
      },
    })
    await abrirGrupo(user, 'Série de Teste', 'Palpite Fraco')

    await user.click(
      screen.getByRole('button', { name: /Auto-sync this group/ }),
    )

    await waitFor(() =>
      expect(espioes.onAutoMatchLote).toHaveBeenCalledWith({
        ids: ['e-baixa', 'e-baixa-2'],
      }),
    )
    await waitFor(() =>
      expect(
        screen.getByText('1 resolved · 1 still pending'),
      ).toBeInTheDocument(),
    )
  })
})
