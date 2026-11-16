import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen } from '@testing-library/react'
import { CollectionMembers } from '@/components/collection-members'
import { renderWithClient } from '@/test/render'
import {
  entityCardBase as card,
  erroJson,
  okJson,
  stubMembros,
} from '@/test/detail-harness'

// Tests the MEMBER list of the collection: internal cards (PosterCard →
// /entities/{id}), loading/error/empty states and the language in the search. It mocks the
// language/router (the card uses Link) and routes GET /entities/{id}/members with a local
// stub (the shared harness only covers /external and /import).

const estado = vi.hoisted(() => ({ lang: 'pt-BR' }))

// CollectionMembers only depends on `Link` (via PosterCard) and on the active language — it does not use
// navigate/params, so it mocks only that (much leaner than the full routerMock).
vi.mock('@/lib/language-context', async () => {
  const { fakeLanguage } = await import('@/test/detail-harness')
  return { useLanguage: () => fakeLanguage(estado.lang) }
})
vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock,
}))

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

// stubEditavel routes GET /members, GET /search (picker), POST /members (add) and DELETE
// /members/{child} (remove). Each route returns the Response of the given function.
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

    // Step 1: it clicks the trash → "Remover?" appears.
    fireEvent.click(screen.getByLabelText('Remover Matrix da coleção'))
    // Step 2: it confirms.
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

    // The member list now includes the new item (written via setQueryData): the
    // card of the grid is a Link to /entities/novo (the button of the picker is not a link).
    await vi.waitFor(() => {
      const hrefs = screen
        .getAllByRole('link')
        .map((l) => l.getAttribute('href'))
      expect(hrefs).toContain('/entities/novo')
    })
  })
})
