import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { EntityManageMenu } from '@/components/entity-manage-menu'
import type { CatalogCompleteness } from '@/lib/catalog-completeness-badges'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson } from '@/test/detail-harness'
import { stubRota, stubRotas } from '@/test/stub-fetch'

// Tests the "Gerenciar" dropdown:
// Editar (always) + "Baixar informações faltando"/"Baixar arte faltando"
// (conditional on `catalog_completeness`) + "Salvar informações (NFO)"
// (always) + "Excluir coleção" (only `isCollection`, with confirmation via
// AlertDialog instead of mutating directly). The <Link> of the router becomes a plain <a>
// (same mock as `poster-card.test.tsx`); `useNavigate` is a spy — the
// entity needs it so that "Excluir coleção" navigates to `/` on success.
// The manual match panel passes the active language to the search; same isolation used
// in review.test.tsx (no settings endpoint involved).
vi.mock('@/lib/language-context', () => ({
  useLanguage: () => ({
    lang: 'pt-BR',
    allowed: ['pt-BR'],
    primary: 'pt-BR',
    setLang: () => {},
  }),
}))

const navigateSpy = vi.fn()
vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock,
  useNavigate: () => navigateSpy,
}))

function stubEntityActions(
  respostaEnriquecer: () => Response,
  respostaArte: () => Response,
  respostaNfo?: () => Response,
  respostaExcluir?: () => Response,
  // The 3 routes below are optional like `respostaNfo`/
  // `respostaExcluir` — only the tests that exercise them pass something.
  respostaVerificarArquivos?: () => Response,
  respostaBaixarMetadados?: () => Response,
  respostaArtesFaltantes?: () => Response,
) {
  const m = vi.fn(async (url: string, init?: RequestInit) => {
    const u = new URL(String(url), 'http://localhost')
    const method = init?.method ?? 'GET'
    if (
      u.pathname === '/api/v1/entities/e1/enrich-missing' &&
      method === 'POST'
    ) {
      return respostaEnriquecer()
    }
    if (
      u.pathname === '/api/v1/entities/e1/download-missing-artwork' &&
      method === 'POST'
    ) {
      return respostaArte()
    }
    if (u.pathname === '/api/v1/entities/e1/nfo' && method === 'POST') {
      if (!respostaNfo) {
        throw new Error('rota /nfo chamada sem resposta configurada')
      }
      return respostaNfo()
    }
    if (u.pathname === '/api/v1/entities/e1' && method === 'DELETE') {
      if (!respostaExcluir) {
        throw new Error('rota DELETE chamada sem resposta configurada')
      }
      return respostaExcluir()
    }
    if (
      u.pathname === '/api/v1/entities/e1/refresh-files' &&
      method === 'POST'
    ) {
      if (!respostaVerificarArquivos) {
        throw new Error('rota /refresh-files chamada sem resposta configurada')
      }
      return respostaVerificarArquivos()
    }
    if (u.pathname === '/api/v1/entities/e1/enrich' && method === 'POST') {
      if (!respostaBaixarMetadados) {
        throw new Error('rota /enrich chamada sem resposta configurada')
      }
      return respostaBaixarMetadados()
    }
    if (
      u.pathname === '/api/v1/entities/e1/materialize-missing-media' &&
      method === 'POST'
    ) {
      if (!respostaArtesFaltantes) {
        throw new Error(
          'rota /materialize-missing-media chamada sem resposta configurada',
        )
      }
      return respostaArtesFaltantes()
    }
    throw new Error(`rota nao mockada: ${method} ${u.pathname}`)
  })
  vi.stubGlobal('fetch', m)
  return m
}

// stubEntityActionsAdiado installs a fetch whose response of the given endpoint only
// resolves when `liberar()` is called — needed to assert the PENDING
// label ("Buscando…"/"Baixando…") deterministically: with a mock that
// resolves on the same tick, React would have already swapped to the final state before the
// assertion runs.
function stubEntityActionsAdiado(pathname: string, resposta: () => Response) {
  let liberar: () => void = () => {}
  const pendente = new Promise<void>((resolve) => {
    liberar = resolve
  })
  const m = vi.fn(async (url: string, init?: RequestInit) => {
    const u = new URL(String(url), 'http://localhost')
    const method = init?.method ?? 'GET'
    if (u.pathname === pathname && method === 'POST') {
      await pendente
      return resposta()
    }
    throw new Error(`rota nao mockada: ${method} ${u.pathname}`)
  })
  vi.stubGlobal('fetch', m)
  return liberar
}

afterEach(() => {
  vi.restoreAllMocks()
  navigateSpy.mockClear()
})

// `type`/`status` default to something that NEVER shows "Forçar
// reconciliação" (movie/cataloged) — the tests that are not about this feature
// keep behaving as before, without having to pass the two values.
function render(
  cc: CatalogCompleteness,
  owned = true,
  isCollection = false,
  type = 'movie',
  status = 'cataloged',
  titulo = '',
) {
  return renderWithClient(
    <EntityManageMenu
      id="e1"
      type={type}
      status={status}
      catalogCompleteness={cc}
      owned={owned}
      isCollection={isCollection}
      titulo={titulo}
    />,
  )
}

// abrirMenu clicks the "Gerenciar" trigger — the `DropdownMenuContent` only mounts in the
// DOM after that (Radix, same behavior as Sheet/Dialog).
async function abrirMenu(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: /Manage/ }))
}

// expectForcedActionsVisible confirms that the 3 always-visible items of the
// menu are there — called by the 2 tests that prove it in
// distinct catalog_completeness scenarios (jscpd).
function expectForcedActionsVisible() {
  expect(
    screen.getByRole('menuitem', {
      name: /Check for file updates/,
    }),
  ).toBeInTheDocument()
  expect(
    screen.getByRole('menuitem', { name: /Download metadata/ }),
  ).toBeInTheDocument()
  expect(
    screen.getByRole('menuitem', { name: /Download all missing artwork/ }),
  ).toBeInTheDocument()
}

// acionarEEsperarErro centralizes the stub+render+open+click+wait pattern
// for role=alert repeated by the 3 "network error/500" tests of the forced
// actions — only the stub, the clicked item and the message change (jscpd).
async function acionarEEsperarErro(
  configurarStub: () => void,
  nomeItem: RegExp,
  mensagemEsperada: string,
) {
  const user = userEvent.setup()
  configurarStub()
  render(null)
  await abrirMenu(user)

  await user.click(screen.getByRole('menuitem', { name: nomeItem }))
  const alerta = await screen.findByRole('alert')
  expect(alerta).toHaveTextContent(mensagemEsperada)
}

// acionarInformacoesFaltando centralizes the stub+render+open+click pattern repeated
// by the outcome tests of "Baixar informações faltando" (applied/flagged/
// none/error) — only the response of the endpoint changes between them (jscpd).
async function acionarInformacoesFaltando(respostaEnriquecer: () => Response) {
  const user = userEvent.setup()
  stubEntityActions(respostaEnriquecer, () =>
    okJson({ downloaded: [], unavailable: [] }),
  )
  render({ complete: false, missing: ['description'] })
  await abrirMenu(user)

  await user.click(
    screen.getByRole('menuitem', { name: /Download missing information/ }),
  )
}

describe('EntityManageMenu — conditional dropdown items', () => {
  it('"Edit" and "Save information (NFO)" always appear, even without catalog_completeness', async () => {
    const user = userEvent.setup()
    render(null)
    await abrirMenu(user)

    expect(screen.getByRole('menuitem', { name: /^Edit$/ })).toBeInTheDocument()
    expect(
      screen.getByRole('menuitem', { name: /Save information \(NFO\)/ }),
    ).not.toHaveAttribute('aria-disabled', 'true')
    expect(
      screen.queryByRole('menuitem', { name: /Download missing information/ }),
    ).toBeNull()
    expect(
      screen.queryByRole('menuitem', { name: /Download missing artwork/ }),
    ).toBeNull()
  })

  it('the 3 forced actions (0088) always appear, even without catalog_completeness', async () => {
    const user = userEvent.setup()
    render(null)
    await abrirMenu(user)

    expectForcedActionsVisible()
  })

  it('"Save information (NFO)" stays disabled with a tooltip when there is no media_file (owned=false)', async () => {
    const user = userEvent.setup()
    render(null, false)
    await abrirMenu(user)

    const item = screen.getByRole('menuitem', {
      name: /Save information \(NFO\)/,
    })
    expect(item).toHaveAttribute('aria-disabled', 'true')
    expect(item).toHaveAttribute(
      'title',
      expect.stringContaining('destination folder'),
    )
  })

  it('no conditional item appears when catalog_completeness.complete is true, but the 3 forced actions (0088) keep appearing', async () => {
    const user = userEvent.setup()
    render({ complete: true, missing: [] })
    await abrirMenu(user)

    expect(
      screen.queryByRole('menuitem', { name: /Download missing information/ }),
    ).toBeNull()
    expect(
      screen.queryByRole('menuitem', { name: /Download missing artwork/ }),
    ).toBeNull()
    expectForcedActionsVisible()
  })

  it('"Download missing information" appears when a non-artwork key is missing, "Download missing artwork" does not', async () => {
    const user = userEvent.setup()
    render({ complete: false, missing: ['cast', 'description'] })
    await abrirMenu(user)

    expect(
      screen.getByRole('menuitem', { name: /Download missing information/ }),
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('menuitem', { name: /Download missing artwork/ }),
    ).toBeNull()
  })

  it('"Download missing artwork" appears when at least one artwork: key is missing', async () => {
    const user = userEvent.setup()
    render({ complete: false, missing: ['cast', 'artwork:poster'] })
    await abrirMenu(user)

    expect(
      screen.getByRole('menuitem', { name: /Download missing information/ }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('menuitem', { name: /Download missing artwork/ }),
    ).toBeInTheDocument()
  })

  it('"Delete collection" only appears when isCollection is true', async () => {
    const user = userEvent.setup()
    render(null, true, false)
    await abrirMenu(user)
    expect(
      screen.queryByRole('menuitem', { name: /Delete collection/ }),
    ).toBeNull()
  })
})

describe('EntityManageMenu — manual match (wrong identity)', () => {
  it('"Find and match manually" always appears and opens the search, prefilled with the current title', async () => {
    const user = userEvent.setup()
    render(null, false, false, 'movie', 'cataloged', 'White Collar')
    await abrirMenu(user)
    await user.click(
      screen.getByRole('menuitem', { name: /Find and match manually/ }),
    )
    // The right identity of a wrong match is NOT a draft: the entity is cataloged and
    // never reaches the review queue, so this action of the detail is the only way in.
    const campo = await screen.findByRole('searchbox', {
      name: /Search for the correct title/,
    })
    expect((campo as HTMLInputElement).value).toBe('White Collar')
  })
})

describe('EntityManageMenu — "Download missing information" (0056 on demand)', () => {
  it('shows "Searching…" while pending, then the applied message', async () => {
    const user = userEvent.setup()
    const liberar = stubEntityActionsAdiado(
      '/api/v1/entities/e1/enrich-missing',
      () => okJson({ outcome: 'applied', method: 'external', score: 96 }),
    )
    render({ complete: false, missing: ['description'] })
    await abrirMenu(user)

    await user.click(
      screen.getByRole('menuitem', { name: /Download missing information/ }),
    )
    expect(
      await screen.findByText('Searching…', { exact: false }),
    ).toBeInTheDocument()

    liberar()
    expect(
      await screen.findByText(/Applied — enriched via provider\./),
    ).toBeInTheDocument()
  })

  it('outcome "applied" via local shows the applied message (linked to an item already cataloged)', async () => {
    await acionarInformacoesFaltando(() =>
      okJson({ outcome: 'applied', method: 'local', score: 98 }),
    )
    expect(
      await screen.findByText(
        /Applied — linked to an item already cataloged\./,
      ),
    ).toBeInTheDocument()
  })

  it('outcome "flagged" shows the message with a link to /dashboard/review', async () => {
    await acionarInformacoesFaltando(() =>
      okJson({ outcome: 'flagged', score: 61 }),
    )
    expect(
      await screen.findByText(/We could not find a reliable match/),
    ).toBeInTheDocument()
    const link = screen.getByRole('link', { name: 'Review' })
    expect(link).toHaveAttribute('href', '/dashboard/review')
  })

  it('outcome "none" shows a neutral message', async () => {
    await acionarInformacoesFaltando(() => okJson({ outcome: 'none' }))
    expect(
      await screen.findByText('No match found this time.'),
    ).toBeInTheDocument()
  })

  it('a network/500 error shows a role=alert message', async () => {
    await acionarInformacoesFaltando(() =>
      erroJson(500, 'db_error', 'falha ao buscar candidato'),
    )
    const alerta = await screen.findByRole('alert')
    expect(alerta).toHaveTextContent('falha ao buscar candidato')
  })
})

describe('EntityManageMenu — "Download missing artwork" (0048/0049 on demand)', () => {
  it('shows "Downloading…" while pending, then the list of kinds (Poster and fanart downloaded.)', async () => {
    const user = userEvent.setup()
    const liberar = stubEntityActionsAdiado(
      '/api/v1/entities/e1/download-missing-artwork',
      () => okJson({ downloaded: ['poster', 'fanart'], unavailable: [] }),
    )
    render({ complete: false, missing: ['artwork:poster', 'artwork:fanart'] })
    await abrirMenu(user)

    await user.click(
      screen.getByRole('menuitem', { name: /Download missing artwork/ }),
    )
    expect(
      await screen.findByText('Downloading…', { exact: false }),
    ).toBeInTheDocument()

    liberar()
    expect(
      await screen.findByText('Poster and fanart downloaded.'),
    ).toBeInTheDocument()
  })

  it('no option available shows a neutral message', async () => {
    const user = userEvent.setup()
    stubEntityActions(
      () => okJson({ outcome: 'none' }),
      () => okJson({ downloaded: [], unavailable: ['clearart'] }),
    )
    render({ complete: false, missing: ['artwork:clearart'] })
    await abrirMenu(user)

    await user.click(
      screen.getByRole('menuitem', { name: /Download missing artwork/ }),
    )
    expect(
      await screen.findByText(
        'No artwork available from the providers for what is missing.',
      ),
    ).toBeInTheDocument()
  })

  it('a network/500 error shows a role=alert message', async () => {
    const user = userEvent.setup()
    stubEntityActions(
      () => okJson({ outcome: 'none' }),
      () => erroJson(500, 'db_error', 'falha ao baixar arte'),
    )
    render({ complete: false, missing: ['artwork:poster'] })
    await abrirMenu(user)

    await user.click(
      screen.getByRole('menuitem', { name: /Download missing artwork/ }),
    )
    const alerta = await screen.findByRole('alert')
    expect(alerta).toHaveTextContent('falha ao baixar arte')
  })
})

describe('EntityManageMenu — "Force reconciliation" (briefing 0122)', () => {
  it('appears when type="faixa" and status="draft"', async () => {
    const user = userEvent.setup()
    render(null, true, false, 'faixa', 'draft')
    await abrirMenu(user)

    expect(
      screen.getByRole('menuitem', { name: /Force reconciliation/ }),
    ).toBeInTheDocument()
  })

  it('appears when type="tv" and status="draft"', async () => {
    const user = userEvent.setup()
    render(null, true, false, 'tv', 'draft')
    await abrirMenu(user)

    expect(
      screen.getByRole('menuitem', { name: /Force reconciliation/ }),
    ).toBeInTheDocument()
  })

  it('disappears when the type is not faixa/tv, even in draft', async () => {
    const user = userEvent.setup()
    render(null, true, false, 'movie', 'draft')
    await abrirMenu(user)

    expect(
      screen.queryByRole('menuitem', { name: /Force reconciliation/ }),
    ).toBeNull()
  })

  it('disappears when the status is not draft, even with type faixa/tv', async () => {
    const user = userEvent.setup()
    render(null, true, false, 'faixa', 'cataloged')
    await abrirMenu(user)

    expect(
      screen.queryByRole('menuitem', { name: /Force reconciliation/ }),
    ).toBeNull()
  })

  it('the click calls forceReconcile and shows "Reconciled" when linked=true', async () => {
    const user = userEvent.setup()
    const fetchMock = stubRota(
      (p, m) => p === '/api/v1/entities/e1/force-reconcile' && m === 'POST',
      () => okJson({ linked: true }),
    )
    render(null, true, false, 'faixa', 'draft')
    await abrirMenu(user)

    await user.click(
      screen.getByRole('menuitem', { name: /Force reconciliation/ }),
    )
    expect(
      await screen.findByText('Reconciled — the item left the draft.'),
    ).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/entities/e1/force-reconcile'),
      expect.objectContaining({ method: 'POST' }),
    )
  })

  it('shows "Could not reconcile this time" when linked=false', async () => {
    const user = userEvent.setup()
    stubRota(
      (p, m) => p === '/api/v1/entities/e1/force-reconcile' && m === 'POST',
      () => okJson({ linked: false }),
    )
    render(null, true, false, 'tv', 'draft')
    await abrirMenu(user)

    await user.click(
      screen.getByRole('menuitem', { name: /Force reconciliation/ }),
    )
    expect(
      await screen.findByText(
        'Could not reconcile this time — the item is still a draft.',
      ),
    ).toBeInTheDocument()
  })

  it('shows "Reconciling…" while pending', async () => {
    const user = userEvent.setup()
    const liberar = stubEntityActionsAdiado(
      '/api/v1/entities/e1/force-reconcile',
      () => okJson({ linked: true }),
    )
    render(null, true, false, 'faixa', 'draft')
    await abrirMenu(user)

    await user.click(
      screen.getByRole('menuitem', { name: /Force reconciliation/ }),
    )
    expect(
      await screen.findByText('Reconciling…', { exact: false }),
    ).toBeInTheDocument()

    liberar()
    expect(
      await screen.findByText('Reconciled — the item left the draft.'),
    ).toBeInTheDocument()
  })

  it('a network/500 error shows a role=alert message', async () => {
    const user = userEvent.setup()
    stubRota(
      (p, m) => p === '/api/v1/entities/e1/force-reconcile' && m === 'POST',
      () => erroJson(500, 'db_error', 'falha ao forçar reconciliação'),
    )
    render(null, true, false, 'faixa', 'draft')
    await abrirMenu(user)

    await user.click(
      screen.getByRole('menuitem', { name: /Force reconciliation/ }),
    )
    const alerta = await screen.findByRole('alert')
    expect(alerta).toHaveTextContent('falha ao forçar reconciliação')
  })
})

describe('EntityManageMenu — "Check for file updates" (briefing 0088, always visible)', () => {
  it('shows "Checking…" while pending, then the result summary', async () => {
    const user = userEvent.setup()
    const liberar = stubEntityActionsAdiado(
      '/api/v1/entities/e1/refresh-files',
      () =>
        okJson({
          files_rechecked: 2,
          assets_found: 1,
          playback_fields_filled: ['watched', 'play_count'],
        }),
    )
    render(null)
    await abrirMenu(user)

    await user.click(
      screen.getByRole('menuitem', {
        name: /Check for file updates/,
      }),
    )
    expect(
      await screen.findByText('Checking…', { exact: false }),
    ).toBeInTheDocument()

    liberar()
    expect(await screen.findByText(/2 file\(s\) rechecked/)).toBeInTheDocument()
    expect(screen.getByText(/watched, play_count/)).toBeInTheDocument()
  })

  it('disables the item while the mutation is pending', async () => {
    const user = userEvent.setup()
    stubEntityActionsAdiado('/api/v1/entities/e1/refresh-files', () =>
      okJson({
        files_rechecked: 0,
        assets_found: 0,
        playback_fields_filled: [],
      }),
    )
    render(null)
    await abrirMenu(user)

    await user.click(
      screen.getByRole('menuitem', {
        name: /Check for file updates/,
      }),
    )
    await abrirMenu(user)
    expect(screen.getByRole('menuitem', { name: /Checking…/ })).toHaveAttribute(
      'aria-disabled',
      'true',
    )
  })

  it('erro de rede/500 mostra mensagem role=alert', () =>
    acionarEEsperarErro(
      () =>
        stubEntityActions(
          () => okJson({ outcome: 'none' }),
          () => okJson({ downloaded: [], unavailable: [] }),
          undefined,
          undefined,
          () => erroJson(500, 'db_error', 'falha ao reconferir arquivos'),
        ),
      /Check for file updates/,
      'falha ao reconferir arquivos',
    ))
})

describe('EntityManageMenu — "Download metadata" (briefing 0088, always visible, force: true)', () => {
  it('sends { force: true } to POST /enrich and shows "Downloading…"/success', async () => {
    const user = userEvent.setup()
    const fetchMock = stubEntityActions(
      () => okJson({ outcome: 'none' }),
      () => okJson({ downloaded: [], unavailable: [] }),
      undefined,
      undefined,
      undefined,
      () => okJson({ id: 'e1', type: 'movie', title: 'Duna' }),
    )
    render(null)
    await abrirMenu(user)

    await user.click(
      screen.getByRole('menuitem', { name: /Download metadata/ }),
    )
    expect(await screen.findByText('Metadata updated.')).toBeInTheDocument()

    const chamada = fetchMock.mock.calls.find(
      ([url]) =>
        new URL(String(url), 'http://localhost').pathname ===
        '/api/v1/entities/e1/enrich',
    )
    expect(chamada).toBeDefined()
    const init = chamada?.[1] as RequestInit
    expect(JSON.parse(String(init.body))).toEqual({ force: true })
  })

  it('erro de rede/500 mostra mensagem role=alert', () =>
    acionarEEsperarErro(
      () =>
        stubEntityActions(
          () => okJson({ outcome: 'none' }),
          () => okJson({ downloaded: [], unavailable: [] }),
          undefined,
          undefined,
          undefined,
          () => erroJson(500, 'db_error', 'falha ao baixar metadados'),
        ),
      /Download metadata/,
      'falha ao baixar metadados',
    ))
})

describe('EntityManageMenu — "Download all missing artwork" (briefing 0088, always visible)', () => {
  it('shows "Downloading…" while pending, then the list of downloaded kinds', async () => {
    const user = userEvent.setup()
    const liberar = stubEntityActionsAdiado(
      '/api/v1/entities/e1/materialize-missing-media',
      () =>
        okJson({ artes_baixadas: ['poster', 'fanart'], nfo_gravado: false }),
    )
    render(null)
    await abrirMenu(user)

    await user.click(
      screen.getByRole('menuitem', { name: /Download all missing artwork/ }),
    )
    expect(
      await screen.findByText('Downloading…', { exact: false }),
    ).toBeInTheDocument()

    liberar()
    expect(
      await screen.findByText(/Poster and fanart downloaded\./),
    ).toBeInTheDocument()
  })

  it('nothing downloaded and nfo not written shows a neutral message', async () => {
    const user = userEvent.setup()
    stubEntityActions(
      () => okJson({ outcome: 'none' }),
      () => okJson({ downloaded: [], unavailable: [] }),
      undefined,
      undefined,
      undefined,
      undefined,
      () => okJson({ artes_baixadas: [], nfo_gravado: false }),
    )
    render(null)
    await abrirMenu(user)

    await user.click(
      screen.getByRole('menuitem', { name: /Download all missing artwork/ }),
    )
    expect(
      await screen.findByText('No new artwork to materialize.'),
    ).toBeInTheDocument()
  })

  it('erro de rede/500 mostra mensagem role=alert', () =>
    acionarEEsperarErro(
      () =>
        stubEntityActions(
          () => okJson({ outcome: 'none' }),
          () => okJson({ downloaded: [], unavailable: [] }),
          undefined,
          undefined,
          undefined,
          undefined,
          () => erroJson(500, 'db_error', 'falha ao baixar artes'),
        ),
      /Download all missing artwork/,
      'falha ao baixar artes',
    ))
})

describe('EntityManageMenu — "Save information (NFO)" (briefing 0062)', () => {
  it('success: shows "Writing…" while pending, then the written nfo_path', async () => {
    const user = userEvent.setup()
    const liberar = stubEntityActionsAdiado('/api/v1/entities/e1/nfo', () =>
      okJson({
        nfo_path: '/biblioteca/The Matrix/movie.nfo',
        nfo_written_at: '2026-07-16T12:00:00Z',
      }),
    )
    render(null)
    await abrirMenu(user)

    await user.click(
      screen.getByRole('menuitem', { name: /Save information \(NFO\)/ }),
    )
    expect(
      await screen.findByText('Writing…', { exact: false }),
    ).toBeInTheDocument()

    liberar()
    expect(
      await screen.findByText(/\/biblioteca\/The Matrix\/movie\.nfo/),
    ).toBeInTheDocument()
  })

  it('404 without media_file: a clear error message (role=alert)', async () => {
    const user = userEvent.setup()
    stubEntityActions(
      () => okJson({ outcome: 'none' }),
      () => okJson({ downloaded: [], unavailable: [] }),
      () => erroJson(404, 'not_found', 'entity sem arquivo na biblioteca'),
    )
    render(null)
    await abrirMenu(user)

    await user.click(
      screen.getByRole('menuitem', { name: /Save information \(NFO\)/ }),
    )
    const alerta = await screen.findByRole('alert')
    expect(alerta).toHaveTextContent('entity sem arquivo na biblioteca')
  })
})

// abrirDialogoExclusaoColecao centralizes the stub+render+open menu+click
// on the item "Excluir coleção"+wait for the AlertDialog, repeated by the 4 outcome
// tests below (only the DELETE response and the final assertion change — jscpd).
async function abrirDialogoExclusaoColecao(respostaExcluir?: () => Response) {
  const user = userEvent.setup()
  const fetchMock = stubEntityActions(
    () => okJson({ outcome: 'none' }),
    () => okJson({ downloaded: [], unavailable: [] }),
    undefined,
    respostaExcluir,
  )
  render(null, true, true)
  await abrirMenu(user)
  await user.click(screen.getByRole('menuitem', { name: /Delete collection/ }))
  await screen.findByRole('alertdialog', { name: 'Delete the collection?' })
  return { user, fetchMock }
}

describe('EntityManageMenu — "Delete collection" (0026, now via AlertDialog)', () => {
  it('clicking the item opens the AlertDialog — it does NOT delete right away (no DELETE call)', async () => {
    const { fetchMock } = await abrirDialogoExclusaoColecao()

    expect(
      screen.getByRole('alertdialog', { name: 'Delete the collection?' }),
    ).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('"Cancel" closes the dialog without deleting', async () => {
    const { user, fetchMock } = await abrirDialogoExclusaoColecao()

    await user.click(screen.getByRole('button', { name: 'Cancel' }))

    await waitFor(() => {
      expect(screen.queryByRole('alertdialog')).toBeNull()
    })
    expect(fetchMock).not.toHaveBeenCalled()
    expect(navigateSpy).not.toHaveBeenCalled()
  })

  it('confirming calls DELETE and navigates to / on success', async () => {
    const { user } = await abrirDialogoExclusaoColecao(() => okJson({}))

    await user.click(screen.getByRole('button', { name: 'Delete collection' }))

    await waitFor(() => {
      expect(navigateSpy).toHaveBeenCalledWith({ to: '/' })
    })
  })

  it('a delete error shows role=alert inside the dialog, without navigating', async () => {
    const { user } = await abrirDialogoExclusaoColecao(() =>
      erroJson(409, 'conflict', 'não foi possível excluir a coleção'),
    )

    await user.click(screen.getByRole('button', { name: 'Delete collection' }))

    const alerta = await screen.findByRole('alert')
    expect(alerta).toHaveTextContent('não foi possível excluir a coleção')
    expect(navigateSpy).not.toHaveBeenCalled()
  })
})

// "Marcar como assistido"/"Marcar como não assistido": the two
// items stay ALWAYS visible (they do not depend on knowing the current state — the reading
// is already done by the badge/section of the detail, not by this menu). "Assistido" creates the
// `completed` event; "não assistido" deletes the MOST
// RECENT `kind=assistir` event (it fetches the list before deleting).
describe('EntityManageMenu — "Mark as (not) watched" (0094)', () => {
  it('both items always appear in the menu', async () => {
    const user = userEvent.setup()
    render(null)
    await abrirMenu(user)

    expect(
      screen.getByRole('menuitem', { name: /Mark as watched/ }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('menuitem', { name: /Mark as not watched/ }),
    ).toBeInTheDocument()
  })

  it('"Mark as watched" creates the event and shows the success message', async () => {
    const user = userEvent.setup()
    const fetchMock = stubRota(
      (p, m) => p === '/api/v1/entities/e1/historico' && m === 'POST',
      () => okJson({ id: 'ev1', kind: 'assistir', status: 'completed' }, 201),
    )
    render(null)
    await abrirMenu(user)

    await user.click(screen.getByRole('menuitem', { name: /Mark as watched/ }))
    expect(await screen.findByText('Marked as watched.')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/entities/e1/historico'),
      expect.objectContaining({ method: 'POST' }),
    )
  })

  it('an error on "Mark as watched" shows role=alert with the envelope message', async () => {
    const user = userEvent.setup()
    stubRota(
      (p, m) => p === '/api/v1/entities/e1/historico' && m === 'POST',
      () => erroJson(422, 'invalid', 'status inválido'),
    )
    render(null)
    await abrirMenu(user)

    await user.click(screen.getByRole('menuitem', { name: /Mark as watched/ }))
    const alerta = await screen.findByRole('alert')
    expect(alerta).toHaveTextContent('status inválido')
  })

  // Common to the two tests below: it opens the menu, clicks "Marcar como não
  // assistido" and confirms the success message — what changes is only the stubbed
  // route (with/without a previous `assistir` event) and the assertion on the DELETE.
  async function clicarMarcarComoNaoAssistido(
    user: ReturnType<typeof userEvent.setup>,
  ) {
    render(null)
    await abrirMenu(user)
    await user.click(
      screen.getByRole('menuitem', { name: /Mark as not watched/ }),
    )
    expect(
      await screen.findByText('Marked as not watched.'),
    ).toBeInTheDocument()
  }

  it('"Mark as not watched" fetches the most recent event and deletes it', async () => {
    const user = userEvent.setup()
    const fetchMock = stubRotas([
      [
        (p, m) => p === '/api/v1/entities/e1/historico' && m === 'GET',
        () =>
          okJson({
            eventos: [
              {
                id: 'ev-recente',
                kind: 'assistir',
                status: 'completed',
                occurred_at: '2026-01-05T20:00:00Z',
                source: 'jellyfin-webhook',
                created_at: '2026-01-05T20:00:00Z',
              },
            ],
          }),
      ],
      [
        (p, m) =>
          p === '/api/v1/entities/e1/historico/ev-recente' && m === 'DELETE',
        () => ({ ok: true, status: 204, json: async () => ({}) }) as Response,
      ],
    ])

    await clicarMarcarComoNaoAssistido(user)

    expect(
      fetchMock.mock.calls.some(
        ([url, init]) =>
          String(url) === '/api/v1/entities/e1/historico/ev-recente' &&
          init?.method === 'DELETE',
      ),
    ).toBe(true)
  })

  it('"Mark as not watched" without any assistir event does not call DELETE', async () => {
    const user = userEvent.setup()
    const fetchMock = stubRota(
      (p, m) => p === '/api/v1/entities/e1/historico' && m === 'GET',
      () => okJson({ eventos: [] }),
    )

    await clicarMarcarComoNaoAssistido(user)

    expect(
      fetchMock.mock.calls.some(([, init]) => init?.method === 'DELETE'),
    ).toBe(false)
  })
})

// "Write metadata to the file": the item is visible only for
// `type==='faixa'`/`type==='album'` (it does NOT depend on `owned`/a real folder
// — that is about the NFO). A destructive/irreversible action on the tag of the
// file: it requires confirmation via AlertDialog before firing
// `writeAudioTags`, the same 2-step pattern as "Excluir coleção".
describe('EntityManageMenu — "Write metadata to the file" (briefing 0125)', () => {
  it('appears for type="faixa"', async () => {
    const user = userEvent.setup()
    render(null, true, false, 'faixa', 'cataloged')
    await abrirMenu(user)
    expect(
      screen.getByRole('menuitem', { name: /Write metadata to the file/ }),
    ).toBeInTheDocument()
  })

  it('aparece para type="album"', async () => {
    const user = userEvent.setup()
    render(null, true, false, 'album', 'cataloged')
    await abrirMenu(user)
    expect(
      screen.getByRole('menuitem', { name: /Write metadata to the file/ }),
    ).toBeInTheDocument()
  })

  it('does not appear for type="movie"', async () => {
    const user = userEvent.setup()
    render(null, true, false, 'movie', 'cataloged')
    await abrirMenu(user)
    expect(
      screen.queryByRole('menuitem', { name: /Write metadata to the file/ }),
    ).toBeNull()
  })

  it('does not appear for type="artista"', async () => {
    const user = userEvent.setup()
    render(null, true, false, 'artista', 'cataloged')
    await abrirMenu(user)
    expect(
      screen.queryByRole('menuitem', { name: /Write metadata to the file/ }),
    ).toBeNull()
  })

  it('clicking the item opens the confirmation AlertDialog — it does NOT fire the mutation right away', async () => {
    const user = userEvent.setup()
    const fetchMock = stubRota(
      (p, m) => p === '/api/v1/entities/e1/audio-tags/write' && m === 'POST',
      () => okJson({ faixas_regravadas: 1, falhas: [] }),
    )
    render(null, true, false, 'faixa', 'cataloged')
    await abrirMenu(user)

    await user.click(
      screen.getByRole('menuitem', { name: /Write metadata to the file/ }),
    )

    expect(
      await screen.findByRole('alertdialog', {
        name: 'Write metadata to the file?',
      }),
    ).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('"Cancel" closes the dialog without calling the API', async () => {
    const user = userEvent.setup()
    const fetchMock = stubRota(
      (p, m) => p === '/api/v1/entities/e1/audio-tags/write' && m === 'POST',
      () => okJson({ faixas_regravadas: 1, falhas: [] }),
    )
    render(null, true, false, 'faixa', 'cataloged')
    await abrirMenu(user)
    await user.click(
      screen.getByRole('menuitem', { name: /Write metadata to the file/ }),
    )
    await screen.findByRole('alertdialog')

    await user.click(screen.getByRole('button', { name: 'Cancel' }))

    await waitFor(() => {
      expect(screen.queryByRole('alertdialog')).toBeNull()
    })
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('confirming calls writeAudioTags and shows "Writing…" followed by the success summary', async () => {
    const user = userEvent.setup()
    const liberar = stubEntityActionsAdiado(
      '/api/v1/entities/e1/audio-tags/write',
      () => okJson({ faixas_regravadas: 12, falhas: [] }),
    )
    render(null, true, false, 'album', 'cataloged')
    await abrirMenu(user)
    await user.click(
      screen.getByRole('menuitem', { name: /Write metadata to the file/ }),
    )
    await screen.findByRole('alertdialog')

    await user.click(screen.getByRole('button', { name: 'Write metadata' }))
    // While the AlertDialog is open, the rest of the tree (where the inline
    // "Gravando…" echo lives) stays `aria-hidden` (Radix traps focus/AT) — the
    // pending assertion is about the dialog button ITSELF, not the echo.
    expect(
      await screen.findByRole('button', { name: 'Writing…' }),
    ).toBeDisabled()

    liberar()
    expect(
      await screen.findByText('12 track(s) rewritten.'),
    ).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.queryByRole('alertdialog')).toBeNull()
    })
  })

  it('a summary with partial failures shows the error count together with the success', async () => {
    const user = userEvent.setup()
    stubRota(
      (p, m) => p === '/api/v1/entities/e1/audio-tags/write' && m === 'POST',
      () =>
        okJson({
          faixas_regravadas: 10,
          falhas: ['faixaX: arquivo ausente', 'faixaY: tag ilegível'],
        }),
    )
    render(null, true, false, 'album', 'cataloged')
    await abrirMenu(user)
    await user.click(
      screen.getByRole('menuitem', { name: /Write metadata to the file/ }),
    )
    await screen.findByRole('alertdialog')
    await user.click(screen.getByRole('button', { name: 'Write metadata' }))

    expect(
      await screen.findByText('10 track(s) rewritten — 2 with errors.'),
    ).toBeInTheDocument()
  })

  it('a network/500 error shows role=alert INSIDE the dialog, which stays open', async () => {
    const user = userEvent.setup()
    stubRota(
      (p, m) => p === '/api/v1/entities/e1/audio-tags/write' && m === 'POST',
      () => erroJson(503, 'unavailable', 'ffmpeg indisponível'),
    )
    render(null, true, false, 'faixa', 'cataloged')
    await abrirMenu(user)
    await user.click(
      screen.getByRole('menuitem', { name: /Write metadata to the file/ }),
    )
    await screen.findByRole('alertdialog')
    await user.click(screen.getByRole('button', { name: 'Write metadata' }))

    const alerta = await screen.findByRole('alert')
    expect(alerta).toHaveTextContent('ffmpeg indisponível')
    expect(screen.getByRole('alertdialog')).toBeInTheDocument()
  })
})
