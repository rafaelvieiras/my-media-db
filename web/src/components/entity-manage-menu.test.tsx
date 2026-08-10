import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { EntityManageMenu } from '@/components/entity-manage-menu'
import type { CatalogCompleteness } from '@/lib/catalog-completeness-badges'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson } from '@/test/detail-harness'
import { stubRota, stubRotas } from '@/test/stub-fetch'

// Testa o dropdown "Gerenciar" (briefing 0086, ex-`EntityActionButtons`/0059):
// Editar (sempre) + "Baixar informações faltando"/"Baixar arte faltando"
// (condicionais a `catalog_completeness`) + "Salvar informações (NFO)"
// (sempre) + "Excluir coleção" (só `isCollection`, com confirmação via
// AlertDialog em vez de mutar direto). O <Link> do router vira um <a> simples
// (mesmo mock de `poster-card.test.tsx`); `useNavigate` é um espião — a
// entity precisa dele pra "Excluir coleção" navegar para `/` no sucesso.
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
  // As 3 rotas abaixo (briefing 0088) são opcionais igual `respostaNfo`/
  // `respostaExcluir` — só os testes que as exercitam passam algo.
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

// stubEntityActionsAdiado instala um fetch cuja resposta do endpoint indicado só
// resolve quando `liberar()` é chamado — necessário para afirmar o rótulo de
// PENDING ("Buscando…"/"Baixando…") de forma determinística: com um mock que
// resolve no mesmo tick, o React já teria trocado para o estado final antes da
// asserção rodar.
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

function render(cc: CatalogCompleteness, owned = true, isCollection = false) {
  return renderWithClient(
    <EntityManageMenu
      id="e1"
      catalogCompleteness={cc}
      owned={owned}
      isCollection={isCollection}
    />,
  )
}

// abrirMenu clica no trigger "Gerenciar" — o `DropdownMenuContent` só monta no
// DOM depois disso (Radix, mesmo comportamento de Sheet/Dialog).
async function abrirMenu(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: /Gerenciar/ }))
}

// expectForcedActionsVisible confirma que os 3 itens sempre-visíveis do
// briefing 0088 estão no menu — chamado pelos 2 testes que provam isso em
// cenários distintos de catalog_completeness (jscpd).
function expectForcedActionsVisible() {
  expect(
    screen.getByRole('menuitem', {
      name: /Verificar Atualizações de Arquivos/,
    }),
  ).toBeInTheDocument()
  expect(
    screen.getByRole('menuitem', { name: /Baixar Metadados/ }),
  ).toBeInTheDocument()
  expect(
    screen.getByRole('menuitem', { name: /Baixar Artes Faltantes/ }),
  ).toBeInTheDocument()
}

// acionarEEsperarErro centraliza o padrão stub+render+abrir+click+esperar
// role=alert repetido pelos 3 testes de "erro de rede/500" das ações
// forçadas (0088) — só o stub, o item clicado e a mensagem mudam (jscpd).
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

// acionarInformacoesFaltando centraliza o padrão stub+render+abrir+click repetido
// pelos testes de desfecho de "Baixar informações faltando" (applied/flagged/
// none/erro) — só a resposta do endpoint muda entre eles (jscpd).
async function acionarInformacoesFaltando(respostaEnriquecer: () => Response) {
  const user = userEvent.setup()
  stubEntityActions(respostaEnriquecer, () =>
    okJson({ downloaded: [], unavailable: [] }),
  )
  render({ complete: false, missing: ['description'] })
  await abrirMenu(user)

  await user.click(
    screen.getByRole('menuitem', { name: /Baixar informações faltando/ }),
  )
}

describe('EntityManageMenu — itens condicionais do dropdown', () => {
  it('"Editar" e "Salvar informações (NFO)" sempre aparecem, mesmo sem catalog_completeness', async () => {
    const user = userEvent.setup()
    render(null)
    await abrirMenu(user)

    expect(screen.getByRole('menuitem', { name: /Editar/ })).toBeInTheDocument()
    expect(
      screen.getByRole('menuitem', { name: /Salvar informações \(NFO\)/ }),
    ).not.toHaveAttribute('aria-disabled', 'true')
    expect(
      screen.queryByRole('menuitem', { name: /Baixar informações faltando/ }),
    ).toBeNull()
    expect(
      screen.queryByRole('menuitem', { name: /Baixar arte faltando/ }),
    ).toBeNull()
  })

  it('as 3 ações forçadas (0088) sempre aparecem, mesmo sem catalog_completeness', async () => {
    const user = userEvent.setup()
    render(null)
    await abrirMenu(user)

    expectForcedActionsVisible()
  })

  it('"Salvar informações (NFO)" fica desabilitado com tooltip quando não há media_file (owned=false)', async () => {
    const user = userEvent.setup()
    render(null, false)
    await abrirMenu(user)

    const item = screen.getByRole('menuitem', {
      name: /Salvar informações \(NFO\)/,
    })
    expect(item).toHaveAttribute('aria-disabled', 'true')
    expect(item).toHaveAttribute(
      'title',
      expect.stringContaining('sem pasta de destino'),
    )
  })

  it('nenhum item condicional aparece quando catalog_completeness.complete é true, mas as 3 ações forçadas (0088) continuam aparecendo', async () => {
    const user = userEvent.setup()
    render({ complete: true, missing: [] })
    await abrirMenu(user)

    expect(
      screen.queryByRole('menuitem', { name: /Baixar informações faltando/ }),
    ).toBeNull()
    expect(
      screen.queryByRole('menuitem', { name: /Baixar arte faltando/ }),
    ).toBeNull()
    expectForcedActionsVisible()
  })

  it('"Baixar informações faltando" aparece quando falta algo não-arte, "Baixar arte faltando" não', async () => {
    const user = userEvent.setup()
    render({ complete: false, missing: ['cast', 'description'] })
    await abrirMenu(user)

    expect(
      screen.getByRole('menuitem', { name: /Baixar informações faltando/ }),
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('menuitem', { name: /Baixar arte faltando/ }),
    ).toBeNull()
  })

  it('"Baixar arte faltando" aparece quando falta ao menos uma chave artwork:', async () => {
    const user = userEvent.setup()
    render({ complete: false, missing: ['cast', 'artwork:poster'] })
    await abrirMenu(user)

    expect(
      screen.getByRole('menuitem', { name: /Baixar informações faltando/ }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('menuitem', { name: /Baixar arte faltando/ }),
    ).toBeInTheDocument()
  })

  it('"Excluir coleção" só aparece quando isCollection é true', async () => {
    const user = userEvent.setup()
    render(null, true, false)
    await abrirMenu(user)
    expect(
      screen.queryByRole('menuitem', { name: /Excluir coleção/ }),
    ).toBeNull()
  })
})

describe('EntityManageMenu — "Baixar informações faltando" (0056 sob demanda)', () => {
  it('mostra "Buscando…" enquanto pendente, depois a mensagem de aplicado', async () => {
    const user = userEvent.setup()
    const liberar = stubEntityActionsAdiado(
      '/api/v1/entities/e1/enrich-missing',
      () => okJson({ outcome: 'applied', method: 'external', score: 96 }),
    )
    render({ complete: false, missing: ['description'] })
    await abrirMenu(user)

    await user.click(
      screen.getByRole('menuitem', { name: /Baixar informações faltando/ }),
    )
    expect(
      await screen.findByText('Buscando…', { exact: false }),
    ).toBeInTheDocument()

    liberar()
    expect(
      await screen.findByText(/Aplicado — enriquecido via provider\./),
    ).toBeInTheDocument()
  })

  it('outcome "applied" via local mostra a mensagem de aplicado (ligado a item já catalogado)', async () => {
    await acionarInformacoesFaltando(() =>
      okJson({ outcome: 'applied', method: 'local', score: 98 }),
    )
    expect(
      await screen.findByText(/Aplicado — ligado a um item já catalogado\./),
    ).toBeInTheDocument()
  })

  it('outcome "flagged" mostra a mensagem com link para /dashboard/review', async () => {
    await acionarInformacoesFaltando(() =>
      okJson({ outcome: 'flagged', score: 61 }),
    )
    expect(
      await screen.findByText(/Não encontramos uma correspondência confiável/),
    ).toBeInTheDocument()
    const link = screen.getByRole('link', { name: 'Revisão' })
    expect(link).toHaveAttribute('href', '/dashboard/review')
  })

  it('outcome "none" mostra mensagem neutra', async () => {
    await acionarInformacoesFaltando(() => okJson({ outcome: 'none' }))
    expect(
      await screen.findByText('Nenhuma correspondência encontrada desta vez.'),
    ).toBeInTheDocument()
  })

  it('erro de rede/500 mostra mensagem role=alert', async () => {
    await acionarInformacoesFaltando(() =>
      erroJson(500, 'db_error', 'falha ao buscar candidato'),
    )
    const alerta = await screen.findByRole('alert')
    expect(alerta).toHaveTextContent('falha ao buscar candidato')
  })
})

describe('EntityManageMenu — "Baixar arte faltando" (0048/0049 sob demanda)', () => {
  it('mostra "Baixando…" enquanto pendente, depois a lista PT-BR (Pôster e fanart baixados.)', async () => {
    const user = userEvent.setup()
    const liberar = stubEntityActionsAdiado(
      '/api/v1/entities/e1/download-missing-artwork',
      () => okJson({ downloaded: ['poster', 'fanart'], unavailable: [] }),
    )
    render({ complete: false, missing: ['artwork:poster', 'artwork:fanart'] })
    await abrirMenu(user)

    await user.click(
      screen.getByRole('menuitem', { name: /Baixar arte faltando/ }),
    )
    expect(
      await screen.findByText('Baixando…', { exact: false }),
    ).toBeInTheDocument()

    liberar()
    expect(
      await screen.findByText('Pôster e fanart baixados.'),
    ).toBeInTheDocument()
  })

  it('nenhuma opção disponível mostra mensagem neutra', async () => {
    const user = userEvent.setup()
    stubEntityActions(
      () => okJson({ outcome: 'none' }),
      () => okJson({ downloaded: [], unavailable: ['clearart'] }),
    )
    render({ complete: false, missing: ['artwork:clearart'] })
    await abrirMenu(user)

    await user.click(
      screen.getByRole('menuitem', { name: /Baixar arte faltando/ }),
    )
    expect(
      await screen.findByText(
        'Nenhuma arte disponível nos provedores para o que falta.',
      ),
    ).toBeInTheDocument()
  })

  it('erro de rede/500 mostra mensagem role=alert', async () => {
    const user = userEvent.setup()
    stubEntityActions(
      () => okJson({ outcome: 'none' }),
      () => erroJson(500, 'db_error', 'falha ao baixar arte'),
    )
    render({ complete: false, missing: ['artwork:poster'] })
    await abrirMenu(user)

    await user.click(
      screen.getByRole('menuitem', { name: /Baixar arte faltando/ }),
    )
    const alerta = await screen.findByRole('alert')
    expect(alerta).toHaveTextContent('falha ao baixar arte')
  })
})

describe('EntityManageMenu — "Verificar Atualizações de Arquivos" (briefing 0088, sempre visível)', () => {
  it('mostra "Verificando…" enquanto pendente, depois o resumo do resultado', async () => {
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
        name: /Verificar Atualizações de Arquivos/,
      }),
    )
    expect(
      await screen.findByText('Verificando…', { exact: false }),
    ).toBeInTheDocument()

    liberar()
    expect(
      await screen.findByText(/2 arquivo\(s\) reconferido\(s\)/),
    ).toBeInTheDocument()
    expect(screen.getByText(/watched, play_count/)).toBeInTheDocument()
  })

  it('desabilita o item enquanto a mutation está pendente', async () => {
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
        name: /Verificar Atualizações de Arquivos/,
      }),
    )
    await abrirMenu(user)
    expect(
      screen.getByRole('menuitem', { name: /Verificando…/ }),
    ).toHaveAttribute('aria-disabled', 'true')
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
      /Verificar Atualizações de Arquivos/,
      'falha ao reconferir arquivos',
    ))
})

describe('EntityManageMenu — "Baixar Metadados" (briefing 0088, sempre visível, force: true)', () => {
  it('envia { force: true } para POST /enrich e mostra "Baixando…"/sucesso', async () => {
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

    await user.click(screen.getByRole('menuitem', { name: /Baixar Metadados/ }))
    expect(
      await screen.findByText('Metadados atualizados.'),
    ).toBeInTheDocument()

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
      /Baixar Metadados/,
      'falha ao baixar metadados',
    ))
})

describe('EntityManageMenu — "Baixar Artes Faltantes" (briefing 0088, sempre visível)', () => {
  it('mostra "Baixando…" enquanto pendente, depois a lista PT-BR dos kinds baixados', async () => {
    const user = userEvent.setup()
    const liberar = stubEntityActionsAdiado(
      '/api/v1/entities/e1/materialize-missing-media',
      () =>
        okJson({ artes_baixadas: ['poster', 'fanart'], nfo_gravado: false }),
    )
    render(null)
    await abrirMenu(user)

    await user.click(
      screen.getByRole('menuitem', { name: /Baixar Artes Faltantes/ }),
    )
    expect(
      await screen.findByText('Baixando…', { exact: false }),
    ).toBeInTheDocument()

    liberar()
    expect(
      await screen.findByText(/Pôster e fanart baixados\./),
    ).toBeInTheDocument()
  })

  it('nada baixado e nfo não gravado mostra mensagem neutra', async () => {
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
      screen.getByRole('menuitem', { name: /Baixar Artes Faltantes/ }),
    )
    expect(
      await screen.findByText('Nenhuma arte nova para materializar.'),
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
      /Baixar Artes Faltantes/,
      'falha ao baixar artes',
    ))
})

describe('EntityManageMenu — "Salvar informações (NFO)" (briefing 0062)', () => {
  it('sucesso: mostra "Gravando…" enquanto pendente, depois o nfo_path gravado', async () => {
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
      screen.getByRole('menuitem', { name: /Salvar informações \(NFO\)/ }),
    )
    expect(
      await screen.findByText('Gravando…', { exact: false }),
    ).toBeInTheDocument()

    liberar()
    expect(
      await screen.findByText(/\/biblioteca\/The Matrix\/movie\.nfo/),
    ).toBeInTheDocument()
  })

  it('404 sem media_file: mensagem de erro clara (role=alert)', async () => {
    const user = userEvent.setup()
    stubEntityActions(
      () => okJson({ outcome: 'none' }),
      () => okJson({ downloaded: [], unavailable: [] }),
      () => erroJson(404, 'not_found', 'entity sem arquivo na biblioteca'),
    )
    render(null)
    await abrirMenu(user)

    await user.click(
      screen.getByRole('menuitem', { name: /Salvar informações \(NFO\)/ }),
    )
    const alerta = await screen.findByRole('alert')
    expect(alerta).toHaveTextContent('entity sem arquivo na biblioteca')
  })
})

// abrirDialogoExclusaoColecao centraliza o padrão stub+render+abrir menu+click
// no item "Excluir coleção"+esperar o AlertDialog, repetido pelos 4 testes de
// desfecho abaixo (só a resposta do DELETE e a asserção final mudam — jscpd).
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
  await user.click(screen.getByRole('menuitem', { name: /Excluir coleção/ }))
  await screen.findByRole('alertdialog', { name: 'Excluir coleção?' })
  return { user, fetchMock }
}

describe('EntityManageMenu — "Excluir coleção" (0026, agora via AlertDialog)', () => {
  it('clicar no item abre o AlertDialog — NÃO apaga direto (sem chamada DELETE)', async () => {
    const { fetchMock } = await abrirDialogoExclusaoColecao()

    expect(
      screen.getByRole('alertdialog', { name: 'Excluir coleção?' }),
    ).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('"Cancelar" fecha o diálogo sem apagar', async () => {
    const { user, fetchMock } = await abrirDialogoExclusaoColecao()

    await user.click(screen.getByRole('button', { name: 'Cancelar' }))

    await waitFor(() => {
      expect(screen.queryByRole('alertdialog')).toBeNull()
    })
    expect(fetchMock).not.toHaveBeenCalled()
    expect(navigateSpy).not.toHaveBeenCalled()
  })

  it('confirmar chama DELETE e navega para / no sucesso', async () => {
    const { user } = await abrirDialogoExclusaoColecao(() => okJson({}))

    await user.click(screen.getByRole('button', { name: 'Excluir coleção' }))

    await waitFor(() => {
      expect(navigateSpy).toHaveBeenCalledWith({ to: '/' })
    })
  })

  it('erro na exclusão mostra role=alert dentro do diálogo, sem navegar', async () => {
    const { user } = await abrirDialogoExclusaoColecao(() =>
      erroJson(409, 'conflict', 'não foi possível excluir a coleção'),
    )

    await user.click(screen.getByRole('button', { name: 'Excluir coleção' }))

    const alerta = await screen.findByRole('alert')
    expect(alerta).toHaveTextContent('não foi possível excluir a coleção')
    expect(navigateSpy).not.toHaveBeenCalled()
  })
})

// "Marcar como assistido"/"Marcar como não assistido" (briefing 0094): os dois
// itens ficam SEMPRE visíveis (não dependem de saber o estado atual — a leitura
// já é feita pelo selo/seção do detalhe, não por este menu). "Assistido" cria o
// evento `completed`; "não assistido" apaga o evento `kind=assistir` MAIS
// RECENTE (busca a lista antes de apagar).
describe('EntityManageMenu — "Marcar como (não) assistido" (0094)', () => {
  it('os dois itens sempre aparecem no menu', async () => {
    const user = userEvent.setup()
    render(null)
    await abrirMenu(user)

    expect(
      screen.getByRole('menuitem', { name: /Marcar como assistido/ }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('menuitem', { name: /Marcar como não assistido/ }),
    ).toBeInTheDocument()
  })

  it('"Marcar como assistido" cria o evento e mostra a mensagem de sucesso', async () => {
    const user = userEvent.setup()
    const fetchMock = stubRota(
      (p, m) => p === '/api/v1/entities/e1/historico' && m === 'POST',
      () => okJson({ id: 'ev1', kind: 'assistir', status: 'completed' }, 201),
    )
    render(null)
    await abrirMenu(user)

    await user.click(
      screen.getByRole('menuitem', { name: /Marcar como assistido/ }),
    )
    expect(
      await screen.findByText('Marcado como assistido.'),
    ).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/entities/e1/historico'),
      expect.objectContaining({ method: 'POST' }),
    )
  })

  it('erro ao "Marcar como assistido" mostra role=alert com a mensagem do envelope', async () => {
    const user = userEvent.setup()
    stubRota(
      (p, m) => p === '/api/v1/entities/e1/historico' && m === 'POST',
      () => erroJson(422, 'invalid', 'status inválido'),
    )
    render(null)
    await abrirMenu(user)

    await user.click(
      screen.getByRole('menuitem', { name: /Marcar como assistido/ }),
    )
    const alerta = await screen.findByRole('alert')
    expect(alerta).toHaveTextContent('status inválido')
  })

  // Comum aos dois testes abaixo: abre o menu, clica "Marcar como não
  // assistido" e confirma a mensagem de sucesso — o que muda é só a rota
  // stubada (com/sem evento `assistir` prévio) e a asserção sobre o DELETE.
  async function clicarMarcarComoNaoAssistido(
    user: ReturnType<typeof userEvent.setup>,
  ) {
    render(null)
    await abrirMenu(user)
    await user.click(
      screen.getByRole('menuitem', { name: /Marcar como não assistido/ }),
    )
    expect(
      await screen.findByText('Marcado como não assistido.'),
    ).toBeInTheDocument()
  }

  it('"Marcar como não assistido" busca o evento mais recente e apaga-o', async () => {
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

  it('"Marcar como não assistido" sem nenhum evento assistir não chama DELETE', async () => {
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
