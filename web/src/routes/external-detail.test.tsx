import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ExternalDetailScreen } from '@/routes/external-detail'
import type { ExternalDetail } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { renderComEspioes } from '@/test/harness'

// Hoisted mutable state: the active language (to prove that the translatable fields
// reflect the switch) and the router spies. `vi.hoisted` lets the factory of
// `vi.mock` (hoisted above the imports) read these values without breaking the order.
const estado = vi.hoisted(() => ({ lang: 'pt-BR' }))
const spies = vi.hoisted(() => ({
  navigate: vi.fn(),
  back: vi.fn(),
  params: { source: 'tmdb', type: 'movie', id: '603' } as Record<
    string,
    string
  >,
}))

// Controllable language: the language test sets `estado.lang` before rendering.
vi.mock('@/lib/language-context', () => ({
  useLanguage: () => ({
    lang: estado.lang,
    allowed: ['pt-BR', 'en'],
    primary: 'pt-BR',
    setLang: () => {},
  }),
}))

// Mocked router: `Link` (from the shared helper) interpolates the `params` into the
// `href` (to assert the destination of the similars); `useNavigate`/
// `useRouter().history.back` become spies and `useParams` returns the source/type/id.
vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock,
  useNavigate: () => spies.navigate,
  useRouter: () => ({ history: { back: spies.back } }),
  useParams: () => spies.params,
}))

function okJson<T>(body: T, status = 200) {
  return { ok: true, status, json: async () => body } as Response
}

function erroJson(status: number, code: string, message: string) {
  return {
    ok: false,
    status,
    json: async () => ({ error: { code, message } }),
  } as Response
}

// Rich payload, ALREADY resolved in the requested language: title/tagline/synopsis
// change with the `lang` (it proves the reflection of the translatable fields); the neutral ones are the same.
function detalhe(lang: string): ExternalDetail {
  const pt = lang === 'pt-BR'
  return {
    source: 'tmdb',
    source_id: '603',
    type: 'movie',
    lang,
    title: pt ? 'Duna' : 'Dune',
    original_title: 'Dune',
    description: pt ? 'Paul Atreides desperta.' : 'Paul Atreides awakens.',
    tagline: pt ? 'O medo mata a mente.' : 'Fear is the mind-killer.',
    release_date: '2021-09-15',
    year: '2021',
    runtime: 155,
    certification: '14',
    original_language: 'en',
    vote_average: 8.1,
    genres: ['Ficção científica', 'Aventura'],
    keywords: ['deserto', 'especiaria'],
    poster_url: null,
    backdrop_url: null,
    videos: [
      {
        name: 'Trailer oficial',
        key: 'abc',
        site: 'YouTube',
        type: 'Trailer',
        official: true,
        url: 'https://youtu.be/abc',
      },
    ],
    collection: {
      source_id: '726871',
      name: 'Coleção Duna',
      poster_url: null,
    },
    similar: [
      {
        source_id: '604',
        type: 'movie',
        title: 'Duna: Parte 2',
        year: '2024',
        poster_url: null,
      },
    ],
    external_links: {
      tmdb: 'https://www.themoviedb.org/movie/603',
      imdb: 'https://www.imdb.com/title/tt1160419',
    },
    resolved_languages: { title: lang, description: lang, tagline: lang },
  }
}

// Routed fetch: the GET of the external detail returns the payload in the `lang` of the `?lang=`;
// the POST /import returns the resolved entity. `detalheResp` allows injecting an error.
function stubFetch(detalheResp?: (lang: string) => Response) {
  const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
    const u = new URL(String(url), 'http://localhost')
    const method = init?.method ?? 'GET'
    if (u.pathname.startsWith('/api/v1/external/') && method === 'GET') {
      const lang = u.searchParams.get('lang') ?? 'pt-BR'
      return detalheResp ? detalheResp(lang) : okJson(detalhe(lang))
    }
    if (u.pathname === '/api/v1/import' && method === 'POST') {
      return okJson({ id: 'ent-1', type: 'movie', title: 'Duna' }, 201)
    }
    throw new Error(`rota não mockada: ${method} ${u.pathname}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

afterEach(() => {
  vi.restoreAllMocks()
  spies.navigate.mockReset()
  spies.back.mockReset()
  spies.params = { source: 'tmdb', type: 'movie', id: '603' }
  estado.lang = 'pt-BR'
})

describe('ExternalDetailScreen', () => {
  it('exibe o conjunto rico do 0022 (pt-BR)', async () => {
    stubFetch()
    renderWithClient(<ExternalDetailScreen />)

    expect(await screen.findByRole('heading', { name: 'Duna' })).toBeTruthy()
    // Tagline highlighted, synopsis, genres, keywords (distinct fields).
    expect(screen.getByText(/O medo mata a mente/)).toBeTruthy()
    expect(screen.getByText('Paul Atreides desperta.')).toBeTruthy()
    expect(screen.getByText('Ficção científica')).toBeTruthy()
    expect(screen.getByText('deserto')).toBeTruthy()
    // Metadata: readable duration, rating, certification.
    expect(screen.getByText('2h 35min')).toBeTruthy()
    expect(screen.getByText('8.1')).toBeTruthy()
    expect(screen.getByText('14')).toBeTruthy()
    // Video with a link, collection and external links.
    expect(screen.getByText('Trailer oficial')).toBeTruthy()
    expect(screen.getByText('Coleção Duna')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'TMDB' })).toBeTruthy()
    expect(screen.getByRole('link', { name: 'IMDb' })).toBeTruthy()
  })

  it('reflete o idioma ativo nos campos traduzíveis', async () => {
    estado.lang = 'en'
    const fetchMock = stubFetch()
    renderWithClient(<ExternalDetailScreen />)

    expect(await screen.findByRole('heading', { name: 'Dune' })).toBeTruthy()
    expect(screen.getByText(/Fear is the mind-killer/)).toBeTruthy()
    expect(screen.getByText('Paul Atreides awakens.')).toBeTruthy()
    // The language entered the URL (and, therefore, the queryKey).
    const chamada = fetchMock.mock.calls.find((c) =>
      String(c[0]).includes('/api/v1/external/'),
    )
    expect(String(chamada?.[0])).toContain('lang=en')
  })

  it('trata campos ausentes e listas vazias sem quebrar', async () => {
    const magro: ExternalDetail = {
      ...detalhe('pt-BR'),
      title: 'Curta',
      original_title: 'Curta',
      description: null,
      tagline: null,
      runtime: 45,
      certification: null,
      original_language: null,
      vote_average: null,
      genres: [],
      keywords: [],
      // Non-renderable video: non-YouTube and without a url -> the section disappears. (A YouTube
      // one with a key is embeddable even without a url, so it would not serve to hide it.)
      videos: [
        { ...detalhe('pt-BR').videos[0], site: 'DailyMotion', url: null },
      ],
      collection: null,
      similar: [],
      external_links: {},
    }
    stubFetch(() => okJson(magro))
    renderWithClient(<ExternalDetailScreen />)

    expect(await screen.findByRole('heading', { name: 'Curta' })).toBeTruthy()
    // A duration < 1h stays in minutes only; blocks without data do not render.
    expect(screen.getByText('45min')).toBeTruthy()
    expect(screen.queryByText('Similares')).toBeNull()
    expect(screen.queryByText('Coleção')).toBeNull()
    expect(screen.queryByText('Vídeos')).toBeNull()
    // Importar stays available even in the lean payload.
    expect(screen.getByRole('button', { name: /Importar/ })).toBeTruthy()
  })

  it('erro genérico (400) cai na mensagem padrão', async () => {
    stubFetch(() => erroJson(400, 'bad_request', 'idioma fora dos permitidos'))
    renderWithClient(<ExternalDetailScreen />)
    expect(
      await screen.findByText('Não foi possível carregar o detalhe'),
    ).toBeTruthy()
  })

  it('vídeo do YouTube toca embutido (nocookie) só após clicar no play', async () => {
    stubFetch()
    renderWithClient(<ExternalDetailScreen />)
    await screen.findByRole('heading', { name: 'Duna' })

    // Before the click: thumbnail with a play button, no iframe loaded.
    expect(document.querySelector('iframe')).toBeNull()
    const play = screen.getByRole('button', {
      name: /Reproduzir Trailer oficial/,
    })

    await userEvent.setup().click(play)

    const iframe = document.querySelector('iframe')
    expect(iframe).not.toBeNull()
    expect(iframe?.getAttribute('src')).toBe(
      'https://www.youtube-nocookie.com/embed/abc?autoplay=1',
    )
    // The title of the trailer stays visible in the caption.
    expect(screen.getByText('Trailer oficial')).toBeTruthy()
  })

  it('vídeo não-YouTube com url vira link externo (sem player)', async () => {
    stubFetch(() =>
      okJson({
        ...detalhe('pt-BR'),
        videos: [
          {
            name: 'Clipe no Vimeo',
            key: 'v1',
            site: 'Vimeo',
            type: 'Clip',
            official: false,
            url: 'https://vimeo.com/v1',
          },
        ],
      }),
    )
    renderWithClient(<ExternalDetailScreen />)
    await screen.findByRole('heading', { name: 'Duna' })

    const link = screen.getByRole('link', { name: /Clipe no Vimeo/ })
    expect(link.getAttribute('href')).toBe('https://vimeo.com/v1')
    expect(screen.queryByRole('button', { name: /Reproduzir/ })).toBeNull()
  })

  it('similar é navegável para o detalhe externo do alvo', async () => {
    stubFetch()
    renderWithClient(<ExternalDetailScreen />)

    const link = await screen.findByRole('link', { name: /Duna: Parte 2/ })
    expect(link.getAttribute('href')).toBe('/external/tmdb/movie/604')
  })

  it('similar JÁ na biblioteca linka para o interno e mostra o selo', async () => {
    // Internal-first: the already imported part (internal_id) navigates to
    // /entities/{id} — not to the external detail — and gains the "Na biblioteca" badge.
    const comInterno = {
      ...detalhe('pt-BR'),
      similar: [
        {
          source_id: '604',
          type: 'movie',
          title: 'Duna: Parte 2',
          year: '2024',
          poster_url: null,
          internal_id: 'ent-604',
        },
      ],
    }
    stubFetch(() => okJson(comInterno))
    renderWithClient(<ExternalDetailScreen />)

    const link = await screen.findByRole('link', { name: /Duna: Parte 2/ })
    expect(link.getAttribute('href')).toBe('/entities/ent-604')
    expect(screen.getByText('Na biblioteca')).toBeTruthy()
  })

  it('Voltar retorna no histórico', async () => {
    stubFetch()
    renderWithClient(<ExternalDetailScreen />)
    await screen.findByRole('heading', { name: 'Duna' })

    await userEvent
      .setup()
      .click(screen.getByRole('button', { name: /Voltar/ }))
    expect(spies.back).toHaveBeenCalledTimes(1)
  })

  it('Importar cria a entity, invalida o grid e navega ao catálogo', async () => {
    stubFetch()
    const { invalidateSpy } = renderComEspioes(<ExternalDetailScreen />)
    await screen.findByRole('heading', { name: 'Duna' })

    await userEvent
      .setup()
      .click(screen.getByRole('button', { name: /Importar/ }))

    await waitFor(() =>
      expect(spies.navigate).toHaveBeenCalledWith({
        to: '/entities/$id',
        params: { id: 'ent-1' },
      }),
    )
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['entities'] })
  })

  it('404: item inexistente na fonte', async () => {
    stubFetch(() => erroJson(404, 'not_found', 'não existe na fonte'))
    renderWithClient(<ExternalDetailScreen />)
    expect(await screen.findByText('Item não encontrado na fonte')).toBeTruthy()
  })

  it('502: fonte indisponível', async () => {
    stubFetch(() => erroJson(502, 'upstream_error', 'a fonte falhou'))
    renderWithClient(<ExternalDetailScreen />)
    expect(
      await screen.findByText('Fonte indisponível no momento'),
    ).toBeTruthy()
  })

  it('503: provider desligado', async () => {
    stubFetch(() => erroJson(503, 'unavailable', 'sem provider'))
    renderWithClient(<ExternalDetailScreen />)
    expect(
      await screen.findByText('Fonte indisponível no momento'),
    ).toBeTruthy()
  })

  // type==='album': it delegates to ExternalAlbumView (tracks
  // of the release), never to the RichDetailView of a single item.
  it('type=album delega à tela de faixas do álbum, não ao RichDetailView', async () => {
    spies.params = { source: 'musicbrainz', type: 'album', id: 'rg-1' }
    stubFetch(() =>
      okJson({
        source: 'musicbrainz',
        source_id: 'rg-1',
        name: 'Fake Album',
        year: '1999',
        genre: 'rock',
        internal_id: 'album-1',
        tracks: [],
      }),
    )
    renderWithClient(<ExternalDetailScreen />)

    expect(
      await screen.findByRole('heading', { name: 'Fake Album' }),
    ).toBeTruthy()
    expect(screen.getByText('Faixas do álbum')).toBeTruthy()
    // It never renders the fields exclusive to the rich detail of a single item.
    expect(screen.queryByRole('button', { name: /Importar/ })).toBeNull()
  })
})
