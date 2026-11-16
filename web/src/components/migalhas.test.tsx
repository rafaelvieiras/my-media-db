import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { Migalhas } from '@/components/migalhas'
import type { AncestorRef } from '@/lib/api'

// The link interpolates the params into the href, to assert the destination of each ancestor.
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

    // Only the ANCESTORS are links, in root→parent order (series before season).
    const links = screen.getAllByRole('link')
    expect(links.map((l) => l.textContent)).toEqual([
      'Os Simpsons',
      'Temporada 1',
    ])
    expect(links[0].getAttribute('href')).toBe('/entities/ent-serie')
    expect(links[1].getAttribute('href')).toBe('/entities/ent-temp')

    // The CURRENT item appears as the current state (aria-current), without a link.
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
