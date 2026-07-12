import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import { PersonDetailScreen } from '@/routes/person-detail'
import type { Person } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson } from '@/test/detail-harness'
import { criarFetch } from '@/test/harness'

// Rota `/person/$personId` (0029, fase D): a pessoa deduplicada + filmografia
// (obras já importadas). `useParams` fixa o id; `Link` vira `<a>` simples (via
// LinkMock, com params interpolados no href).
const spies = vi.hoisted(() => ({
  navigate: vi.fn(),
  back: vi.fn(),
  params: { personId: 'p1' } as Record<string, string>,
}))

vi.mock('@tanstack/react-router', async () => {
  const { routerMock } = await import('@/test/detail-harness')
  return routerMock(spies)
})

// pessoa é o payload canônico do `GET /person/{id}` (Person, briefing 0029 fase D):
// `over` sobrescreve só o que o teste precisa (ex.: a filmografia).
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

// stubPerson roteia SÓ GET /api/v1/person/{id} com a Response dada.
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
  it('renderiza o nome e a filmografia, com o papel de cada obra', async () => {
    stubPerson(() =>
      okJson(
        pessoa({
          filmography: [
            {
              entity_id: 'e1',
              type: 'movie',
              title: 'Breaking Bad: O Filme',
              year: '2019',
              poster_url: '/api/v1/entities/e1/assets/poster',
              kind: 'cast',
              character: 'Walter White',
            },
          ],
        }),
      ),
    )
    renderWithClient(<PersonDetailScreen />)

    expect(
      await screen.findByRole('heading', { name: 'Bryan Cranston' }),
    ).toBeTruthy()
    expect(screen.getByText('Breaking Bad: O Filme')).toBeTruthy()
    expect(screen.getByText('Walter White')).toBeTruthy()
    // O card da filmografia navega ao detalhe INTERNO da entity.
    expect(
      screen.getByRole('link', { name: /Breaking Bad: O Filme/ }),
    ).toHaveAttribute('href', '/entities/e1')
  })

  it('mostra o link externo para o TMDB quando a fonte é tmdb', async () => {
    stubPerson(() => okJson(pessoa()))
    renderWithClient(<PersonDetailScreen />)
    const link = await screen.findByRole('link', { name: 'TMDB' })
    expect(link).toHaveAttribute(
      'href',
      'https://www.themoviedb.org/person/7447',
    )
  })

  it('filmografia vazia mostra a mensagem de biblioteca vazia', async () => {
    stubPerson(() => okJson(pessoa()))
    renderWithClient(<PersonDetailScreen />)
    expect(
      await screen.findByText('Nenhuma obra na sua biblioteca ainda.'),
    ).toBeTruthy()
  })

  it('pessoa inexistente (404) mostra "Pessoa não encontrada"', async () => {
    stubPerson(() => erroJson(404, 'not_found', 'pessoa nao encontrada'))
    renderWithClient(<PersonDetailScreen />)
    expect(await screen.findByText('Pessoa não encontrada')).toBeTruthy()
  })

  it('erro genérico mostra mensagem amigável', async () => {
    stubPerson(() => erroJson(502, 'upstream_error', 'fonte indisponivel'))
    renderWithClient(<PersonDetailScreen />)
    expect(
      await screen.findByText('Não foi possível carregar a pessoa'),
    ).toBeTruthy()
  })
})
