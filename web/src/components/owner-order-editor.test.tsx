import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { OwnerOrderEditor } from '@/components/owner-order-editor'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson } from '@/test/detail-harness'
import { corpoEnviado, stubRotas } from '@/test/stub-fetch'

// Tests the OWNER ORDER editor (ADR-0033): start from a base order, merge episodes into
// one unit, split back, and the two mutations (save with the exact payload, delete).
vi.mock('@/lib/language-context', () => ({
  useLanguage: () => ({
    lang: 'en',
    allowed: ['en'],
    primary: 'en',
    setLang: () => {},
  }),
}))

// The grouped episode response of the base order: three episodes in one season.
function gruposBase() {
  return {
    ordering: 'official',
    groups: [
      {
        season: 1,
        label: 'Season 1',
        episodes: [
          { id: 'e1', title: 'Blackout', numbering: { season: 1, number: 1 } },
          { id: 'e2', title: 'The Wire', numbering: { season: 1, number: 2 } },
          { id: 'e3', title: 'Tremé', numbering: { season: 1, number: 3 } },
        ],
      },
    ],
  }
}

// stubOrdem installs the four routes the editor talks to. Returns the fetch mock so the
// tests can inspect what was sent (corpoEnviado) and which methods were called.
function stubOrdem(
  ordem: unknown,
  respostaPut?: () => Response,
  respostaDelete?: () => Response,
) {
  return stubRotas([
    [(p, m) => p === '/api/v1/entities/s1' && m === 'GET', () => okJson({})],
    [
      (p, m) => p === '/api/v1/entities/s1/owner-order' && m === 'GET',
      () => okJson(ordem),
    ],
    [
      (p, m) => p === '/api/v1/entities/s1/episodes' && m === 'GET',
      () => okJson(gruposBase()),
    ],
    [
      (p, m) => p === '/api/v1/entities/s1/owner-order' && m === 'PUT',
      respostaPut ?? (() => okJson({ name: '', based_on: '', units: [] })),
    ],
    [
      (p, m) => p === '/api/v1/entities/s1/owner-order' && m === 'DELETE',
      respostaDelete ?? (() => new Response(null, { status: 204 })),
    ],
  ])
}

// metodosChamados lists the HTTP methods the editor fired, in order.
function metodosChamados(m: ReturnType<typeof vi.fn>): string[] {
  return m.mock.calls.map((c) => String((c[1] as RequestInit)?.method ?? 'GET'))
}

const vazio = { name: '', based_on: '', units: [] }

describe('OwnerOrderEditor', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('starts from the base order and saves one unit per episode', async () => {
    const stubs = stubOrdem(vazio)
    const user = userEvent.setup()
    renderWithClient(
      <OwnerOrderEditor id="s1" base="official" open onOpenChange={() => {}} />,
    )

    const criar = await screen.findByRole('button', {
      name: 'Start from the official order',
    })
    await waitFor(() => expect(criar.hasAttribute('disabled')).toBe(false))
    await user.click(criar)

    // Three units, one per episode: each episode title appears twice — once in the unit
    // (the chip) and once in the episode list — so the count is the assertion.
    expect(screen.getAllByText('Blackout')).toHaveLength(2)
    expect(screen.getAllByText('The Wire')).toHaveLength(2)
    expect(screen.getAllByText('Tremé')).toHaveLength(2)

    await user.click(screen.getByRole('button', { name: 'Save order' }))

    expect(metodosChamados(stubs)).toContain('PUT')
    const put = corpoEnviado(
      stubs,
      metodosChamados(stubs).indexOf('PUT'),
    ) as Record<string, unknown>
    expect(put).toMatchObject({
      based_on: 'official',
      units: [
        { season: 1, number: 1, episodes: ['e1'] },
        { season: 1, number: 2, episodes: ['e2'] },
        { season: 1, number: 3, episodes: ['e3'] },
      ],
    })
  })

  it('merges the selected episodes into ONE unit and splits it back', async () => {
    const stubs = stubOrdem(vazio)
    const user = userEvent.setup()
    renderWithClient(
      <OwnerOrderEditor id="s1" base="official" open onOpenChange={() => {}} />,
    )

    await user.click(
      await screen.findByRole('button', {
        name: 'Start from the official order',
      }),
    )

    // Select the first two episodes and merge them.
    const caixas = screen.getAllByRole('checkbox')
    await user.click(caixas[0])
    await user.click(caixas[1])
    await user.click(screen.getByRole('button', { name: 'Merge 2 selected' }))

    // One unit now holds both episodes.
    expect(screen.getByText('2 episodes')).toBeTruthy()

    await user.click(screen.getByRole('button', { name: 'Save order' }))
    const put = corpoEnviado(
      stubs,
      metodosChamados(stubs).indexOf('PUT'),
    ) as Record<string, unknown>
    expect(put).toMatchObject({
      units: [
        { season: 1, number: 1, episodes: ['e1', 'e2'] },
        { season: 1, number: 3, episodes: ['e3'] },
      ],
    })
  })

  it('loads a stored order, and removes it', async () => {
    const stubs = stubOrdem({
      name: 'HBO 2004',
      based_on: 'official',
      units: [
        {
          season: 5,
          number: 5,
          title: 'Blackout',
          episodes: ['e1', 'e2', 'e3'],
        },
      ],
    })
    const user = userEvent.setup()
    renderWithClient(
      <OwnerOrderEditor id="s1" base="official" open onOpenChange={() => {}} />,
    )

    expect(await screen.findByDisplayValue('HBO 2004')).toBeTruthy()
    expect(screen.getByText('3 episodes')).toBeTruthy()

    await user.click(screen.getByRole('button', { name: 'Remove order' }))
    await waitFor(() => expect(metodosChamados(stubs)).toContain('DELETE'))
  })

  it('shows the save error instead of closing when the backend refuses', async () => {
    const onOpenChange = vi.fn()
    stubOrdem(vazio, () =>
      erroJson(
        422,
        'episode_not_in_series',
        'Episódio não pertence a esta série.',
      ),
    )
    const user = userEvent.setup()
    renderWithClient(
      <OwnerOrderEditor
        id="s1"
        base="official"
        open
        onOpenChange={onOpenChange}
      />,
    )

    await user.click(
      await screen.findByRole('button', {
        name: 'Start from the official order',
      }),
    )
    await user.click(screen.getByRole('button', { name: 'Save order' }))

    expect(await screen.findByText('Could not save the order.')).toBeTruthy()
    expect(onOpenChange).not.toHaveBeenCalledWith(false)
  })

  it('reorders, splits, renumbers and removes units before saving', async () => {
    const stubs = stubOrdem(vazio)
    const user = userEvent.setup()
    renderWithClient(
      <OwnerOrderEditor id="s1" base="official" open onOpenChange={() => {}} />,
    )

    await user.click(
      await screen.findByRole('button', {
        name: 'Start from the official order',
      }),
    )

    // Move the first unit down: the order of the units is the order of the release.
    await user.click(screen.getAllByRole('button', { name: 'Move down' })[0])
    // Merge two episodes, then split the merged unit back into two.
    const caixas = screen.getAllByRole('checkbox')
    await user.click(caixas[0])
    await user.click(caixas[1])
    await user.click(screen.getByRole('button', { name: 'Merge 2 selected' }))
    await user.click(screen.getAllByRole('button', { name: 'Split' })[0])
    // Remove the unit of the third episode.
    await user.click(screen.getAllByRole('button', { name: 'Remove' })[2])
    // Renumber the whole order 1..N.
    await user.click(screen.getByRole('button', { name: 'Units 1..N' }))

    await user.click(screen.getByRole('button', { name: 'Save order' }))

    const metodos = metodosChamados(stubs)
    const put = corpoEnviado(stubs, metodos.indexOf('PUT')) as {
      units: { season: number; number: number; episodes: string[] }[]
    }
    // The moves/splits/removals above leave e1 and e2, renumbered 1 and 2 by the button.
    expect(put.units).toEqual([
      { season: 1, number: 1, episodes: ['e1'] },
      { season: 1, number: 2, episodes: ['e2'] },
    ])
  })

  it('edits the season and number of a unit', async () => {
    const stubs = stubOrdem(vazio)
    const user = userEvent.setup()
    renderWithClient(
      <OwnerOrderEditor id="s1" base="official" open onOpenChange={() => {}} />,
    )

    await user.click(
      await screen.findByRole('button', {
        name: 'Start from the official order',
      }),
    )

    // The numeric inputs come in pairs per unit: season then number. fireEvent is used
    // here (not user.type) because Radix's dialog owns the focus scope and jsdom refuses
    // the keyboard path — the onChange handler is the same one the browser triggers.
    const campos = screen.getAllByRole('spinbutton')
    fireEvent.change(campos[0], { target: { value: '5' } })
    fireEvent.change(campos[1], { target: { value: '12' } })

    await user.click(screen.getByRole('button', { name: 'Save order' }))

    const metodos = metodosChamados(stubs)
    const put = corpoEnviado(stubs, metodos.indexOf('PUT')) as {
      units: { season: number; number: number; episodes: string[] }[]
    }
    expect(put.units[0]).toEqual({ season: 5, number: 12, episodes: ['e1'] })
  })

  it('reports a failing delete without closing the drawer', async () => {
    const onOpenChange = vi.fn()
    stubOrdem(
      {
        name: 'HBO 2004',
        based_on: 'official',
        units: [{ season: 1, number: 1, episodes: ['e1'] }],
      },
      undefined,
      () => erroJson(500, 'internal', 'Falha.'),
    )
    const user = userEvent.setup()
    renderWithClient(
      <OwnerOrderEditor
        id="s1"
        base="official"
        open
        onOpenChange={onOpenChange}
      />,
    )

    await user.click(
      await screen.findByRole('button', { name: 'Remove order' }),
    )
    expect(await screen.findByText('Could not save the order.')).toBeTruthy()
    expect(onOpenChange).not.toHaveBeenCalledWith(false)
  })
})
