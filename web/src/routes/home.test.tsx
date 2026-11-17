import { describe, expect, it } from 'vitest'
import { screen } from '@testing-library/react'
import { renderRota } from '@/test/route-tree-harness'

// The catalog home: the two creation shortcuts plus the "recently added"
// carousels (one per top-level type). The route is mounted through the REAL route
// tree, so this also proves the router wiring and the header render in English.
describe('home (/)', () => {
  it('renders the title, the subtitle and both creation shortcuts', async () => {
    await renderRota('/')

    expect(
      await screen.findByRole('heading', { name: 'Library' }),
    ).toBeInTheDocument()
    expect(
      screen.getByText('Catalog detected from your datasources.'),
    ).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'New item' })).toHaveAttribute(
      'href',
      '/new/item',
    )
    expect(
      screen.getByRole('link', { name: 'New collection' }),
    ).toHaveAttribute('href', '/new/collection')
  })
})
