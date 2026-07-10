import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { CollectionDetailScreen } from '@/routes/collection-detail'
import type { ExternalCollectionDetail } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson, stubExternalFetch } from '@/test/detail-harness'

const estado = vi.hoisted(() => ({ lang: 'pt-BR' }))
const spies = vi.hoisted(() => ({
  navigate: vi.fn(),
  back: vi.fn(),
  params: { source: 'tmdb', id: '726871' } as Record<string, string>,
}))

// Os factories do vi.mock são içados acima dos imports: usam import() DINÂMICO do
// harness (resolvido em tempo de chamada, sem TDZ) e leem os hoisted `estado`/`spies`.
vi.mock('@/lib/language-context', async () => {
  const { fakeLanguage } = await import('@/test/detail-harness')
  return { useLanguage: () => fakeLanguage(estado.lang) }
})
vi.mock('@tanstack/react-router', async () => {
  const { routerMock } = await import('@/test/detail-harness')
  return routerMock(spies)
})

function colecao(
  over: Partial<ExternalCollectionDetail> = {},
): ExternalCollectionDetail {
  return {
    source: 'tmdb',
    source_id: '726871',
    name: 'Coleção Duna',
    overview: 'A saga de Arrakis.',
    poster_url: null,
    backdrop_url: null,
    parts: [
      {
        source_id: '438631',
        type: 'movie',
        title: 'Duna',
        year: '2021',
        overview: 'Paul desperta.',
        poster_url: null,
      },
      {
        source_id: '693134',
        type: 'movie',
        title: 'Duna: Parte 2',
        year: '2024',
        overview: null,
        poster_url: null,
      },
    ],
    ...over,
  }
}

afterEach(() => {
  vi.restoreAllMocks()
  spies.back.mockReset()
  estado.lang = 'pt-BR'
})

describe('CollectionDetailScreen', () => {
  it('exibe o header e as partes da coleção', async () => {
    stubExternalFetch(() => okJson(colecao()))
    renderWithClient(<CollectionDetailScreen />)

    expect(
      await screen.findByRole('heading', { name: 'Coleção Duna' }),
    ).toBeTruthy()
    expect(screen.getByText('A saga de Arrakis.')).toBeTruthy()
    expect(screen.getByText('Duna')).toBeTruthy()
    expect(screen.getByText('Duna: Parte 2')).toBeTruthy()
  })

  it('renderiza o backdrop da coleção quando presente', async () => {
    stubExternalFetch(() =>
      okJson(colecao({ backdrop_url: 'https://img.tmdb.org/colbg.jpg' })),
    )
    renderWithClient(<CollectionDetailScreen />)
    await screen.findByRole('heading', { name: 'Coleção Duna' })

    const banner = document.querySelector(
      'img[src="https://img.tmdb.org/colbg.jpg"]',
    )
    expect(banner).not.toBeNull()
  })

  it('parte navega ao detalhe externo e traz o atalho Importar', async () => {
    stubExternalFetch(() => okJson(colecao()))
    renderWithClient(<CollectionDetailScreen />)

    const link = await screen.findByRole('link', { name: /Duna: Parte 2/ })
    expect(link.getAttribute('href')).toBe('/external/tmdb/movie/693134')
    // Reuso do ExternalResultCard: cada parte tem o botão Importar.
    expect(screen.getAllByRole('button', { name: /Importar/ }).length).toBe(2)
  })

  it('coleção sem itens mostra o aviso', async () => {
    stubExternalFetch(() => okJson(colecao({ parts: [] })))
    renderWithClient(<CollectionDetailScreen />)
    expect(await screen.findByText('Esta coleção não tem itens.')).toBeTruthy()
  })

  it('passa o idioma ativo na busca (entra na queryKey)', async () => {
    estado.lang = 'en'
    const fetchMock = stubExternalFetch(() => okJson(colecao()))
    renderWithClient(<CollectionDetailScreen />)
    await screen.findByRole('heading', { name: 'Coleção Duna' })

    const chamada = fetchMock.mock.calls.find((c) =>
      String(c[0]).includes('/collection/'),
    )
    expect(String(chamada?.[0])).toContain('lang=en')
  })

  it('404: coleção inexistente na fonte', async () => {
    stubExternalFetch(() => erroJson(404, 'not_found', 'não existe'))
    renderWithClient(<CollectionDetailScreen />)
    expect(
      await screen.findByText('Coleção não encontrada na fonte'),
    ).toBeTruthy()
  })

  it('503: fonte sem coleção', async () => {
    stubExternalFetch(() => erroJson(503, 'unavailable', 'sem coleção'))
    renderWithClient(<CollectionDetailScreen />)
    expect(
      await screen.findByText('Fonte indisponível no momento'),
    ).toBeTruthy()
  })

  it('erro genérico (400) cai na mensagem padrão', async () => {
    stubExternalFetch(() => erroJson(400, 'bad_request', 'idioma inválido'))
    renderWithClient(<CollectionDetailScreen />)
    expect(
      await screen.findByText('Não foi possível carregar a coleção'),
    ).toBeTruthy()
  })

  it('Voltar retorna no histórico', async () => {
    stubExternalFetch(() => okJson(colecao()))
    renderWithClient(<CollectionDetailScreen />)
    await screen.findByRole('heading', { name: 'Coleção Duna' })

    await userEvent
      .setup()
      .click(screen.getByRole('button', { name: /Voltar/ }))
    expect(spies.back).toHaveBeenCalledTimes(1)
  })
})
