import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ExternalDetailScreen } from '@/routes/external-detail'
import type { ExternalDetail } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { renderComEspioes } from '@/test/harness'

// Estado mutável hoisteado: o idioma ativo (para provar que os traduzíveis
// refletem a troca) e os spies do router. `vi.hoisted` deixa o factory do
// `vi.mock` (içado acima dos imports) ler estes valores sem quebrar a ordem.
const estado = vi.hoisted(() => ({ lang: 'pt-BR' }))
const spies = vi.hoisted(() => ({
  navigate: vi.fn(),
  back: vi.fn(),
  params: { source: 'tmdb', type: 'movie', id: '603' } as Record<
    string,
    string
  >,
}))

// Idioma controlável: o teste de idioma seta `estado.lang` antes de renderizar.
vi.mock('@/lib/language-context', () => ({
  useLanguage: () => ({
    lang: estado.lang,
    allowed: ['pt-BR', 'en'],
    primary: 'pt-BR',
    setLang: () => {},
  }),
}))

// Router mockado: `Link` (do helper compartilhado) interpola os `params` no
// `href` (para afirmar o destino dos similares); `useNavigate`/
// `useRouter().history.back` viram spies e `useParams` devolve a fonte/tipo/id.
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

// Payload rico do 0022, JÁ resolvido no idioma pedido: título/slogan/sinopse
// mudam com o `lang` (prova a reflexão dos traduzíveis); os neutros são iguais.
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

// fetch roteado: GET do detalhe externo devolve o payload no `lang` do `?lang=`;
// POST /import devolve a entity resolvida. `detalheResp` permite injetar erro.
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
  estado.lang = 'pt-BR'
})

describe('ExternalDetailScreen', () => {
  it('exibe o conjunto rico do 0022 (pt-BR)', async () => {
    stubFetch()
    renderWithClient(<ExternalDetailScreen />)

    expect(await screen.findByRole('heading', { name: 'Duna' })).toBeTruthy()
    // Slogan em destaque, sinopse, gêneros, palavras-chave (campos distintos).
    expect(screen.getByText(/O medo mata a mente/)).toBeTruthy()
    expect(screen.getByText('Paul Atreides desperta.')).toBeTruthy()
    expect(screen.getByText('Ficção científica')).toBeTruthy()
    expect(screen.getByText('deserto')).toBeTruthy()
    // Metadados: duração legível, nota, classificação.
    expect(screen.getByText('2h 35min')).toBeTruthy()
    expect(screen.getByText('8.1')).toBeTruthy()
    expect(screen.getByText('14')).toBeTruthy()
    // Vídeo com link, coleção e links externos.
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
    // O idioma entrou na URL (e, portanto, na queryKey).
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
      videos: [{ ...detalhe('pt-BR').videos[0], url: null }],
      collection: null,
      similar: [],
      external_links: {},
    }
    stubFetch(() => okJson(magro))
    renderWithClient(<ExternalDetailScreen />)

    expect(await screen.findByRole('heading', { name: 'Curta' })).toBeTruthy()
    // Duração < 1h fica só em minutos; blocos sem dado não renderizam.
    expect(screen.getByText('45min')).toBeTruthy()
    expect(screen.queryByText('Similares')).toBeNull()
    expect(screen.queryByText('Coleção')).toBeNull()
    expect(screen.queryByText('Vídeos')).toBeNull()
    // Importar segue disponível mesmo no payload magro.
    expect(screen.getByRole('button', { name: /Importar/ })).toBeTruthy()
  })

  it('erro genérico (400) cai na mensagem padrão', async () => {
    stubFetch(() => erroJson(400, 'bad_request', 'idioma fora dos permitidos'))
    renderWithClient(<ExternalDetailScreen />)
    expect(
      await screen.findByText('Não foi possível carregar o detalhe'),
    ).toBeTruthy()
  })

  it('similar é navegável para o detalhe externo do alvo', async () => {
    stubFetch()
    renderWithClient(<ExternalDetailScreen />)

    const link = await screen.findByRole('link', { name: /Duna: Parte 2/ })
    expect(link.getAttribute('href')).toBe('/external/tmdb/movie/604')
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
})
