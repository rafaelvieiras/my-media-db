import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ExternalAlbumView } from '@/routes/external-album'
import type { ExternalAlbum } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson, stubExternalFetch } from '@/test/detail-harness'

// Testa a tela de "faixas faltando" do álbum (briefing 0115 Parte 2, ADR-0029):
// molde de collection-detail.test.tsx, adaptado a álbum→faixas. ExternalAlbumView
// recebe source/id/back como PROPS (não useParams) — ExternalDetailScreen é quem
// decide delegar aqui quando type==='album'; o teste exercita o componente
// diretamente, sem precisar mockar useParams.

const estado = vi.hoisted(() => ({ lang: 'pt-BR' }))
const spies = vi.hoisted(() => ({ navigate: vi.fn() }))

vi.mock('@/lib/language-context', async () => {
  const { fakeLanguage } = await import('@/test/detail-harness')
  return { useLanguage: () => fakeLanguage(estado.lang) }
})
vi.mock('@tanstack/react-router', async () => ({
  useNavigate: () => spies.navigate,
  Link: (await import('@/test/link-mock')).LinkMock,
}))

function album(over: Partial<ExternalAlbum> = {}): ExternalAlbum {
  return {
    source: 'musicbrainz',
    source_id: 'rg-1',
    name: 'Fake Album',
    year: '1999',
    genre: 'rock',
    internal_id: 'album-1',
    tracks: [
      {
        source_id: 'track-mbid-1',
        title: 'Track One',
        track_number: 1,
        disc_number: 1,
        isrc: null,
        internal_id: 'faixa-1',
      },
      {
        source_id: 'track-mbid-2',
        title: 'Track Two',
        track_number: 2,
        disc_number: 1,
        isrc: null,
        internal_id: null,
      },
    ],
    ...over,
  }
}

function renderAlbum() {
  return renderWithClient(
    <ExternalAlbumView
      source="musicbrainz"
      id="rg-1"
      back={<button type="button">Voltar</button>}
    />,
  )
}

afterEach(() => {
  vi.restoreAllMocks()
  estado.lang = 'pt-BR'
})

describe('ExternalAlbumView', () => {
  it('exibe o header do álbum (nome/ano/gênero) e o link Ver álbum quando já interno', async () => {
    stubExternalFetch(() => okJson(album()))
    renderAlbum()

    expect(
      await screen.findByRole('heading', { name: 'Fake Album' }),
    ).toBeTruthy()
    expect(screen.getByText('1999 · rock')).toBeTruthy()
    const ver = screen.getByRole('link', { name: /Ver álbum/ })
    expect(ver.getAttribute('href')).toBe('/entities/album-1')
  })

  it('faixa já catalogada linka pro detalhe interno e mostra "na biblioteca"', async () => {
    stubExternalFetch(() => okJson(album()))
    renderAlbum()
    await screen.findByRole('heading', { name: 'Fake Album' })

    const link = screen.getByRole('link', { name: 'Track One' })
    expect(link.getAttribute('href')).toBe('/entities/faixa-1')
    expect(screen.getByText('na biblioteca')).toBeTruthy()
  })

  it('faixa faltando ganha o botão Importar', async () => {
    stubExternalFetch(() => okJson(album()))
    renderAlbum()
    await screen.findByRole('heading', { name: 'Fake Album' })

    expect(screen.getByText('Track Two')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Importar' })).toBeTruthy()
  })

  it('Importar faixa envia type=faixa e parent_id=id interno do álbum', async () => {
    const fetchMock = stubExternalFetch(() => okJson(album()))
    renderAlbum()
    await screen.findByRole('heading', { name: 'Fake Album' })

    await userEvent
      .setup()
      .click(screen.getByRole('button', { name: 'Importar' }))

    await vi.waitFor(() => {
      const chamada = fetchMock.mock.calls.find(
        (c) =>
          new URL(String(c[0]), 'http://localhost').pathname ===
          '/api/v1/import',
      )
      expect(chamada).toBeTruthy()
      expect(
        JSON.parse(String((chamada?.[1] as RequestInit).body)),
      ).toMatchObject({
        source: 'musicbrainz',
        source_id: 'track-mbid-2',
        type: 'faixa',
        parent_id: 'album-1',
      })
    })
  })

  it('álbum sem faixas mostra o aviso', async () => {
    stubExternalFetch(() => okJson(album({ tracks: [] })))
    renderAlbum()
    expect(
      await screen.findByText('Este álbum não tem faixas na fonte.'),
    ).toBeTruthy()
  })

  it('404: álbum inexistente na fonte', async () => {
    stubExternalFetch(() => erroJson(404, 'not_found', 'não existe'))
    renderAlbum()
    expect(
      await screen.findByText('Álbum não encontrado na fonte'),
    ).toBeTruthy()
  })

  it('503: fonte sem a capacidade seletiva de álbum', async () => {
    stubExternalFetch(() => erroJson(503, 'unavailable', 'sem album'))
    renderAlbum()
    expect(
      await screen.findByText('Fonte indisponível no momento'),
    ).toBeTruthy()
  })
})
