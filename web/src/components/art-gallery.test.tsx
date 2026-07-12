import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ArtGallery } from '@/components/art-gallery'
import type { Asset } from '@/lib/api'
import { baseAsset, okJson } from '@/test/entity-fixtures'
import { chavesInvalidadas } from '@/test/harness'

// Espiões dos verbos de escrita da galeria — cada teste lê com que argumentos a
// UI bateu na API (kind do upload, id do prefer/delete).
interface Espioes {
  onUpload: ReturnType<typeof vi.fn<(kind: unknown, fileName: string) => void>>
  onPrefer: ReturnType<typeof vi.fn<(assetId: string) => void>>
  onDelete: ReturnType<typeof vi.fn<(assetId: string) => void>>
}

interface Opcoes {
  assets?: Asset[]
  uploadErro?: { status: number; code: string; message: string }
  // Tipo da entity (0044): default `movie`; `episode` habilita o still.
  type?: string
  // Still do provider (0044), passado como prop do detalhe.
  stillUrl?: string | null
}

// Envelope de uma resposta de erro do padrão `{ error: { code, message } }`.
function erroJson(status: number, code: string, message: string) {
  return {
    ok: false,
    status,
    json: async () => ({ error: { code, message } }),
  } as Response
}

// Roteia o fetch por método + caminho: GET da galeria, POST (upload multipart),
// PUT prefer, DELETE. O POST lê o FormData (campo `file` e `kind`).
function fetchRouter(opts: Opcoes, espioes: Espioes) {
  const assets = opts.assets ?? []
  return vi.fn(async (url: string, init?: RequestInit) => {
    const u = new URL(String(url), 'http://localhost')
    const path = u.pathname
    const method = init?.method ?? 'GET'

    if (path === '/api/v1/entities/e1/assets' && method === 'GET') {
      return okJson(assets)
    }
    if (path === '/api/v1/entities/e1/assets' && method === 'POST') {
      const form = init?.body as FormData
      const file = form.get('file') as File
      espioes.onUpload(form.get('kind'), file.name)
      if (opts.uploadErro) {
        return erroJson(
          opts.uploadErro.status,
          opts.uploadErro.code,
          opts.uploadErro.message,
        )
      }
      return okJson(baseAsset({ id: 'novo', origin: 'user' }))
    }
    const prefer = path.match(/^\/api\/v1\/entities\/e1\/assets\/(.+)\/prefer$/)
    if (prefer && method === 'PUT') {
      espioes.onPrefer(prefer[1])
      return okJson(baseAsset({ id: prefer[1], preferred: true }))
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

// Renderiza a galeria com um QueryClient próprio (retries off) e espiona o
// `invalidateQueries` para afirmar as invalidações pós-mutação.
function renderGallery(opts: Opcoes = {}) {
  const espioes: Espioes = {
    onUpload: vi.fn<(kind: unknown, fileName: string) => void>(),
    onPrefer: vi.fn<(assetId: string) => void>(),
    onDelete: vi.fn<(assetId: string) => void>(),
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
      />
    </QueryClientProvider>,
  )
  return { espioes, invalidateSpy }
}

describe('ArtGallery — galeria + upload (0017)', () => {
  it('(a) lista as artes com selos origin (sua/externa) e preferida', async () => {
    renderGallery({
      assets: [
        baseAsset({ id: 'a1', origin: 'user', preferred: true }),
        baseAsset({ id: 'a2', origin: 'external', preferred: false }),
      ],
    })

    await waitFor(() => expect(screen.getByText('sua')).toBeInTheDocument())
    expect(screen.getByText('externa')).toBeInTheDocument()
    expect(screen.getByText('preferida')).toBeInTheDocument()
  })

  it('(b) upload de um File chama uploadAsset com o kind e invalida as tres queries', async () => {
    const user = userEvent.setup()
    const { espioes, invalidateSpy } = renderGallery({ assets: [] })

    await waitFor(() =>
      expect(screen.getByText('Nenhuma arte ainda')).toBeInTheDocument(),
    )

    // Escolhe kind=fanart e envia um PNG.
    await user.selectOptions(screen.getByLabelText('Tipo de arte'), 'fanart')
    const file = new File(['x'], 'arte.png', { type: 'image/png' })
    await user.upload(screen.getByLabelText('Arquivo de arte'), file)

    await waitFor(() => expect(espioes.onUpload).toHaveBeenCalledTimes(1))
    expect(espioes.onUpload).toHaveBeenCalledWith('fanart', 'arte.png')

    // Invalida galeria + detalhe + grid (o poster reflete a nova precedencia).
    await waitFor(() => {
      const chaves = chavesInvalidadas(invalidateSpy.mock.calls)
      expect(chaves).toContain('["assets","e1"]')
      expect(chaves).toContain('["entity","e1"]')
      expect(chaves).toContain('["entities"]')
    })
  })

  it('(c) upload de arquivo nao-imagem e bloqueado no cliente (erro inline, sem chamar a API)', async () => {
    // `applyAccept: false` deixa o arquivo nao-imagem chegar ao handler apesar do
    // `accept` do input — simula o caminho (drag-drop / navegador) que ignora o
    // accept, que e justamente o que a validacao no cliente cobre.
    const user = userEvent.setup({ applyAccept: false })
    const { espioes } = renderGallery({ assets: [] })

    await waitFor(() =>
      expect(screen.getByText('Nenhuma arte ainda')).toBeInTheDocument(),
    )

    const file = new File(['x'], 'nota.txt', { type: 'text/plain' })
    await user.upload(screen.getByLabelText('Arquivo de arte'), file)

    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent(
        'Tipo de imagem nao suportado',
      ),
    )
    // Nao bateu na API (rejeitado antes do fetch de upload).
    expect(espioes.onUpload).not.toHaveBeenCalled()
  })

  it('(d) "Preferir" chama preferAsset com o id do asset', async () => {
    const user = userEvent.setup()
    const { espioes } = renderGallery({
      assets: [baseAsset({ id: 'a2', origin: 'external', preferred: false })],
    })

    await user.click(await screen.findByRole('button', { name: 'Preferir' }))

    await waitFor(() => expect(espioes.onPrefer).toHaveBeenCalledWith('a2'))
  })

  it('(e) "Remover" aparece so em origin=user e chama deleteAsset', async () => {
    const user = userEvent.setup()
    const { espioes } = renderGallery({
      assets: [
        baseAsset({ id: 'a1', origin: 'user' }),
        baseAsset({ id: 'a2', origin: 'external' }),
      ],
    })

    // So a do dono tem o botao Remover (uma unica ocorrencia).
    const remover = await screen.findAllByRole('button', {
      name: /Remover arte/,
    })
    expect(remover).toHaveLength(1)

    await user.click(remover[0])
    await waitFor(() => expect(espioes.onDelete).toHaveBeenCalledWith('a1'))
  })

  it('(f) erro 415 do servidor aparece inline', async () => {
    const user = userEvent.setup()
    renderGallery({
      assets: [],
      uploadErro: {
        status: 415,
        code: 'unsupported_media_type',
        message: 'Tipo de arquivo nao suportado pelo servidor.',
      },
    })

    await waitFor(() =>
      expect(screen.getByText('Nenhuma arte ainda')).toBeInTheDocument(),
    )

    const file = new File(['x'], 'arte.png', { type: 'image/png' })
    await user.upload(screen.getByLabelText('Arquivo de arte'), file)

    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent(
        'Tipo de arquivo nao suportado pelo servidor.',
      ),
    )
  })
})

describe('ArtGallery — enquadramento e still do episódio (0044)', () => {
  it('(g) miniatura por kind: pôster 2:3, fanart 16:9, com rótulo pt-BR', async () => {
    renderGallery({
      assets: [
        baseAsset({ id: 'p', kind: 'poster', origin: 'user' }),
        baseAsset({ id: 'f', kind: 'fanart', origin: 'external' }),
      ],
    })

    const poster = (await screen.findByAltText(
      'poster sua',
    )) as HTMLImageElement
    const fanart = screen.getByAltText('fanart externa') as HTMLImageElement
    // Pôster no frame retrato; fanart no frame paisagem (16:9) — sem deformar.
    expect(poster.className).toContain('aspect-[2/3]')
    expect(fanart.className).toContain('aspect-video')
    // Rótulo do kind visível em cada miniatura (o rótulo também aparece no seletor
    // de upload, daí `getAllByText`).
    expect(screen.getAllByText('Pôster').length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText('Fanart').length).toBeGreaterThanOrEqual(1)
  })

  it('(h) episódio: o upload oferece só o kind still (sem pôster/fanart)', async () => {
    renderGallery({ type: 'episode', assets: [] })

    await waitFor(() =>
      expect(
        screen.getByText('Envie um still para este episódio.'),
      ).toBeInTheDocument(),
    )
    // O seletor de tipo só tem "Still".
    expect(screen.getByRole('option', { name: 'Still' })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: 'Pôster' })).toBeNull()
    expect(screen.queryByRole('option', { name: 'Fanart' })).toBeNull()
  })

  it('(i) episódio sem still do dono: card "provider" read-only a partir do stillUrl', async () => {
    const raw = 'https://artworks.thetvdb.com/s.jpg'
    renderGallery({ type: 'episode', assets: [], stillUrl: raw })

    const img = (await screen.findByAltText(
      'Still do provider',
    )) as HTMLImageElement
    // Still cru do provider passa pelo image proxy (arteSrc).
    expect(img.getAttribute('src')).toBe(
      `/api/v1/images?src=${encodeURIComponent(raw)}`,
    )
    expect(img.className).toContain('aspect-video')
    expect(screen.getByText('provider')).toBeInTheDocument()
    // É a fonte: não deletável.
    expect(screen.queryByRole('button', { name: /Remover arte/ })).toBeNull()
  })

  it('(j) episódio com still do dono: o custom vence e o card "provider" some', async () => {
    renderGallery({
      type: 'episode',
      assets: [baseAsset({ id: 's1', kind: 'still', origin: 'user' })],
      stillUrl: 'https://artworks.thetvdb.com/s.jpg',
    })

    // O still do dono aparece (removível); o card do provider não é renderizado.
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: /Remover arte/ }),
      ).toBeInTheDocument(),
    )
    expect(screen.queryByAltText('Still do provider')).toBeNull()
  })
})
