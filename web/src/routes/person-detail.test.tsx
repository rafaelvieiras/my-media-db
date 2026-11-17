import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import { PersonDetailScreen } from '@/routes/person-detail'
import type { Person, PersonCredit } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson } from '@/test/detail-harness'
import { criarFetch } from '@/test/harness'

// Route `/person/$personId` (phase D): the deduplicated person + filmography
// (works already imported). `useParams` fixes the id; `Link` becomes a plain `<a>` (via
// LinkMock, with params interpolated into the href).
const spies = vi.hoisted(() => ({
  navigate: vi.fn(),
  back: vi.fn(),
  params: { personId: 'p1' } as Record<string, string>,
}))

vi.mock('@tanstack/react-router', async () => {
  const { routerMock } = await import('@/test/detail-harness')
  return routerMock(spies)
})

// pessoa is the canonical payload of `GET /person/{id}` (Person, phase D):
// `over` overrides only what the test needs (e.g. the filmography).
function pessoa(over: Partial<Person> = {}): Person {
  return {
    id: 'p1',
    name: 'Bryan Cranston',
    profile_url: null,
    source: 'tmdb',
    source_id: '7447',
    filmography: [],
    ...over,
  }
}

// creditoBB is the cast credit reused by several tests (avoids repeating the same
// literal — the ratchet measures duplication even in the tests). `over` adjusts whatever varies.
function creditoBB(over: Partial<PersonCredit> = {}): PersonCredit {
  return {
    entity_id: 'e1',
    type: 'movie',
    title: 'Breaking Bad: O Filme',
    year: '2019',
    poster_url: '/api/v1/entities/e1/assets/poster',
    kind: 'cast',
    character: 'Walter White',
    ...over,
  }
}

// stubPerson routes ONLY GET /api/v1/person/{id} with the given Response.
function stubPerson(resp: () => Response) {
  vi.stubGlobal(
    'fetch',
    criarFetch(({ path }) => {
      if (/^\/api\/v1\/person\/[^/]+$/.test(path)) {
        return resp()
      }
      throw new Error(`rota nao mockada: ${path}`)
    }),
  )
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('PersonDetailScreen', () => {
  it('renders the name and the filmography, with the role of each work', async () => {
    stubPerson(() => okJson(pessoa({ filmography: [creditoBB()] })))
    renderWithClient(<PersonDetailScreen />)

    expect(
      await screen.findByRole('heading', { name: 'Bryan Cranston' }),
    ).toBeTruthy()
    expect(screen.getByText('Breaking Bad: O Filme')).toBeTruthy()
    expect(screen.getByText('Walter White')).toBeTruthy()
    // The filmography card navigates to the INTERNAL detail of the entity.
    expect(
      screen.getByRole('link', { name: /Breaking Bad: O Filme/ }),
    ).toHaveAttribute('href', '/entities/e1')
  })

  it('renders both sections when both arrays have items', async () => {
    stubPerson(() =>
      okJson(
        pessoa({
          filmography: [creditoBB()],
          filmography_external: [
            {
              source: 'tmdb',
              source_id: '604',
              type: 'movie',
              title: 'Filme Ainda Não Importado',
              year: '2021',
              poster_url: 'https://proxy.local/poster.jpg',
              kind: 'crew',
              job: 'Diretor',
            },
          ],
        }),
      ),
    )
    renderWithClient(<PersonDetailScreen />)

    expect(
      await screen.findByRole('heading', { name: 'In your library' }),
    ).toBeTruthy()
    expect(
      screen.getByRole('heading', { name: 'Not imported yet' }),
    ).toBeTruthy()

    // An imported card navigates to the INTERNAL detail of the entity.
    expect(
      screen.getByRole('link', { name: /Breaking Bad: O Filme/ }),
    ).toHaveAttribute('href', '/entities/e1')

    // A non-imported card navigates to the EXTERNAL detail (same route as search/similars).
    const linkExterno = screen.getByRole('link', {
      name: /Filme Ainda Não Importado \(not imported yet\)/,
    })
    expect(linkExterno).toHaveAttribute('href', '/external/tmdb/movie/604')
    expect(screen.getByText('Diretor')).toBeTruthy()
    expect(screen.getByText('Not imported')).toBeTruthy()
  })

  it('empty or absent filmography_external does not show the external section', async () => {
    stubPerson(() =>
      okJson(pessoa({ filmography: [creditoBB({ poster_url: null })] })),
    )
    renderWithClient(<PersonDetailScreen />)

    expect(
      await screen.findByRole('heading', { name: 'In your library' }),
    ).toBeTruthy()
    expect(
      screen.queryByRole('heading', { name: 'Not imported yet' }),
    ).toBeNull()
  })

  it('empty filmography but full filmography_external shows only the external section, without the global empty message', async () => {
    stubPerson(() =>
      okJson(
        pessoa({
          filmography: [],
          filmography_external: [
            {
              source: 'tmdb',
              source_id: '604',
              type: 'movie',
              title: 'Filme Ainda Não Importado',
              year: '2021',
              poster_url: null,
              kind: 'cast',
              character: 'Ela mesma',
            },
          ],
        }),
      ),
    )
    renderWithClient(<PersonDetailScreen />)

    expect(
      await screen.findByRole('heading', { name: 'Not imported yet' }),
    ).toBeTruthy()
    expect(
      screen.queryByRole('heading', { name: 'In your library' }),
    ).toBeNull()
    expect(screen.queryByText('No works in your library yet.')).toBeNull()
  })

  it('shows the external link to TMDB when the source is tmdb', async () => {
    stubPerson(() => okJson(pessoa()))
    renderWithClient(<PersonDetailScreen />)
    const link = await screen.findByRole('link', { name: 'TMDB' })
    expect(link).toHaveAttribute(
      'href',
      'https://www.themoviedb.org/person/7447',
    )
  })

  it('empty filmography shows the empty library message', async () => {
    stubPerson(() => okJson(pessoa()))
    renderWithClient(<PersonDetailScreen />)
    expect(
      await screen.findByText('No works in your library yet.'),
    ).toBeTruthy()
  })

  it('missing person (404) shows "Person not found"', async () => {
    stubPerson(() => erroJson(404, 'not_found', 'pessoa nao encontrada'))
    renderWithClient(<PersonDetailScreen />)
    expect(await screen.findByText('Person not found')).toBeTruthy()
  })

  it('generic error shows a friendly message', async () => {
    stubPerson(() => erroJson(502, 'upstream_error', 'fonte indisponivel'))
    renderWithClient(<PersonDetailScreen />)
    expect(await screen.findByText('Could not load the person')).toBeTruthy()
  })
})
