import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ReviewScreen } from '@/routes/review'
import {
  filterReviewItems,
  groupContext,
  groupReviewItems,
  reviewReason,
  sortReviewGroups,
} from '@/lib/review-filters'
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
// Empty candidate preview ("no external metadata"): the message the card shows. It is
// the starting point of the preview cases and of the "Keep as is" dialog cases.
const SEM_CANDIDATO =
  'no candidate found — neither in the local catalog nor in the external source'

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

// Same base scenario with the default empty preview, waiting for the card to state that
// there is no candidate — the state that enables "Keep as is".
async function renderBaixaSemCandidato() {
  const r = await renderBaixa({ fila: [baixaCard], preview: previewVazia })
  await waitFor(() =>
    expect(screen.getByText(SEM_CANDIDATO)).toBeInTheDocument(),
  )
  return r
}

// "Keep as is" over the empty preview: clicks the action that opens the dialog, which
// lives above the row. The confirm and cancel cases differ only in what they do inside
// the dialog.
async function abrirManterComoEsta() {
  const r = await renderBaixaSemCandidato()
  await r.user.click(
    screen.getByRole('button', { name: 'Keep as is (no external metadata)' }),
  )
  return r
}

// Confirmation of the auto-match (batch or group): the dialog states how many items are
// evaluated, nothing is sent before the "yes", and the action that sends it is the same.
async function confirmarAutoMatch(
  user: ReturnType<typeof userEvent.setup>,
  espioes: Espioes,
  mensagem: string,
) {
  expect(screen.getByText(mensagem)).toBeInTheDocument()
  expect(espioes.onAutoMatchLote).not.toHaveBeenCalled()
  await user.click(screen.getByRole('button', { name: 'Sync candidates' }))
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

  it('"Keep as is" (no candidate in the preview) asks for confirmation, then calls confirmMatch, invalidates and announces the decision (§5: the old "Confirm" behavior)', async () => {
    const { user, espioes, invalidateSpy } = await abrirManterComoEsta()

    // "Keep as is" writes the identity: the dialog states it and the verb is on the action.
    expect(screen.getByText('Keep as is?')).toBeInTheDocument()
    expect(
      screen.getByText(
        'This writes the current identity as final and re-enriches the item. There is no undo.',
      ),
    ).toBeInTheDocument()
    expect(espioes.onConfirm).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: 'Keep as is' }))

    await waitFor(() =>
      expect(espioes.onConfirm).toHaveBeenCalledWith('e-baixa'),
    )
    await esperarChaves(invalidateSpy, CHAVES_REVISAO)
    // The row disappears with the invalidation, so the confirmation lives above it.
    await waitFor(() =>
      expect(
        screen.getByText(
          '"Palpite Fraco" kept as is — the identity was written.',
        ),
      ).toBeInTheDocument(),
    )
  })

  it('cancelling "Keep as is" does not write anything', async () => {
    const { user, espioes } = await abrirManterComoEsta()
    await user.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(espioes.onConfirm).not.toHaveBeenCalled()
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
    expect(screen.getByText('confidence 87.3%')).toBeInTheDocument()
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
    await renderBaixaSemCandidato()

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
      screen.getByRole('button', { name: /Sync eligible candidates/ }),
    ).toBeInTheDocument()
  })
})

describe('ReviewScreen — batch auto-match (0050)', () => {
  it('"Sync eligible candidates" (batch) asks first — with how many items it touches — then shows the aggregated summary and invalidates the queue', async () => {
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

    await user.click(
      screen.getByRole('button', { name: /Sync eligible candidates/ }),
    )

    // One click used to rewrite the identity of the whole queue: now it says how many
    // items are evaluated before doing it.
    expect(
      screen.getByText('Sync the eligible candidates?'),
    ).toBeInTheDocument()
    await confirmarAutoMatch(
      user,
      espioes,
      'This evaluates 1 item of the queue and re-enriches the ones that match.',
    )

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
  it('"Auto-sync this group" confirms with the size of the group, then sends the ids and shows the summary', async () => {
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
    await confirmarAutoMatch(
      user,
      espioes,
      'This evaluates 2 items of this group and re-enriches the ones that match.',
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

describe('ReviewScreen — search, filters and order (Lote B)', () => {
  // Two groups built only to exercise the header of the queue: one album with three
  // items and one folder with a single item.
  const albumGrande: EntityCard = {
    ...baixaCard,
    id: 'e-album-1',
    type: 'faixa',
    title: 'Primeira Faixa',
    review_group: {
      kind: 'album',
      key: 'album:/Musicas/Duran Duran/Notorious',
      label: 'Duran Duran — Notorious',
      size: 3,
    },
  }
  const albumGrande2: EntityCard = {
    ...albumGrande,
    id: 'e-album-2',
    title: 'Segunda Faixa',
  }
  const albumGrande3: EntityCard = {
    ...albumGrande,
    id: 'e-album-3',
    title: 'Terceira Faixa',
  }

  it('orders the sections by group size — the biggest group is the first one', async () => {
    renderReview({ fila: [draftCard, albumGrande, albumGrande2, albumGrande3] })

    const cabecalhos = await screen.findAllByTestId('review-grupo')
    expect(cabecalhos).toHaveLength(2)
    expect(cabecalhos[0]).toHaveTextContent('Duran Duran — Notorious')
    expect(cabecalhos[0]).toHaveTextContent('3 items')
    expect(cabecalhos[1]).toHaveTextContent('Pasta Fita Sem Nome')
  })

  it('searches by title with debounce, ignoring case and accents', async () => {
    const user = userEvent.setup()
    renderReview({ fila: [draftCard, baixaCard] })
    await screen.findByText('Série de Teste')

    // The group label ("Série de Teste") does not carry the title: the match is the row.
    await user.type(
      screen.getByLabelText('Search the queue by title'),
      'PALPITE',
    )

    await waitFor(() =>
      expect(screen.queryByText('Pasta Fita Sem Nome')).not.toBeInTheDocument(),
    )
    expect(screen.getByText('Série de Teste')).toBeInTheDocument()

    // "serie" also finds "Série" — the search normalizes the accents.
    await user.clear(screen.getByLabelText('Search the queue by title'))
    await user.type(screen.getByLabelText('Search the queue by title'), 'serie')
    await waitFor(() =>
      expect(screen.queryByText('Pasta Fita Sem Nome')).not.toBeInTheDocument(),
    )
    expect(screen.getByText('Série de Teste')).toBeInTheDocument()
  })

  it('filters by group kind', async () => {
    const user = userEvent.setup()
    renderReview({ fila: [draftCard, baixaCard] })
    await screen.findByText('Série de Teste')

    await user.selectOptions(
      screen.getByLabelText('Filter by group type'),
      'pasta',
    )

    expect(screen.getByText('Pasta Fita Sem Nome')).toBeInTheDocument()
    expect(screen.queryByText('Série de Teste')).not.toBeInTheDocument()
  })

  it('filters by review reason', async () => {
    const user = userEvent.setup()
    renderReview({ fila: [draftCard, baixaCard] })
    await screen.findByText('Série de Teste')

    await user.selectOptions(
      screen.getByLabelText('Filter by reason'),
      'low_confidence',
    )

    expect(screen.getByText('Série de Teste')).toBeInTheDocument()
    expect(screen.queryByText('Pasta Fita Sem Nome')).not.toBeInTheDocument()
  })

  it('shows how many items the filters kept, with a way to clear them', async () => {
    const user = userEvent.setup()
    renderReview({ fila: [draftCard, baixaCard] })
    await screen.findByText('Série de Teste')

    await user.selectOptions(
      screen.getByLabelText('Filter by group type'),
      'serie',
    )

    expect(screen.getByText(/1 of 2 items/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Clear filters' }))

    expect(screen.queryByText(/1 of 2 items/)).not.toBeInTheDocument()
    expect(screen.getByText('Pasta Fita Sem Nome')).toBeInTheDocument()
    expect(screen.getByText('Série de Teste')).toBeInTheDocument()
  })

  it('nothing matching the filters gives an empty state with "Clear filters"', async () => {
    const user = userEvent.setup()
    renderReview({ fila: [draftCard, baixaCard] })
    await screen.findByText('Série de Teste')

    await user.type(
      screen.getByLabelText('Search the queue by title'),
      'nao existe',
    )

    await waitFor(() =>
      expect(
        screen.getByText('No item matches the filters'),
      ).toBeInTheDocument(),
    )
    expect(
      screen.getByRole('button', { name: 'Clear filters' }),
    ).toBeInTheDocument()
  })

  it('shows the folder under the label, so equal labels are told apart', async () => {
    renderReview({ fila: [draftCard, baixaCard] })
    await screen.findByText('Série de Teste')

    // The card is the only index of the screen: the label repeats across the queue, the
    // path behind it does not.
    expect(screen.getByText('Filmes/Fita Sem Nome')).toBeInTheDocument()
    expect(screen.getByText('Series/Palpite')).toBeInTheDocument()
  })
})

describe('ReviewScreen — pagination inside the open group (Lote B)', () => {
  function filaDe(n: number): EntityCard[] {
    return Array.from({ length: n }, (_, i) => ({
      ...baixaCard,
      id: `e-item-${i}`,
      title: `Item ${i}`,
      review_group: {
        kind: 'serie',
        key: 'serie:/Series/Palpite',
        label: 'Série de Teste',
        size: n,
      },
    }))
  }

  it('renders 50 rows, shows the total and loads the next 50 on "Show more"', async () => {
    const user = userEvent.setup()
    renderReview({ fila: filaDe(55) })
    await abrirGrupo(user, 'Série de Teste', 'Item 0')

    expect(screen.getAllByText(/^Item \d+$/)).toHaveLength(50)
    expect(screen.getByText('Showing 50 of 55')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Show more (5)' }))

    expect(screen.getAllByText(/^Item \d+$/)).toHaveLength(55)
    expect(
      screen.queryByRole('button', { name: /Show more/ }),
    ).not.toBeInTheDocument()
  })

  it('a group smaller than one page shows no "Show more"', async () => {
    const user = userEvent.setup()
    renderReview({ fila: filaDe(3) })
    await abrirGrupo(user, 'Série de Teste', 'Item 0')

    expect(screen.getAllByText(/^Item \d+$/)).toHaveLength(3)
    expect(screen.getByText('Showing 3 of 3')).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: /Show more/ }),
    ).not.toBeInTheDocument()
  })
})

describe('review-filters (pure helpers)', () => {
  const album: EntityCard = {
    ...baixaCard,
    id: 'e-album',
    type: 'faixa',
    title: 'Faixa Um',
    review_group: {
      kind: 'album',
      key: 'album:/Musicas/Artista/Disco',
      label: 'Artista — Disco',
      size: 2,
    },
  }
  const album2: EntityCard = { ...album, id: 'e-album-2', title: 'Faixa Dois' }

  it('classifies the reason from the card status', () => {
    expect(reviewReason(draftCard)).toBe('not_identified')
    expect(reviewReason(baixaCard)).toBe('low_confidence')
    expect(reviewReason(album)).toBe('low_confidence')
  })

  it('sorts the biggest group first and breaks ties by label', () => {
    const grupos = groupReviewItems(
      [draftCard, album, album2, baixaCard],
      'No file',
    )
    const ordenados = sortReviewGroups(grupos, 'en')

    expect(ordenados.map((g) => g.label)).toEqual([
      'Artista — Disco',
      'Pasta Fita Sem Nome',
      'Série de Teste',
    ])
  })

  it('filters by title, kind and reason on the client', () => {
    const cards = [draftCard, baixaCard, album]
    const semFiltro = filterReviewItems(
      cards,
      { query: '', kind: 'all', reason: 'all' },
      'No file',
    )
    expect(semFiltro).toHaveLength(3)

    expect(
      filterReviewItems(
        cards,
        { query: 'faixa', kind: 'all', reason: 'all' },
        'No file',
      ),
    ).toEqual([album])
    expect(
      filterReviewItems(
        cards,
        { query: '', kind: 'serie', reason: 'all' },
        'No file',
      ),
    ).toEqual([baixaCard])
    expect(
      filterReviewItems(
        cards,
        { query: '', kind: 'all', reason: 'not_identified' },
        'No file',
      ),
    ).toEqual([draftCard])
    // The group label/path also answers the search: the series name brings its items.
    expect(
      filterReviewItems(
        cards,
        { query: 'DISC', kind: 'all', reason: 'all' },
        'No file',
      ),
    ).toEqual([album])
  })

  it('hides the path when it does not add context to the label', () => {
    expect(groupContext('Musicas/Artista/Disco', 'Artista — Disco')).toBe(
      'Musicas/Artista/Disco',
    )
    expect(groupContext('Sem arquivo', 'Sem arquivo')).toBe('')
    expect(groupContext('', 'Sem arquivo')).toBe('')
  })
})
