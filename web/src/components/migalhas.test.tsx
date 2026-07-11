import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { Migalhas } from '@/components/migalhas'
import type { AncestorRef } from '@/lib/api'

// Link interpola os params no href, para afirmar o destino de cada ancestral.
vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock,
}))

const SERIE: AncestorRef = { id: 'ent-serie', type: 'tv', title: 'Os Simpsons' }
const TEMPORADA: AncestorRef = {
  id: 'ent-temp',
  type: 'season',
  title: 'Temporada 1',
}

describe('Migalhas (breadcrumb ascendente 0032)', () => {
  it('num episódio: os ancestrais viram links raiz→pai e o atual fecha sem link', () => {
    render(
      <Migalhas
        ancestors={[SERIE, TEMPORADA]}
        atual={{ title: 'Episódio 1', type: 'episode' }}
      />,
    )

    // Só os ANCESTRAIS são links, na ordem raiz→pai (série antes da temporada).
    const links = screen.getAllByRole('link')
    expect(links.map((l) => l.textContent)).toEqual([
      'Os Simpsons',
      'Temporada 1',
    ])
    expect(links[0].getAttribute('href')).toBe('/entities/ent-serie')
    expect(links[1].getAttribute('href')).toBe('/entities/ent-temp')

    // O item ATUAL aparece como estado corrente (aria-current), sem link.
    const atual = screen.getByText('Episódio 1')
    expect(atual.getAttribute('aria-current')).toBe('page')
    expect(screen.queryByRole('link', { name: 'Episódio 1' })).toBeNull()
  })

  it('numa temporada: um único ancestral (a série) clicável', () => {
    render(
      <Migalhas
        ancestors={[SERIE]}
        atual={{ title: 'Temporada 1', type: 'season' }}
      />,
    )
    const link = screen.getByRole('link', { name: 'Os Simpsons' })
    expect(link.getAttribute('href')).toBe('/entities/ent-serie')
    expect(screen.getByText('Temporada 1').getAttribute('aria-current')).toBe(
      'page',
    )
  })

  it('sem ancestrais (obra de topo): não renderiza breadcrumb', () => {
    const { container } = render(
      <Migalhas ancestors={[]} atual={{ title: 'Duna', type: 'movie' }} />,
    )
    expect(container.querySelector('nav')).toBeNull()
    expect(screen.queryByText('Duna')).toBeNull()
  })
})
