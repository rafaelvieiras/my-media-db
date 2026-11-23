import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ArtGallery } from '@/components/art-gallery'
import type { ArtworkItem, ArtworkResponse } from '@/lib/api'
import { baseAsset, okJson } from '@/test/entity-fixtures'
import { chavesInvalidadas } from '@/test/harness'

// Artwork: the gallery consumes `GET /artwork` and renders ONE ROW per kind;
// the lightbox opens the artwork at high resolution with navigation and the actions (prefer/download/remove).
// The spies read WHAT the UI hit the API with (id of the prefer/delete, body of the select).

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

// Test artwork item, with occasional overrides. `is_local_in_library`/
// `is_principal` default `false` — override per case.
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

// Default artwork of the movie: poster with [inventory, local of the owner, local preferred] +
// a 16:9 fanart (to check the aspect per kind).
function artworkPadrao(): ArtworkResponse {
  return {
    poster: [
      item({ source: 'fanarttv', is_local: false, is_preferred: false }),
      item({
        url: '/api/v1/entities/e1/assets/p1/file',
        source: 'user',
        is_local: true,
        // Cached internally, but NOT saved in the real folder yet — it exercises
        // "Salvar na pasta" isolated from "Definir como principal".
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

// caminhoEMetodo extracts (path, method) from a mocked fetch call — a parse
// shared by every route mock of this file (jscpd).
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

// renderGalleryDinamica mounts the gallery with its OWN fetch mock (not the
// fixed fetchRouter of artworkPadrao()): used by the re-anchoring tests of the
// lightbox, whose GET /artwork has to answer DIFFERENTLY on each
// call (the list actually changes after the action).
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

// It opens the lightbox on the card `label` and waits for the dialog to appear.
async function abrirLightbox(
  user: ReturnType<typeof userEvent.setup>,
  label: string,
) {
  await user.click(await screen.findByRole('button', { name: label }))
  return screen.findByRole('dialog')
}

describe('ArtGallery — one row per kind (0049)', () => {
  it('(a) renders one row per kind, each card in the right aspect', async () => {
    renderGallery({ artwork: artworkPadrao() })

    // Poster in the 2:3 portrait frame; fanart in the 16:9 landscape frame — without distorting.
    const poster = (await screen.findByAltText('Poster 1')) as HTMLImageElement
    const fanart = screen.getByAltText('Fanart 1') as HTMLImageElement
    expect(poster.className).toContain('aspect-[2/3]')
    expect(fanart.className).toContain('aspect-video')
    // Kind label visible in each row (heading of the section).
    expect(
      screen.getByRole('heading', { name: 'Poster', level: 3 }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('heading', { name: 'Fanart', level: 3 }),
    ).toBeInTheDocument()
  })

  it('(b) marks the principal artwork (aria-label + badge, briefing 0062)', async () => {
    renderGallery({ artwork: artworkPadrao() })
    // The 3rd poster is the principal one: the button announces it as such.
    expect(
      await screen.findByRole('button', { name: 'View Poster 3 (principal)' }),
    ).toBeInTheDocument()
  })

  it('(c) empty state when there is no artwork', async () => {
    renderGallery({ artwork: { available: {} } })
    await waitFor(() =>
      expect(screen.getByText('No artwork available yet.')).toBeInTheDocument(),
    )
  })

  it('(d) episode with no still in the inventory: read-only provider card from stillUrl', async () => {
    const raw = 'https://artworks.thetvdb.com/s.jpg'
    renderGallery({
      type: 'episode',
      artwork: { available: {} },
      stillUrl: raw,
    })
    const img = (await screen.findByAltText(
      'Provider still',
    )) as HTMLImageElement
    expect(img.getAttribute('src')).toBe(
      `/api/v1/images?src=${encodeURIComponent(raw)}`,
    )
    expect(img.className).toContain('aspect-video')
  })
})

// Broken files: the gallery/lightbox used to draw the browser's broken-image icon AND the
// alt text ("Poster 1") over the cover — a loose number that said nothing about the artwork.
describe('ArtGallery — broken artwork falls back to a placeholder (03xx)', () => {
  it('a card whose image fails shows "Image unavailable" instead of the alt text', async () => {
    renderGallery({ artwork: artworkPadrao() })

    const poster = (await screen.findByAltText('Poster 1')) as HTMLImageElement
    fireEvent.error(poster)

    expect(screen.queryByAltText('Poster 1')).toBeNull()
    expect(screen.getByRole('img', { name: 'Poster 1' })).toHaveTextContent(
      'Image unavailable',
    )
    // The other rows are untouched.
    expect(screen.getByAltText('Fanart 1')).toBeInTheDocument()
  })

  it('the lightbox of a broken artwork shows the placeholder, keeping the caption', async () => {
    const user = userEvent.setup()
    renderGallery({ artwork: artworkPadrao() })
    await abrirLightbox(user, 'View Poster 1')

    fireEvent.error(
      screen.getAllByAltText('Poster — artwork 1')[0] as HTMLImageElement,
    )

    expect(
      screen.getByRole('img', { name: 'Poster — artwork 1' }),
    ).toHaveTextContent('Image unavailable')
    expect(screen.getByRole('dialog')).toHaveAccessibleName(/Poster 1 of/)
  })
})

describe('ArtGallery — lightbox: navigation and keyboard (0049)', () => {
  it('(e) opens on the card, navigates with the arrow keys and closes on Esc', async () => {
    const user = userEvent.setup()
    renderGallery({ artwork: artworkPadrao() })

    const dialog = await abrirLightbox(user, 'View Poster 1')
    expect(dialog).toHaveAttribute('aria-label', 'Poster 1 of 3')

    // The right arrow advances within the kind.
    await user.keyboard('{ArrowRight}')
    await waitFor(() =>
      expect(screen.getByRole('dialog')).toHaveAttribute(
        'aria-label',
        'Poster 2 of 3',
      ),
    )
    // The left arrow goes back.
    await user.keyboard('{ArrowLeft}')
    await waitFor(() =>
      expect(screen.getByRole('dialog')).toHaveAttribute(
        'aria-label',
        'Poster 1 of 3',
      ),
    )
    // Esc closes.
    await user.keyboard('{Escape}')
    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument(),
    )
  })

  it('(f2) the "Previous artwork" button navigates (with wrap) within the kind', async () => {
    const user = userEvent.setup()
    renderGallery({ artwork: artworkPadrao() })
    await abrirLightbox(user, 'View Poster 1')
    // From 1/3, "previous" wraps around to 3/3.
    await user.click(screen.getByRole('button', { name: 'Previous artwork' }))
    await waitFor(() =>
      expect(screen.getByRole('dialog')).toHaveAttribute(
        'aria-label',
        'Poster 3 of 3',
      ),
    )
  })

  it('(f3) Tab stays trapped: from the last focusable it goes back to the first', async () => {
    const user = userEvent.setup()
    renderGallery({ artwork: artworkPadrao() })
    const dialog = await abrirLightbox(user, 'View Poster 1')
    const botoes = dialog.querySelectorAll<HTMLElement>('button')
    const primeiro = botoes[0]
    const ultimo = botoes[botoes.length - 1]
    // With the focus on the last one, Tab (without shift) goes back to the first.
    ultimo.focus()
    fireEvent.keyDown(document, { key: 'Tab' })
    expect(document.activeElement).toBe(primeiro)
    // With the focus on the first one, Shift+Tab goes to the last.
    primeiro.focus()
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true })
    expect(document.activeElement).toBe(ultimo)
  })

  it('(f) the next button navigates and the zoom toggle swaps the label', async () => {
    const user = userEvent.setup()
    renderGallery({ artwork: artworkPadrao() })
    await abrirLightbox(user, 'View Poster 1')

    await user.click(screen.getByRole('button', { name: 'Next artwork' }))
    await waitFor(() =>
      expect(screen.getByRole('dialog')).toHaveAttribute(
        'aria-label',
        'Poster 2 of 3',
      ),
    )
    // Zoom: it starts at "Zoom to 100%"; on click it becomes "Fit to screen".
    await user.click(screen.getByRole('button', { name: 'Zoom to 100%' }))
    expect(
      screen.getByRole('button', { name: 'Fit to screen' }),
    ).toBeInTheDocument()
  })
})

describe('ArtGallery — lightbox: metadata and backdrop (0049)', () => {
  it('(m) shows the language, likes and origin of the inventory artwork', async () => {
    const user = userEvent.setup()
    renderGallery({ artwork: artworkPadrao() })
    await abrirLightbox(user, 'View Poster 1')

    // Poster 1 (inventory): lang 'en', likes 5, source fanarttv.
    expect(screen.getByText('5')).toBeInTheDocument()
    expect(screen.getByText('fanarttv')).toBeInTheDocument()
  })

  it('(n) textless disc: "no text" badge, disc_type and season', async () => {
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
    await abrirLightbox(user, 'View Disc 1')
    expect(screen.getByText('no text')).toBeInTheDocument()
    expect(screen.getByText('bluray')).toBeInTheDocument()
    expect(screen.getByText('Season 1')).toBeInTheDocument()
  })

  it('(o) principal: no "Set as principal" button, with the badges in the footer (0062)', async () => {
    const user = userEvent.setup()
    renderGallery({ artwork: artworkPadrao() })
    // Poster 3 is the principal one (and it is saved in the folder).
    await abrirLightbox(user, 'View Poster 3 (principal)')
    expect(
      screen.queryByRole('button', { name: 'Set as principal' }),
    ).toBeNull()
    expect(screen.queryByRole('button', { name: 'Save to folder' })).toBeNull()
    expect(screen.getAllByText('principal').length).toBeGreaterThanOrEqual(1)
    expect(
      screen.getAllByText('saved in the folder').length,
    ).toBeGreaterThanOrEqual(1)
  })

  it('(p) a click on the backdrop closes the lightbox', async () => {
    const user = userEvent.setup()
    renderGallery({ artwork: artworkPadrao() })
    const dialog = await abrirLightbox(user, 'View Poster 1')
    // A direct click on the backdrop (the dialog node itself) closes it.
    fireEvent.click(dialog)
    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument(),
    )
  })
})

describe('ArtGallery — routed actions (0049/0062)', () => {
  it('(g) set as principal on the owner LOCAL artwork → direct PUT prefer with the asset_id', async () => {
    const user = userEvent.setup()
    const { espioes, invalidateSpy } = renderGallery({
      artwork: artworkPadrao(),
    })

    // Poster 2 is the owner's local one (asset_id p1), not principal yet.
    await abrirLightbox(user, 'View Poster 2')
    await user.click(screen.getByRole('button', { name: 'Set as principal' }))

    await waitFor(() => expect(espioes.onPrefer).toHaveBeenCalledWith('p1'))
    expect(espioes.onSelect).not.toHaveBeenCalled()
    // It invalidates the gallery + the detail + the grid.
    await waitFor(() => {
      const chaves = chavesInvalidadas(invalidateSpy.mock.calls)
      expect(chaves).toContain('["artwork","e1"]')
      expect(chaves).toContain('["entity","e1"]')
      expect(chaves).toContain('["entities"]')
    })
  })

  it('(h) set as principal on INVENTORY artwork chains select (without preferring) + prefer (0062 §2 step 4)', async () => {
    const user = userEvent.setup()
    const { espioes } = renderGallery({ artwork: artworkPadrao() })

    // Poster 1 is from the inventory (fanarttv, without an asset_id yet).
    await abrirLightbox(user, 'View Poster 1')
    await user.click(screen.getByRole('button', { name: 'Set as principal' }))

    await waitFor(() =>
      expect(espioes.onSelect).toHaveBeenCalledWith({
        kind: 'poster',
        url: 'https://assets.fanart.tv/x.jpg',
        prefer: false,
      }),
    )
    // The select creates the asset 'novo' (mock); the single click chains the prefer on it.
    await waitFor(() => expect(espioes.onPrefer).toHaveBeenCalledWith('novo'))
  })

  it('(i) save to folder on INVENTORY artwork chains select + materialize', async () => {
    const user = userEvent.setup()
    const { espioes, invalidateSpy } = renderGallery({
      artwork: artworkPadrao(),
    })

    await abrirLightbox(user, 'View Poster 1')
    await user.click(screen.getByRole('button', { name: 'Save to folder' }))

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

  it('(i2) save to folder on already local artwork (with asset_id) → direct POST materialize', async () => {
    const user = userEvent.setup()
    const { espioes } = renderGallery({ artwork: artworkPadrao() })

    // Poster 2 is the owner's local one (asset_id p1), not saved in the folder yet.
    await abrirLightbox(user, 'View Poster 2')
    await user.click(screen.getByRole('button', { name: 'Save to folder' }))

    await waitFor(() =>
      expect(espioes.onMaterialize).toHaveBeenCalledWith('p1'),
    )
    expect(espioes.onSelect).not.toHaveBeenCalled()
  })

  it('(i3) download to cache on inventory artwork → POST select {prefer:false} (0049, without touching the real folder)', async () => {
    const user = userEvent.setup()
    const { espioes } = renderGallery({ artwork: artworkPadrao() })

    await abrirLightbox(user, 'View Poster 1')
    await user.click(screen.getByRole('button', { name: 'Download to cache' }))

    await waitFor(() =>
      expect(espioes.onSelect).toHaveBeenCalledWith({
        kind: 'poster',
        url: 'https://assets.fanart.tv/x.jpg',
        prefer: false,
      }),
    )
    expect(espioes.onMaterialize).not.toHaveBeenCalled()
  })

  it('(i4) without a present media_file (owned=false): "Set as principal"/"Save to folder" stay disabled', async () => {
    const user = userEvent.setup()
    renderGallery({ artwork: artworkPadrao(), owned: false })

    await abrirLightbox(user, 'View Poster 1')
    expect(
      screen.getByRole('button', { name: 'Set as principal' }),
    ).toBeDisabled()
    expect(
      screen.getByRole('button', { name: 'Save to folder' }),
    ).toBeDisabled()
    // "Download to cache" does not depend on a folder in the library — it stays free.
    expect(
      screen.getByRole('button', { name: 'Download to cache' }),
    ).toBeEnabled()
  })

  it('(j) remove only appears on the owner artwork and calls DELETE of the asset', async () => {
    const user = userEvent.setup()
    const { espioes } = renderGallery({ artwork: artworkPadrao() })

    // Poster 1 (inventory) has no "Remover".
    await abrirLightbox(user, 'View Poster 1')
    expect(screen.queryByRole('button', { name: 'Remove artwork' })).toBeNull()
    await user.keyboard('{Escape}')

    // Poster 2 (of the owner) has "Remove".
    await abrirLightbox(user, 'View Poster 2')
    await user.click(screen.getByRole('button', { name: 'Remove artwork' }))
    await waitFor(() => expect(espioes.onDelete).toHaveBeenCalledWith('p1'))
  })
})

describe('ArtGallery — owner upload (0017)', () => {
  it('(k) uploading a PNG calls uploadAsset with the kind and invalidates the queries', async () => {
    const user = userEvent.setup()
    const { espioes, invalidateSpy } = renderGallery({
      artwork: { available: {} },
    })

    await user.selectOptions(screen.getByLabelText('Artwork type'), 'fanart')
    const file = new File(['x'], 'arte.png', { type: 'image/png' })
    await user.upload(screen.getByLabelText('Artwork file'), file)

    await waitFor(() =>
      expect(espioes.onUpload).toHaveBeenCalledWith('fanart', 'arte.png'),
    )
    await waitFor(() => {
      const chaves = chavesInvalidadas(invalidateSpy.mock.calls)
      expect(chaves).toContain('["artwork","e1"]')
    })
  })

  it('(l) a non-image upload is blocked on the client (inline error)', async () => {
    const user = userEvent.setup({ applyAccept: false })
    const { espioes } = renderGallery({ artwork: { available: {} } })

    const file = new File(['x'], 'nota.txt', { type: 'text/plain' })
    await user.upload(screen.getByLabelText('Artwork file'), file)

    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent(
        'Unsupported image type',
      ),
    )
    expect(espioes.onUpload).not.toHaveBeenCalled()
  })
})

describe('ArtGallery — lightbox re-anchored by identity (0072)', () => {
  it('(q) an action that reorders the list keeps the lightbox on the SAME item', async () => {
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
    // Initial order: [local, candidato] — it opens on the candidate (2nd card).
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
        // Preferring the candidate (now materialized as the asset 'novo') makes it
        // principal — the ordering (is_principal desc) moves it to the START of the
        // list: same artwork, different numeric position.
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

    const dialog = await abrirLightbox(user, 'View Poster 2')
    expect(dialog).toHaveAttribute('aria-label', 'Poster 2 of 2')

    await user.click(screen.getByRole('button', { name: 'Set as principal' }))

    // The list reordered (the candidate became the 1st), but the lightbox stays on it —
    // not on what was left at position 2 (now `local`).
    await waitFor(() =>
      expect(screen.getByRole('dialog')).toHaveAttribute(
        'aria-label',
        'Poster 1 of 2',
      ),
    )
    expect(
      screen.getByRole('dialog').querySelector('img')?.getAttribute('src'),
    ).toBe(candidato.url)
  })

  it('(r) removing the item open in the lightbox closes the lightbox', async () => {
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

    await abrirLightbox(user, 'View Poster 1')
    await user.click(screen.getByRole('button', { name: 'Remove artwork' }))

    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument(),
    )
  })
})
