import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import { CollectionMembers } from '@/components/collection-members'
import type { EntityCard } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson } from '@/test/detail-harness'

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

function card(over: Partial<EntityCard> = {}): EntityCard {
  return {
    id: 'e1',
    type: 'movie',
    title: 'Duna',
    status: 'cataloged',
    owned: true,
    file_count: 1,
    match_confidence: 100,
    poster_url: null,
    ...over,
  }
}

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
})
