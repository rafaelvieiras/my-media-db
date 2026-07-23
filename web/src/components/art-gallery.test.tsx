import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ArtGallery } from '@/components/art-gallery'
import type { ArtworkItem, ArtworkResponse } from '@/lib/api'
import { baseAsset, okJson } from '@/test/entity-fixtures'
import { chavesInvalidadas } from '@/test/harness'

// Artes (0049): a galeria consome `GET /artwork` (0048) e rende UMA LINHA por kind;
// o lightbox abre a arte em alta com navegação e as ações (preferir/baixar/remover).
// Os espiões leem COM QUE a UI bateu na API (id do prefer/delete, corpo do select).

interface Espioes {
  onSelect: ReturnType<typeof vi.fn<(body: unknown) => void>>
  onPrefer: ReturnType<typeof vi.fn<(assetId: string) => void>>
  onMaterialize: ReturnType<typeof vi.fn<(assetId: string) => void>>
  onDelete: ReturnType<typeof vi.fn<(assetId: string) => void>>
  onUpload: ReturnType<typeof vi.fn<(kind: unknown, fileName: string) => void>>
}

interface Opcoes {
  artwork?: ArtworkResponse
  type?: string
  stillUrl?: string | null
  owned?: boolean
}

// Item de arte de teste, com sobrescritas pontuais. `is_local_in_library`/
// `is_principal` (briefing 0062) default `false` — sobrescreva por caso.
function item(over: Partial<ArtworkItem> = {}): ArtworkItem {
  return {
    url: '/api/v1/images?src=https%3A%2F%2Fassets.fanart.tv%2Fx.jpg',
    source: 'fanarttv',
    is_preferred: false,
    is_local: false,
    is_local_in_library: false,
    is_principal: false,
    src_url: 'https://assets.fanart.tv/x.jpg',
    lang: 'en',
    likes: 5,
    ...over,
  }
}

// Arte padrão do filme: pôster com [inventário, local do dono, local preferida] +
// uma fanart 16:9 (para conferir o aspect por kind).
function artworkPadrao(): ArtworkResponse {
  return {
    poster: [
      item({ source: 'fanarttv', is_local: false, is_preferred: false }),
      item({
        url: '/api/v1/entities/e1/assets/p1/file',
        source: 'user',
        is_local: true,
        // Cacheado internamente, mas AINDA não salvo na pasta real (0062) — exercita
        // "Salvar na pasta" isolado de "Definir como principal".
        is_local_in_library: false,
        is_preferred: false,
        asset_id: 'p1',
        src_url: undefined,
        lang: undefined,
        likes: undefined,
      }),
      item({
        url: '/api/v1/entities/e1/assets/p2/file',
        source: 'external',
        is_local: true,
        is_local_in_library: true,
        is_preferred: true,
        is_principal: true,
        asset_id: 'p2',
        src_url: undefined,
        lang: undefined,
        likes: undefined,
      }),
    ],
    fanart: [
      item({
        url: '/api/v1/images?src=https%3A%2F%2Fassets.fanart.tv%2Ff.jpg',
        src_url: 'https://assets.fanart.tv/f.jpg',
      }),
    ],
    available: { poster: 3, fanart: 1 },
  }
}

// caminhoEMetodo extrai (path, method) de uma chamada fetch mockada — parse
// compartilhado por todo mock de rota deste arquivo (jscpd).
function caminhoEMetodo(url: string, init?: RequestInit) {
  const u = new URL(String(url), 'http://localhost')
  return { path: u.pathname, method: init?.method ?? 'GET' }
}

function fetchRouter(opts: Opcoes, espioes: Espioes) {
  const artwork = opts.artwork ?? {}
  return vi.fn(async (url: string, init?: RequestInit) => {
    const { path, method } = caminhoEMetodo(url, init)

    if (path === '/api/v1/entities/e1/artwork' && method === 'GET') {
      return okJson(artwork)
    }
    if (path === '/api/v1/entities/e1/artwork/select' && method === 'POST') {
      espioes.onSelect(JSON.parse(String(init?.body)))
      return okJson(baseAsset({ id: 'novo', origin: 'external' }))
    }
    if (path === '/api/v1/entities/e1/assets' && method === 'POST') {
      const form = init?.body as FormData
      const file = form.get('file') as File
      espioes.onUpload(form.get('kind'), file.name)
      return okJson(baseAsset({ id: 'up', origin: 'user' }))
    }
    const prefer = path.match(/^\/api\/v1\/entities\/e1\/assets\/(.+)\/prefer$/)
    if (prefer && method === 'PUT') {
      espioes.onPrefer(prefer[1])
      return okJson(
        baseAsset({ id: prefer[1], preferred: true, is_principal: true }),
      )
    }
    const materialize = path.match(
      /^\/api\/v1\/entities\/e1\/assets\/(.+)\/materialize$/,
    )
    if (materialize && method === 'POST') {
      espioes.onMaterialize(materialize[1])
      return okJson(baseAsset({ id: materialize[1], local_path: '/lib/p.jpg' }))
    }
    const del = path.match(/^\/api\/v1\/entities\/e1\/assets\/(.+)$/)
    if (del && method === 'DELETE') {
      espioes.onDelete(del[1])
      return okJson(baseAsset({ id: del[1] }))
    }
    throw new Error(`rota nao mockada: ${method} ${path}`)
  })
}

afterEach(() => {
  vi.restoreAllMocks()
})

function renderGallery(opts: Opcoes = {}) {
  const espioes: Espioes = {
    onSelect: vi.fn<(body: unknown) => void>(),
    onPrefer: vi.fn<(assetId: string) => void>(),
    onMaterialize: vi.fn<(assetId: string) => void>(),
    onDelete: vi.fn<(assetId: string) => void>(),
    onUpload: vi.fn<(kind: unknown, fileName: string) => void>(),
  }
  vi.stubGlobal('fetch', fetchRouter(opts, espioes))
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')
  render(
    <QueryClientProvider client={queryClient}>
      <ArtGallery
        entityId="e1"
        type={opts.type ?? 'movie'}
        stillUrl={opts.stillUrl ?? null}
        owned={opts.owned ?? true}
      />
    </QueryClientProvider>,
  )
  return { espioes, invalidateSpy }
}

// renderGalleryDinamica monta a galeria com um mock de fetch PRÓPRIO (não o
// fetchRouter fixo de artworkPadrao()): usado pelos testes de reancoragem do
// lightbox (0072), cujo GET /artwork precisa responder DIFERENTE a cada
// chamada (a lista muda de verdade após a ação).
function renderGalleryDinamica(
  fetchImpl: (url: string, init?: RequestInit) => Promise<Response>,
) {
  vi.stubGlobal('fetch', vi.fn(fetchImpl))
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={queryClient}>
      <ArtGallery entityId="e1" type="movie" owned />
    </QueryClientProvider>,
  )
}

// Abre o lightbox no card `label` e espera o dialog aparecer.
async function abrirLightbox(
  user: ReturnType<typeof userEvent.setup>,
  label: string,
) {
  await user.click(await screen.findByRole('button', { name: label }))
  return screen.findByRole('dialog')
}

describe('ArtGallery — linhas por kind (0049)', () => {
  it('(a) rende uma linha por kind, cada card no aspect certo', async () => {
    renderGallery({ artwork: artworkPadrao() })

    // Pôster no frame retrato 2:3; fanart no frame paisagem 16:9 — sem deformar.
    const poster = (await screen.findByAltText('Pôster 1')) as HTMLImageElement
    const fanart = screen.getByAltText('Fanart 1') as HTMLImageElement
    expect(poster.className).toContain('aspect-[2/3]')
    expect(fanart.className).toContain('aspect-video')
    // Rótulo do kind visível em cada linha (heading da seção).
    expect(
      screen.getByRole('heading', { name: 'Pôster', level: 3 }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('heading', { name: 'Fanart', level: 3 }),
    ).toBeInTheDocument()
  })

  it('(b) marca a arte principal (aria-label + selo, briefing 0062)', async () => {
    renderGallery({ artwork: artworkPadrao() })
    // O 3o pôster é o principal: o botão o anuncia como tal.
    expect(
      await screen.findByRole('button', { name: 'Ver Pôster 3 (principal)' }),
    ).toBeInTheDocument()
  })

  it('(c) estado vazio quando não há artes', async () => {
    renderGallery({ artwork: { available: {} } })
    await waitFor(() =>
      expect(
        screen.getByText('Nenhuma arte disponível ainda.'),
      ).toBeInTheDocument(),
    )
  })

  it('(d) episódio sem still no inventário: card provider read-only do stillUrl', async () => {
    const raw = 'https://artworks.thetvdb.com/s.jpg'
    renderGallery({
      type: 'episode',
      artwork: { available: {} },
      stillUrl: raw,
    })
    const img = (await screen.findByAltText(
      'Still do provider',
    )) as HTMLImageElement
    expect(img.getAttribute('src')).toBe(
      `/api/v1/images?src=${encodeURIComponent(raw)}`,
    )
    expect(img.className).toContain('aspect-video')
  })
})

describe('ArtGallery — lightbox: navegação e teclado (0049)', () => {
  it('(e) abre no card, navega com as setas e fecha no Esc', async () => {
    const user = userEvent.setup()
    renderGallery({ artwork: artworkPadrao() })

    const dialog = await abrirLightbox(user, 'Ver Pôster 1')
    expect(dialog).toHaveAttribute('aria-label', 'Pôster 1 de 3')

    // Seta direita avança dentro do tipo.
    await user.keyboard('{ArrowRight}')
    await waitFor(() =>
      expect(screen.getByRole('dialog')).toHaveAttribute(
        'aria-label',
        'Pôster 2 de 3',
      ),
    )
    // Seta esquerda volta.
    await user.keyboard('{ArrowLeft}')
    await waitFor(() =>
      expect(screen.getByRole('dialog')).toHaveAttribute(
        'aria-label',
        'Pôster 1 de 3',
      ),
    )
    // Esc fecha.
    await user.keyboard('{Escape}')
    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument(),
    )
  })

  it('(f2) botão "Arte anterior" navega (com wrap) dentro do tipo', async () => {
    const user = userEvent.setup()
    renderGallery({ artwork: artworkPadrao() })
    await abrirLightbox(user, 'Ver Pôster 1')
    // De 1/3, "anterior" dá a volta para 3/3.
    await user.click(screen.getByRole('button', { name: 'Arte anterior' }))
    await waitFor(() =>
      expect(screen.getByRole('dialog')).toHaveAttribute(
        'aria-label',
        'Pôster 3 de 3',
      ),
    )
  })

  it('(f3) Tab fica preso: do último focável volta ao primeiro', async () => {
    const user = userEvent.setup()
    renderGallery({ artwork: artworkPadrao() })
    const dialog = await abrirLightbox(user, 'Ver Pôster 1')
    const botoes = dialog.querySelectorAll<HTMLElement>('button')
    const primeiro = botoes[0]
    const ultimo = botoes[botoes.length - 1]
    // Com o foco no último, Tab (sem shift) devolve ao primeiro.
    ultimo.focus()
    fireEvent.keyDown(document, { key: 'Tab' })
    expect(document.activeElement).toBe(primeiro)
    // Com o foco no primeiro, Shift+Tab vai ao último.
    primeiro.focus()
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true })
    expect(document.activeElement).toBe(ultimo)
  })

  it('(f) botão próxima navega e o toggle de zoom alterna o rótulo', async () => {
    const user = userEvent.setup()
    renderGallery({ artwork: artworkPadrao() })
    await abrirLightbox(user, 'Ver Pôster 1')

    await user.click(screen.getByRole('button', { name: 'Próxima arte' }))
    await waitFor(() =>
      expect(screen.getByRole('dialog')).toHaveAttribute(
        'aria-label',
        'Pôster 2 de 3',
      ),
    )
    // Zoom: começa em "Ampliar para 100%"; ao clicar vira "Ajustar à tela".
    await user.click(screen.getByRole('button', { name: 'Ampliar para 100%' }))
    expect(
      screen.getByRole('button', { name: 'Ajustar à tela' }),
    ).toBeInTheDocument()
  })
})

describe('ArtGallery — lightbox: metadados e backdrop (0049)', () => {
  it('(m) mostra idioma, likes e origem da arte de inventário', async () => {
    const user = userEvent.setup()
    renderGallery({ artwork: artworkPadrao() })
    await abrirLightbox(user, 'Ver Pôster 1')

    // Pôster 1 (inventário): lang 'en', likes 5, source fanarttv.
    expect(screen.getByText('5')).toBeInTheDocument()
    expect(screen.getByText('fanarttv')).toBeInTheDocument()
  })

  it('(n) disc textless: selo "sem texto", disc_type e temporada', async () => {
    const user = userEvent.setup()
    renderGallery({
      artwork: {
        disc: [
          item({
            source: 'fanarttv',
            lang: '',
            likes: 0,
            disc_type: 'bluray',
            season: '1',
          }),
        ],
        available: { disc: 1 },
      },
    })
    await abrirLightbox(user, 'Ver Disco 1')
    expect(screen.getByText('sem texto')).toBeInTheDocument()
    expect(screen.getByText('bluray')).toBeInTheDocument()
    expect(screen.getByText('Temporada 1')).toBeInTheDocument()
  })

  it('(o) principal: sem botão "Definir como principal", com selos no rodapé (0062)', async () => {
    const user = userEvent.setup()
    renderGallery({ artwork: artworkPadrao() })
    // Pôster 3 é o principal (e está salvo na pasta).
    await abrirLightbox(user, 'Ver Pôster 3 (principal)')
    expect(
      screen.queryByRole('button', { name: 'Definir como principal' }),
    ).toBeNull()
    expect(screen.queryByRole('button', { name: 'Salvar na pasta' })).toBeNull()
    expect(screen.getAllByText('principal').length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText('salvo na pasta').length).toBeGreaterThanOrEqual(
      1,
    )
  })

  it('(p) clique no backdrop fecha o lightbox', async () => {
    const user = userEvent.setup()
    renderGallery({ artwork: artworkPadrao() })
    const dialog = await abrirLightbox(user, 'Ver Pôster 1')
    // Clique direto no backdrop (o próprio nó do dialog) fecha.
    fireEvent.click(dialog)
    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument(),
    )
  })
})

describe('ArtGallery — ações roteadas (0049/0062)', () => {
  it('(g) definir principal de arte LOCAL do dono → PUT prefer direto com o asset_id', async () => {
    const user = userEvent.setup()
    const { espioes, invalidateSpy } = renderGallery({
      artwork: artworkPadrao(),
    })

    // Pôster 2 é o local do dono (asset_id p1), ainda não principal.
    await abrirLightbox(user, 'Ver Pôster 2')
    await user.click(
      screen.getByRole('button', { name: 'Definir como principal' }),
    )

    await waitFor(() => expect(espioes.onPrefer).toHaveBeenCalledWith('p1'))
    expect(espioes.onSelect).not.toHaveBeenCalled()
    // Invalida galeria + detalhe + grid.
    await waitFor(() => {
      const chaves = chavesInvalidadas(invalidateSpy.mock.calls)
      expect(chaves).toContain('["artwork","e1"]')
      expect(chaves).toContain('["entity","e1"]')
      expect(chaves).toContain('["entities"]')
    })
  })

  it('(h) definir principal de arte do INVENTÁRIO encadeia select (sem preferir) + prefer (0062 §2 passo 4)', async () => {
    const user = userEvent.setup()
    const { espioes } = renderGallery({ artwork: artworkPadrao() })

    // Pôster 1 é do inventário (fanarttv, sem asset_id ainda).
    await abrirLightbox(user, 'Ver Pôster 1')
    await user.click(
      screen.getByRole('button', { name: 'Definir como principal' }),
    )

    await waitFor(() =>
      expect(espioes.onSelect).toHaveBeenCalledWith({
        kind: 'poster',
        url: 'https://assets.fanart.tv/x.jpg',
        prefer: false,
      }),
    )
    // O select cria o asset 'novo' (mock); o clique único encadeia o prefer nele.
    await waitFor(() => expect(espioes.onPrefer).toHaveBeenCalledWith('novo'))
  })

  it('(i) salvar na pasta arte do INVENTÁRIO encadeia select + materialize', async () => {
    const user = userEvent.setup()
    const { espioes, invalidateSpy } = renderGallery({
      artwork: artworkPadrao(),
    })

    await abrirLightbox(user, 'Ver Pôster 1')
    await user.click(screen.getByRole('button', { name: 'Salvar na pasta' }))

    await waitFor(() =>
      expect(espioes.onSelect).toHaveBeenCalledWith({
        kind: 'poster',
        url: 'https://assets.fanart.tv/x.jpg',
        prefer: false,
      }),
    )
    await waitFor(() =>
      expect(espioes.onMaterialize).toHaveBeenCalledWith('novo'),
    )
    expect(espioes.onPrefer).not.toHaveBeenCalled()
    await waitFor(() => {
      const chaves = chavesInvalidadas(invalidateSpy.mock.calls)
      expect(chaves).toContain('["artwork","e1"]')
    })
  })

  it('(i2) salvar na pasta arte já local (com asset_id) → POST materialize direto', async () => {
    const user = userEvent.setup()
    const { espioes } = renderGallery({ artwork: artworkPadrao() })

    // Pôster 2 é local do dono (asset_id p1), ainda não salvo na pasta.
    await abrirLightbox(user, 'Ver Pôster 2')
    await user.click(screen.getByRole('button', { name: 'Salvar na pasta' }))

    await waitFor(() =>
      expect(espioes.onMaterialize).toHaveBeenCalledWith('p1'),
    )
    expect(espioes.onSelect).not.toHaveBeenCalled()
  })

  it('(i3) baixar pro cache do inventário → POST select {prefer:false} (0049, sem tocar a pasta real)', async () => {
    const user = userEvent.setup()
    const { espioes } = renderGallery({ artwork: artworkPadrao() })

    await abrirLightbox(user, 'Ver Pôster 1')
    await user.click(screen.getByRole('button', { name: 'Baixar pro cache' }))

    await waitFor(() =>
      expect(espioes.onSelect).toHaveBeenCalledWith({
        kind: 'poster',
        url: 'https://assets.fanart.tv/x.jpg',
        prefer: false,
      }),
    )
    expect(espioes.onMaterialize).not.toHaveBeenCalled()
  })

  it('(i4) sem media_file presente (owned=false): "Definir como principal"/"Salvar na pasta" ficam desabilitados', async () => {
    const user = userEvent.setup()
    renderGallery({ artwork: artworkPadrao(), owned: false })

    await abrirLightbox(user, 'Ver Pôster 1')
    expect(
      screen.getByRole('button', { name: 'Definir como principal' }),
    ).toBeDisabled()
    expect(
      screen.getByRole('button', { name: 'Salvar na pasta' }),
    ).toBeDisabled()
    // "Baixar pro cache" não depende de pasta na biblioteca — continua livre.
    expect(
      screen.getByRole('button', { name: 'Baixar pro cache' }),
    ).toBeEnabled()
  })

  it('(j) remover só aparece na arte do dono e chama DELETE do asset', async () => {
    const user = userEvent.setup()
    const { espioes } = renderGallery({ artwork: artworkPadrao() })

    // Pôster 1 (inventário) não tem "Remover".
    await abrirLightbox(user, 'Ver Pôster 1')
    expect(screen.queryByRole('button', { name: 'Remover arte' })).toBeNull()
    await user.keyboard('{Escape}')

    // Pôster 2 (do dono) tem "Remover".
    await abrirLightbox(user, 'Ver Pôster 2')
    await user.click(screen.getByRole('button', { name: 'Remover arte' }))
    await waitFor(() => expect(espioes.onDelete).toHaveBeenCalledWith('p1'))
  })
})

describe('ArtGallery — upload do dono (0017)', () => {
  it('(k) upload de um PNG chama uploadAsset com o kind e invalida as queries', async () => {
    const user = userEvent.setup()
    const { espioes, invalidateSpy } = renderGallery({
      artwork: { available: {} },
    })

    await user.selectOptions(screen.getByLabelText('Tipo de arte'), 'fanart')
    const file = new File(['x'], 'arte.png', { type: 'image/png' })
    await user.upload(screen.getByLabelText('Arquivo de arte'), file)

    await waitFor(() =>
      expect(espioes.onUpload).toHaveBeenCalledWith('fanart', 'arte.png'),
    )
    await waitFor(() => {
      const chaves = chavesInvalidadas(invalidateSpy.mock.calls)
      expect(chaves).toContain('["artwork","e1"]')
    })
  })

  it('(l) upload de não-imagem é bloqueado no cliente (erro inline)', async () => {
    const user = userEvent.setup({ applyAccept: false })
    const { espioes } = renderGallery({ artwork: { available: {} } })

    const file = new File(['x'], 'nota.txt', { type: 'text/plain' })
    await user.upload(screen.getByLabelText('Arquivo de arte'), file)

    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent(
        'Tipo de imagem nao suportado',
      ),
    )
    expect(espioes.onUpload).not.toHaveBeenCalled()
  })
})

describe('ArtGallery — lightbox reancorado por identidade (0072)', () => {
  it('(q) uma ação que reordena a lista mantém o lightbox no MESMO item', async () => {
    const user = userEvent.setup()
    const candidato = item({
      url: '/api/v1/images?src=https%3A%2F%2Fassets.fanart.tv%2Fcand.jpg',
      source: 'fanarttv',
      src_url: 'https://assets.fanart.tv/cand.jpg',
    })
    const local = item({
      url: '/api/v1/entities/e1/assets/p1/file',
      source: 'user',
      is_local: true,
      asset_id: 'p1',
      src_url: undefined,
      lang: undefined,
      likes: undefined,
    })
    // Ordem inicial: [local, candidato] — abre no candidato (2o card).
    let artworkAtual: ArtworkResponse = {
      poster: [local, candidato],
      available: { poster: 2 },
    }
    renderGalleryDinamica(async (url, init) => {
      const { path, method } = caminhoEMetodo(url, init)
      if (path === '/api/v1/entities/e1/artwork' && method === 'GET') {
        return okJson(artworkAtual)
      }
      if (path === '/api/v1/entities/e1/artwork/select' && method === 'POST') {
        return okJson(baseAsset({ id: 'novo', origin: 'external' }))
      }
      const prefer = path.match(
        /^\/api\/v1\/entities\/e1\/assets\/(.+)\/prefer$/,
      )
      if (prefer && method === 'PUT') {
        // Preferir o candidato (agora materializado como asset 'novo') o torna
        // principal — a ordenação (is_principal desc) o move para o INÍCIO da
        // lista: mesma arte, posição numérica diferente.
        artworkAtual = {
          poster: [
            {
              ...candidato,
              asset_id: 'novo',
              src_url: undefined,
              is_principal: true,
              is_local: true,
              is_preferred: true,
            },
            local,
          ],
          available: { poster: 2 },
        }
        return okJson(
          baseAsset({ id: prefer[1], preferred: true, is_principal: true }),
        )
      }
      throw new Error(`rota nao mockada: ${method} ${path}`)
    })

    const dialog = await abrirLightbox(user, 'Ver Pôster 2')
    expect(dialog).toHaveAttribute('aria-label', 'Pôster 2 de 2')

    await user.click(
      screen.getByRole('button', { name: 'Definir como principal' }),
    )

    // A lista reordenou (o candidato virou o 1o), mas o lightbox segue nele —
    // não no que sobrou na posição 2 (agora `local`).
    await waitFor(() =>
      expect(screen.getByRole('dialog')).toHaveAttribute(
        'aria-label',
        'Pôster 1 de 2',
      ),
    )
    expect(
      screen.getByRole('dialog').querySelector('img')?.getAttribute('src'),
    ).toBe(candidato.url)
  })

  it('(r) remover o item aberto no lightbox fecha o lightbox', async () => {
    const user = userEvent.setup()
    const local = item({
      url: '/api/v1/entities/e1/assets/p1/file',
      source: 'user',
      is_local: true,
      asset_id: 'p1',
      src_url: undefined,
      lang: undefined,
      likes: undefined,
    })
    let artworkAtual: ArtworkResponse = {
      poster: [local],
      available: { poster: 1 },
    }
    renderGalleryDinamica(async (url, init) => {
      const { path, method } = caminhoEMetodo(url, init)
      if (path === '/api/v1/entities/e1/artwork' && method === 'GET') {
        return okJson(artworkAtual)
      }
      const del = path.match(/^\/api\/v1\/entities\/e1\/assets\/(.+)$/)
      if (del && method === 'DELETE') {
        artworkAtual = { poster: [], available: { poster: 0 } }
        return okJson(baseAsset({ id: del[1] }))
      }
      throw new Error(`rota nao mockada: ${method} ${path}`)
    })

    await abrirLightbox(user, 'Ver Pôster 1')
    await user.click(screen.getByRole('button', { name: 'Remover arte' }))

    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument(),
    )
  })
})
