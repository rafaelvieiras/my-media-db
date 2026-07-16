import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { EntityActionButtons } from '@/components/entity-action-buttons'
import type { CatalogCompleteness } from '@/lib/catalog-completeness-badges'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson } from '@/test/detail-harness'

// Testa o grupo de ações rápidas do detalhe (briefing 0059): Editar (sempre) +
// "Baixar informações faltando"/"Baixar arte faltando" (condicionais a
// `catalog_completeness`). O <Link> do router vira um <a> simples (mesmo mock
// de `poster-card.test.tsx`) — o componente não precisa de contexto de rota.
vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock,
}))

function stubEntityActions(
  respostaEnriquecer: () => Response,
  respostaArte: () => Response,
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
})

function render(cc: CatalogCompleteness) {
  return renderWithClient(
    <EntityActionButtons id="e1" catalogCompleteness={cc} />,
  )
}

// acionarInformacoesFaltando centraliza o padrão stub+render+click repetido pelos
// testes de desfecho de "Baixar informações faltando" (applied/flagged/none/erro) —
// só a resposta do endpoint muda entre eles (jscpd).
function acionarInformacoesFaltando(respostaEnriquecer: () => Response) {
  stubEntityActions(respostaEnriquecer, () =>
    okJson({ downloaded: [], unavailable: [] }),
  )
  render({ complete: false, missing: ['description'] })

  fireEvent.click(
    screen.getByRole('button', { name: /Baixar informações faltando/ }),
  )
}

describe('EntityActionButtons — renderização condicional', () => {
  it('"Editar" sempre aparece, mesmo sem catalog_completeness', () => {
    render(null)
    expect(screen.getByRole('link', { name: /Editar/ })).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: /Baixar informações faltando/ }),
    ).toBeNull()
    expect(
      screen.queryByRole('button', { name: /Baixar arte faltando/ }),
    ).toBeNull()
  })

  it('nenhum botão novo aparece quando catalog_completeness.complete é true', () => {
    render({ complete: true, missing: [] })
    expect(
      screen.queryByRole('button', { name: /Baixar informações faltando/ }),
    ).toBeNull()
    expect(
      screen.queryByRole('button', { name: /Baixar arte faltando/ }),
    ).toBeNull()
  })

  it('"Baixar informações faltando" aparece quando falta algo não-arte, "Baixar arte faltando" não', () => {
    render({ complete: false, missing: ['cast', 'description'] })
    expect(
      screen.getByRole('button', { name: /Baixar informações faltando/ }),
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: /Baixar arte faltando/ }),
    ).toBeNull()
  })

  it('"Baixar arte faltando" aparece quando falta ao menos uma chave artwork:', () => {
    render({ complete: false, missing: ['cast', 'artwork:poster'] })
    expect(
      screen.getByRole('button', { name: /Baixar informações faltando/ }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: /Baixar arte faltando/ }),
    ).toBeInTheDocument()
  })
})

describe('EntityActionButtons — "Baixar informações faltando" (0056 sob demanda)', () => {
  it('mostra "Buscando…" enquanto pendente, depois a mensagem de aplicado', async () => {
    const liberar = stubEntityActionsAdiado(
      '/api/v1/entities/e1/enrich-missing',
      () => okJson({ outcome: 'applied', method: 'external', score: 96 }),
    )
    render({ complete: false, missing: ['description'] })

    fireEvent.click(
      screen.getByRole('button', { name: /Baixar informações faltando/ }),
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
    acionarInformacoesFaltando(() =>
      okJson({ outcome: 'applied', method: 'local', score: 98 }),
    )
    expect(
      await screen.findByText(/Aplicado — ligado a um item já catalogado\./),
    ).toBeInTheDocument()
  })

  it('outcome "flagged" mostra a mensagem com link para /review', async () => {
    acionarInformacoesFaltando(() => okJson({ outcome: 'flagged', score: 61 }))
    expect(
      await screen.findByText(/Não encontramos uma correspondência confiável/),
    ).toBeInTheDocument()
    const link = screen.getByRole('link', { name: 'Revisão' })
    expect(link).toHaveAttribute('href', '/review')
  })

  it('outcome "none" mostra mensagem neutra', async () => {
    acionarInformacoesFaltando(() => okJson({ outcome: 'none' }))
    expect(
      await screen.findByText('Nenhuma correspondência encontrada desta vez.'),
    ).toBeInTheDocument()
  })

  it('erro de rede/500 mostra mensagem role=alert', async () => {
    acionarInformacoesFaltando(() =>
      erroJson(500, 'db_error', 'falha ao buscar candidato'),
    )
    const alerta = await screen.findByRole('alert')
    expect(alerta).toHaveTextContent('falha ao buscar candidato')
  })
})

describe('EntityActionButtons — "Baixar arte faltando" (0048/0049 sob demanda)', () => {
  it('mostra "Baixando…" enquanto pendente, depois a lista PT-BR (Pôster e fanart baixados.)', async () => {
    const liberar = stubEntityActionsAdiado(
      '/api/v1/entities/e1/download-missing-artwork',
      () => okJson({ downloaded: ['poster', 'fanart'], unavailable: [] }),
    )
    render({ complete: false, missing: ['artwork:poster', 'artwork:fanart'] })

    fireEvent.click(
      screen.getByRole('button', { name: /Baixar arte faltando/ }),
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
    stubEntityActions(
      () => okJson({ outcome: 'none' }),
      () => okJson({ downloaded: [], unavailable: ['clearart'] }),
    )
    render({ complete: false, missing: ['artwork:clearart'] })

    fireEvent.click(
      screen.getByRole('button', { name: /Baixar arte faltando/ }),
    )
    expect(
      await screen.findByText(
        'Nenhuma arte disponível nos provedores para o que falta.',
      ),
    ).toBeInTheDocument()
  })

  it('erro de rede/500 mostra mensagem role=alert', async () => {
    stubEntityActions(
      () => okJson({ outcome: 'none' }),
      () => erroJson(500, 'db_error', 'falha ao baixar arte'),
    )
    render({ complete: false, missing: ['artwork:poster'] })

    fireEvent.click(
      screen.getByRole('button', { name: /Baixar arte faltando/ }),
    )
    const alerta = await screen.findByRole('alert')
    expect(alerta).toHaveTextContent('falha ao baixar arte')
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: /Baixar arte faltando/ }),
      ).toBeInTheDocument(),
    )
  })
})
