import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen } from '@testing-library/react'
import { CollectionMemberPicker } from '@/components/collection-member-picker'
import type { EntityCard } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { entityCardBase as card, okJson } from '@/test/detail-harness'

// Tests the member picker: it searches the local catalog, EXCLUDES the collection
// itself / already linked members / other collections, and adds via POST returning the
// updated list to the parent. The picker does not use the router (no Link) — only the language is mocked.

const estado = vi.hoisted(() => ({ lang: 'pt-BR' }))

vi.mock('@/lib/language-context', async () => {
  const { fakeLanguage } = await import('@/test/detail-harness')
  return { useLanguage: () => fakeLanguage(estado.lang) }
})

// stubPicker routes GET /search (local results) and POST /entities/{id}/members.
function stubPicker(locais: EntityCard[], aoAdicionar: () => Response) {
  const m = vi.fn(async (url: string, init?: RequestInit) => {
    const u = new URL(String(url), 'http://localhost')
    const method = init?.method ?? 'GET'
    if (u.pathname === '/api/v1/search' && method === 'GET') {
      return okJson({ local: locais, external: [], external_available: true })
    }
    if (
      /^\/api\/v1\/entities\/[^/]+\/members$/.test(u.pathname) &&
      method === 'POST'
    ) {
      return aoAdicionar()
    }
    throw new Error(`rota nao mockada: ${method} ${u.pathname}`)
  })
  vi.stubGlobal('fetch', m)
  return m
}

afterEach(() => {
  vi.restoreAllMocks()
  estado.lang = 'pt-BR'
})

describe('CollectionMemberPicker', () => {
  it('excludes the collection itself, already linked members and other collections', async () => {
    stubPicker(
      [
        card({ id: 'col-1', title: 'A própria coleção', type: 'collection' }),
        card({ id: 'e1', title: 'Já é membro' }),
        card({ id: 'c9', title: 'Outra coleção', type: 'collection' }),
        card({ id: 'ok', title: 'Filme disponível' }),
      ],
      () => okJson([]),
    )
    renderWithClient(
      <CollectionMemberPicker
        collectionId="col-1"
        excludeIds={new Set(['e1'])}
        onAdded={() => {}}
        onClose={() => {}}
      />,
    )
    fireEvent.change(screen.getByLabelText('Search for an item to add'), {
      target: { value: 'a' },
    })

    expect(await screen.findByText('Filme disponível')).toBeTruthy()
    expect(screen.queryByText('A própria coleção')).toBeNull()
    expect(screen.queryByText('Já é membro')).toBeNull()
    expect(screen.queryByText('Outra coleção')).toBeNull()
  })

  it('adds an item and hands the updated list to the parent', async () => {
    const onAdded = vi.fn()
    stubPicker([card({ id: 'ok', title: 'Filme disponível' })], () =>
      okJson([card({ id: 'ok', title: 'Filme disponível' })]),
    )
    renderWithClient(
      <CollectionMemberPicker
        collectionId="col-1"
        excludeIds={new Set()}
        onAdded={onAdded}
        onClose={() => {}}
      />,
    )
    fireEvent.change(screen.getByLabelText('Search for an item to add'), {
      target: { value: 'a' },
    })
    fireEvent.click(await screen.findByText('Filme disponível'))

    await vi.waitFor(() => expect(onAdded).toHaveBeenCalledTimes(1))
    expect(onAdded.mock.calls[0][0]).toHaveLength(1)
  })
})
