import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { EntityManageMenu } from '@/components/entity-manage-menu'
import type { CatalogCompleteness } from '@/lib/catalog-completeness-badges'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson } from '@/test/detail-harness'

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

  it('nenhum item novo aparece quando catalog_completeness.complete é true', async () => {
    const user = userEvent.setup()
    render({ complete: true, missing: [] })
    await abrirMenu(user)

    expect(
      screen.queryByRole('menuitem', { name: /Baixar informações faltando/ }),
    ).toBeNull()
    expect(
      screen.queryByRole('menuitem', { name: /Baixar arte faltando/ }),
    ).toBeNull()
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
