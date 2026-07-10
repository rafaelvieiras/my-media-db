import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen } from '@testing-library/react'
import { CollectionMembers } from '@/components/collection-members'
import { renderWithClient } from '@/test/render'
import { entityCardBase as card, erroJson, okJson } from '@/test/detail-harness'

// Testa a lista de MEMBROS da coleção (0025): cards internos (PosterCard →
// /entities/{id}), estados carregando/erro/vazio e o idioma na busca. Mocka o
// idioma/router (o card usa Link) e roteia GET /entities/{id}/members com um stub
// local (o harness compartilhado só cobre /external e /import).

const estado = vi.hoisted(() => ({ lang: 'pt-BR' }))

// CollectionMembers só depende de `Link` (via PosterCard) e do idioma ativo — não usa
// navigate/params, então mocka só isso (bem mais enxuto que o routerMock completo).
vi.mock('@/lib/language-context', async () => {
  const { fakeLanguage } = await import('@/test/detail-harness')
  return { useLanguage: () => fakeLanguage(estado.lang) }
})
vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock,
}))

// stubMembros roteia só GET /api/v1/entities/{id}/members (o membro é o alvo do teste).
function stubMembros(resp: () => Response) {
  const m = vi.fn(async (url: string) => {
    const u = new URL(String(url), 'http://localhost')
    if (/^\/api\/v1\/entities\/[^/]+\/members$/.test(u.pathname)) {
      return resp()
    }
    throw new Error(`rota nao mockada: ${u.pathname}`)
  })
  vi.stubGlobal('fetch', m)
  return m
}

afterEach(() => {
  vi.restoreAllMocks()
  estado.lang = 'pt-BR'
})

describe('CollectionMembers', () => {
  it('renderiza os membros como cards internos (→ /entities/{id})', async () => {
    stubMembros(() =>
      okJson([
        card({ id: 'e1', title: 'Duna' }),
        card({ id: 'e2', title: 'Duna: Parte 2' }),
      ]),
    )
    renderWithClient(<CollectionMembers id="col-1" />)

    expect(await screen.findByText('Duna')).toBeTruthy()
    expect(screen.getByText('Duna: Parte 2')).toBeTruthy()
    const hrefs = screen.getAllByRole('link').map((l) => l.getAttribute('href'))
    expect(hrefs).toContain('/entities/e1')
    expect(hrefs).toContain('/entities/e2')
  })

  it('coleção sem itens mostra o aviso', async () => {
    stubMembros(() => okJson([]))
    renderWithClient(<CollectionMembers id="col-1" />)
    expect(
      await screen.findByText('Esta coleção ainda não tem itens.'),
    ).toBeTruthy()
  })

  it('erro ao carregar membros mostra o estado de erro', async () => {
    stubMembros(() => erroJson(500, 'internal', 'boom'))
    renderWithClient(<CollectionMembers id="col-1" />)
    expect(
      await screen.findByText('Não foi possível carregar os itens da coleção'),
    ).toBeTruthy()
  })

  it('passa o idioma ativo na busca dos membros (re-busca ao trocar)', async () => {
    estado.lang = 'en'
    const m = stubMembros(() => okJson([card()]))
    renderWithClient(<CollectionMembers id="col-1" />)
    await screen.findByText('Duna')

    const chamada = m.mock.calls.find((c) => String(c[0]).includes('/members'))
    expect(String(chamada?.[0])).toContain('lang=en')
  })

  it('sem editable NÃO mostra o botão de adicionar nem o de remover', async () => {
    stubMembros(() => okJson([card({ id: 'e1', title: 'Duna' })]))
    renderWithClient(<CollectionMembers id="col-1" />)
    await screen.findByText('Duna')
    expect(screen.queryByText('Adicionar item')).toBeNull()
    expect(screen.queryByLabelText('Remover Duna da coleção')).toBeNull()
  })
})

// stubEditavel roteia GET /members, GET /search (picker), POST /members (add) e DELETE
// /members/{child} (remove). Cada rota devolve a Response da função dada.
function stubEditavel(rotas: {
  membros: () => Response
  busca?: () => Response
  add?: () => Response
  remove?: () => Response
}) {
  const m = vi.fn(async (url: string, init?: RequestInit) => {
    const u = new URL(String(url), 'http://localhost')
    const method = init?.method ?? 'GET'
    if (/\/members$/.test(u.pathname) && method === 'GET') {
      return rotas.membros()
    }
    if (u.pathname === '/api/v1/search' && method === 'GET') {
      return (
        rotas.busca ??
        (() => okJson({ local: [], external: [], external_available: true }))
      )()
    }
    if (/\/members$/.test(u.pathname) && method === 'POST') {
      return (rotas.add ?? (() => okJson([])))()
    }
    if (/\/members\/[^/]+$/.test(u.pathname) && method === 'DELETE') {
      return (rotas.remove ?? (() => okJson([])))()
    }
    throw new Error(`rota nao mockada: ${method} ${u.pathname}`)
  })
  vi.stubGlobal('fetch', m)
  return m
}

describe('CollectionMembers editável (0025 Fase B)', () => {
  it('remove um membro em duas etapas e a lista reflete', async () => {
    stubEditavel({
      membros: () =>
        okJson([
          card({ id: 'e1', title: 'Matrix' }),
          card({ id: 'e2', title: 'Matrix Reloaded' }),
        ]),
      remove: () => okJson([card({ id: 'e2', title: 'Matrix Reloaded' })]),
    })
    renderWithClient(<CollectionMembers id="col-1" editable />)
    await screen.findByText('Matrix')

    // Etapa 1: clica no lixo → aparece o "Remover?".
    fireEvent.click(screen.getByLabelText('Remover Matrix da coleção'))
    // Etapa 2: confirma.
    fireEvent.click(screen.getByText('Remover?'))

    await vi.waitFor(() => expect(screen.queryByText('Matrix')).toBeNull())
    expect(screen.getByText('Matrix Reloaded')).toBeTruthy()
  })

  it('adiciona um item pelo picker e a lista reflete', async () => {
    stubEditavel({
      membros: () => okJson([card({ id: 'e1', title: 'Matrix' })]),
      busca: () =>
        okJson({
          local: [card({ id: 'novo', title: 'Matrix Resurrections' })],
          external: [],
          external_available: true,
        }),
      add: () =>
        okJson([
          card({ id: 'e1', title: 'Matrix' }),
          card({ id: 'novo', title: 'Matrix Resurrections' }),
        ]),
    })
    renderWithClient(<CollectionMembers id="col-1" editable />)
    await screen.findByText('Matrix')

    fireEvent.click(screen.getByText('Adicionar item'))
    fireEvent.change(screen.getByLabelText('Buscar item para adicionar'), {
      target: { value: 'matrix' },
    })
    fireEvent.click(await screen.findByText('Matrix Resurrections'))

    // A lista de membros passa a incluir o novo item (grava via setQueryData): o
    // card do grid é um Link para /entities/novo (o botão do picker não é link).
    await vi.waitFor(() => {
      const hrefs = screen
        .getAllByRole('link')
        .map((l) => l.getAttribute('href'))
      expect(hrefs).toContain('/entities/novo')
    })
  })
})
