import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen } from '@testing-library/react'
import { ExternalIdsEditor } from '@/components/external-ids-editor'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson } from '@/test/detail-harness'
import { corpoEnviado, stubRota } from '@/test/stub-fetch'

// Tests the external identity editor: it shows the current ids, saves the map via
// PUT /entities/{id}/external-ids, adds/removes sources, and shows an inline error.

// putIds routes PUT /api/v1/entities/{id}/external-ids to `resp`.
const putIds = (resp: () => Response) =>
  stubRota((p, m) => /\/external-ids$/.test(p) && m === 'PUT', resp)

// stubIdsEEnrich serves BOTH routes of "Salvar e re-enriquecer": the PUT of the ids and the
// POST of the enrich, each with its own response. It returns the mock (to assert the order).
function stubIdsEEnrich(idsResp: () => Response, enrichResp: () => Response) {
  const m = vi.fn(async (url: string, init?: RequestInit) => {
    const u = new URL(String(url), 'http://localhost')
    const method = init?.method ?? 'GET'
    if (/\/external-ids$/.test(u.pathname) && method === 'PUT') return idsResp()
    if (/\/enrich$/.test(u.pathname) && method === 'POST') return enrichResp()
    throw new Error(`rota nao mockada: ${method} ${u.pathname}`)
  })
  vi.stubGlobal('fetch', m)
  return m
}

// salvarECorpo clicks "Salvar IDs", waits for the call and returns the `external_ids` sent.
async function salvarECorpo(m: ReturnType<typeof vi.fn>) {
  fireEvent.click(screen.getByText('Salvar IDs'))
  await vi.waitFor(() => expect(m).toHaveBeenCalled())
  return (corpoEnviado(m) as { external_ids: Record<string, string> })
    .external_ids
}

afterEach(() => vi.restoreAllMocks())

describe('ExternalIdsEditor', () => {
  it('mostra os ids externos atuais', () => {
    putIds(() => okJson({}, 200))
    renderWithClient(
      <ExternalIdsEditor
        id="e1"
        externalIds={{ tmdb: '603', imdb: 'tt0133093' }}
      />,
    )
    const fontes = screen
      .getAllByLabelText('Fonte do id externo')
      .map((i) => (i as HTMLInputElement).value)
    expect(fontes).toContain('tmdb')
    expect(fontes).toContain('imdb')
  })

  it('salva o mapa de ids via PUT', async () => {
    const m = putIds(() => okJson({ id: 'e1' }, 200))
    renderWithClient(
      <ExternalIdsEditor id="e1" externalIds={{ tmdb: '603' }} />,
    )
    expect(await salvarECorpo(m)).toEqual({ tmdb: '603' })
  })

  it('adiciona uma fonte nova e a inclui no mapa salvo', async () => {
    const m = putIds(() => okJson({ id: 'e1' }, 200))
    renderWithClient(<ExternalIdsEditor id="e1" externalIds={{}} />)
    fireEvent.click(screen.getByText('Adicionar fonte'))
    fireEvent.change(screen.getByLabelText('Fonte do id externo'), {
      target: { value: 'tmdb' },
    })
    fireEvent.change(screen.getByLabelText('Id na fonte tmdb'), {
      target: { value: '99' },
    })
    expect(await salvarECorpo(m)).toEqual({ tmdb: '99' })
  })

  it('remover deixa o mapa sem a fonte', async () => {
    const m = putIds(() => okJson({ id: 'e1' }, 200))
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    renderWithClient(
      <ExternalIdsEditor id="e1" externalIds={{ tmdb: '603' }} />,
    )
    fireEvent.click(screen.getByLabelText('Remover tmdb'))
    expect(await salvarECorpo(m)).toEqual({})
  })

  it('mostra erro inline quando o PUT falha', async () => {
    putIds(() => erroJson(404, 'not_found', 'entidade nao encontrada'))
    renderWithClient(
      <ExternalIdsEditor id="e1" externalIds={{ tmdb: '603' }} />,
    )
    fireEvent.click(screen.getByText('Salvar IDs'))
    expect(await screen.findByText('entidade nao encontrada')).toBeTruthy()
  })

  it('salvar e re-enriquecer grava os ids e DEPOIS enriquece', async () => {
    const m = stubIdsEEnrich(
      () => okJson({ id: 'e1' }, 200),
      () => okJson({ id: 'e1', title: 'Matrix' }, 200),
    )
    renderWithClient(
      <ExternalIdsEditor id="e1" externalIds={{ tmdb: '603' }} />,
    )
    fireEvent.click(screen.getByText('Salvar e re-enriquecer'))
    await vi.waitFor(() => expect(m).toHaveBeenCalledTimes(2))
    // Order: first the PUT of the ids, then the POST of the enrich (the enrich reads the saved id).
    expect(m.mock.calls[0][1]?.method).toBe('PUT')
    expect(m.mock.calls[1][1]?.method).toBe('POST')
  })

  it('re-enriquecer mostra erro da fonte inline (ids ja salvos)', async () => {
    stubIdsEEnrich(
      () => okJson({ id: 'e1' }, 200),
      () => erroJson(502, 'external_source_error', 'a fonte externa falhou'),
    )
    renderWithClient(
      <ExternalIdsEditor id="e1" externalIds={{ tmdb: '603' }} />,
    )
    fireEvent.click(screen.getByText('Salvar e re-enriquecer'))
    expect(await screen.findByText('a fonte externa falhou')).toBeTruthy()
  })
})

// "Leave without saving" guard: onDirtyChange reflects whether the current map
// diverges from the last loaded/saved one — only on the transition.
describe('ExternalIdsEditor — onDirtyChange (0073)', () => {
  it('reporta false ao montar (nada editado)', () => {
    const onDirtyChange = vi.fn()
    renderWithClient(
      <ExternalIdsEditor
        id="e1"
        externalIds={{ tmdb: '603' }}
        onDirtyChange={onDirtyChange}
      />,
    )
    expect(onDirtyChange).toHaveBeenCalledWith(false)
    expect(onDirtyChange).toHaveBeenCalledTimes(1)
  })

  it('reporta true ao adicionar uma fonte', () => {
    const onDirtyChange = vi.fn()
    renderWithClient(
      <ExternalIdsEditor
        id="e1"
        externalIds={{}}
        onDirtyChange={onDirtyChange}
      />,
    )
    fireEvent.click(screen.getByText('Adicionar fonte'))
    fireEvent.change(screen.getByLabelText('Fonte do id externo'), {
      target: { value: 'tmdb' },
    })
    expect(onDirtyChange).toHaveBeenLastCalledWith(true)
  })

  it('reporta true ao remover uma fonte existente', () => {
    const onDirtyChange = vi.fn()
    renderWithClient(
      <ExternalIdsEditor
        id="e1"
        externalIds={{ tmdb: '603' }}
        onDirtyChange={onDirtyChange}
      />,
    )
    fireEvent.click(screen.getByLabelText('Remover tmdb'))
    expect(onDirtyChange).toHaveBeenLastCalledWith(true)
  })

  it('volta a false depois de "Salvar IDs" com sucesso', async () => {
    const m = putIds(() => okJson({ id: 'e1' }, 200))
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const onDirtyChange = vi.fn()
    renderWithClient(
      <ExternalIdsEditor
        id="e1"
        externalIds={{ tmdb: '603' }}
        onDirtyChange={onDirtyChange}
      />,
    )
    fireEvent.click(screen.getByLabelText('Remover tmdb'))
    expect(onDirtyChange).toHaveBeenLastCalledWith(true)
    fireEvent.click(screen.getByText('Salvar IDs'))
    await vi.waitFor(() => expect(m).toHaveBeenCalled())
    await vi.waitFor(() =>
      expect(onDirtyChange).toHaveBeenLastCalledWith(false),
    )
  })
})

// "Empty it by accident" guard: saving an empty map is a full-replace
// PUT that wipes the identity of the entity — it should only confirm when it is
// a net removal (there was something saved before).
describe('ExternalIdsEditor — guard de mapa vazio (0098)', () => {
  it('pede confirmação ao remover a única fonte e salvar', async () => {
    const m = putIds(() => okJson({ id: 'e1' }, 200))
    const confirmar = vi.spyOn(window, 'confirm').mockReturnValue(true)
    renderWithClient(
      <ExternalIdsEditor id="e1" externalIds={{ tmdb: '603' }} />,
    )
    fireEvent.click(screen.getByLabelText('Remover tmdb'))
    expect(await salvarECorpo(m)).toEqual({})
    expect(confirmar).toHaveBeenCalledTimes(1)
  })

  it('cancelar a confirmação não envia o PUT', () => {
    const m = putIds(() => okJson({ id: 'e1' }, 200))
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    renderWithClient(
      <ExternalIdsEditor id="e1" externalIds={{ tmdb: '603' }} />,
    )
    fireEvent.click(screen.getByLabelText('Remover tmdb'))
    fireEvent.click(screen.getByText('Salvar IDs'))
    expect(m).not.toHaveBeenCalled()
  })

  it('cancelar a confirmação no "Salvar e re-enriquecer" não chama nenhuma rota', () => {
    const m = stubIdsEEnrich(
      () => okJson({ id: 'e1' }, 200),
      () => okJson({ id: 'e1' }, 200),
    )
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    renderWithClient(
      <ExternalIdsEditor id="e1" externalIds={{ tmdb: '603' }} />,
    )
    fireEvent.click(screen.getByLabelText('Remover tmdb'))
    fireEvent.click(screen.getByText('Salvar e re-enriquecer'))
    expect(m).not.toHaveBeenCalled()
  })

  it('salvar um mapa não-vazio não pede confirmação', async () => {
    const m = putIds(() => okJson({ id: 'e1' }, 200))
    const confirmar = vi.spyOn(window, 'confirm')
    renderWithClient(
      <ExternalIdsEditor id="e1" externalIds={{ tmdb: '603' }} />,
    )
    expect(await salvarECorpo(m)).toEqual({ tmdb: '603' })
    expect(confirmar).not.toHaveBeenCalled()
  })

  it('item sem ids desde o início: salvar vazio não pede confirmação', async () => {
    const m = putIds(() => okJson({ id: 'e1' }, 200))
    const confirmar = vi.spyOn(window, 'confirm')
    renderWithClient(<ExternalIdsEditor id="e1" externalIds={{}} />)
    expect(await salvarECorpo(m)).toEqual({})
    expect(confirmar).not.toHaveBeenCalled()
  })
})
