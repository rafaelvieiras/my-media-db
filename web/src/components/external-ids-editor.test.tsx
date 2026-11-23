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

// salvarECorpo clicks "Save IDs", waits for the call and returns the `external_ids` sent.
async function salvarECorpo(m: ReturnType<typeof vi.fn>) {
  fireEvent.click(screen.getByText('Save IDs'))
  await vi.waitFor(() => expect(m).toHaveBeenCalled())
  return (corpoEnviado(m) as { external_ids: Record<string, string> })
    .external_ids
}

// The two save buttons are disabled while nothing changed (a write with no change
// is noise): the tests that only want the PUT start by editing an id.
function editarId(valor: string) {
  fireEvent.change(screen.getByLabelText('Id in the source tmdb'), {
    target: { value: valor },
  })
}

// confirmarDialogo clicks the destructive action of the "empty map" AlertDialog.
function confirmarDialogo(rotulo = 'Remove the IDs') {
  fireEvent.click(screen.getByRole('button', { name: rotulo }))
}

afterEach(() => vi.restoreAllMocks())

describe('ExternalIdsEditor', () => {
  it('shows the current external ids', () => {
    putIds(() => okJson({}, 200))
    renderWithClient(
      <ExternalIdsEditor
        id="e1"
        externalIds={{ tmdb: '603', imdb: 'tt0133093' }}
      />,
    )
    const fontes = screen
      .getAllByLabelText('External id source')
      .map((i) => (i as HTMLInputElement).value)
    expect(fontes).toContain('tmdb')
    expect(fontes).toContain('imdb')
  })

  it('saves the map of ids via PUT', async () => {
    const m = putIds(() => okJson({ id: 'e1' }, 200))
    renderWithClient(
      <ExternalIdsEditor id="e1" externalIds={{ tmdb: '603' }} />,
    )
    editarId('604')
    expect(await salvarECorpo(m)).toEqual({ tmdb: '604' })
  })

  it('disables both save buttons while nothing changed', () => {
    putIds(() => okJson({}, 200))
    renderWithClient(
      <ExternalIdsEditor id="e1" externalIds={{ tmdb: '603' }} />,
    )
    expect(screen.getByRole('button', { name: 'Save IDs' })).toBeDisabled()
    expect(
      screen.getByRole('button', { name: 'Save and re-enrich' }),
    ).toBeDisabled()
    editarId('604')
    expect(screen.getByRole('button', { name: 'Save IDs' })).toBeEnabled()
  })

  it('adds a new source and includes it in the saved map', async () => {
    const m = putIds(() => okJson({ id: 'e1' }, 200))
    renderWithClient(<ExternalIdsEditor id="e1" externalIds={{}} />)
    fireEvent.click(screen.getByText('Add source'))
    fireEvent.change(screen.getByLabelText('External id source'), {
      target: { value: 'tmdb' },
    })
    fireEvent.change(screen.getByLabelText('Id in the source tmdb'), {
      target: { value: '99' },
    })
    expect(await salvarECorpo(m)).toEqual({ tmdb: '99' })
  })

  it('removing leaves the map without the source', async () => {
    const m = putIds(() => okJson({ id: 'e1' }, 200))
    renderWithClient(
      <ExternalIdsEditor id="e1" externalIds={{ tmdb: '603' }} />,
    )
    fireEvent.click(screen.getByLabelText('Remove tmdb'))
    fireEvent.click(screen.getByText('Save IDs'))
    // Net removal of everything: the AlertDialog confirms before the PUT.
    confirmarDialogo()
    await vi.waitFor(() => expect(m).toHaveBeenCalled())
    expect(
      (corpoEnviado(m) as { external_ids: Record<string, string> })
        .external_ids,
    ).toEqual({})
  })

  it('shows an inline error when the PUT fails', async () => {
    putIds(() => erroJson(404, 'not_found', 'entidade nao encontrada'))
    renderWithClient(
      <ExternalIdsEditor id="e1" externalIds={{ tmdb: '603' }} />,
    )
    editarId('604')
    fireEvent.click(screen.getByText('Save IDs'))
    expect(await screen.findByText('entidade nao encontrada')).toBeTruthy()
  })

  it('save and re-enrich writes the ids and THEN enriches', async () => {
    const m = stubIdsEEnrich(
      () => okJson({ id: 'e1' }, 200),
      () => okJson({ id: 'e1', title: 'Matrix' }, 200),
    )
    renderWithClient(
      <ExternalIdsEditor id="e1" externalIds={{ tmdb: '603' }} />,
    )
    editarId('604')
    fireEvent.click(screen.getByText('Save and re-enrich'))
    await vi.waitFor(() => expect(m).toHaveBeenCalledTimes(2))
    // Order: first the PUT of the ids, then the POST of the enrich (the enrich reads the saved id).
    expect(m.mock.calls[0][1]?.method).toBe('PUT')
    expect(m.mock.calls[1][1]?.method).toBe('POST')
  })

  it('re-enriching shows the source error inline (ids already saved)', async () => {
    stubIdsEEnrich(
      () => okJson({ id: 'e1' }, 200),
      () => erroJson(502, 'external_source_error', 'a fonte externa falhou'),
    )
    renderWithClient(
      <ExternalIdsEditor id="e1" externalIds={{ tmdb: '603' }} />,
    )
    editarId('604')
    fireEvent.click(screen.getByText('Save and re-enrich'))
    expect(await screen.findByText('a fonte externa falhou')).toBeTruthy()
  })
})

// "Leave without saving" guard: onDirtyChange reflects whether the current map
// diverges from the last loaded/saved one — only on the transition.
describe('ExternalIdsEditor — onDirtyChange (0073)', () => {
  it('reports false on mount (nothing edited)', () => {
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

  it('reports true when a source is added', () => {
    const onDirtyChange = vi.fn()
    renderWithClient(
      <ExternalIdsEditor
        id="e1"
        externalIds={{}}
        onDirtyChange={onDirtyChange}
      />,
    )
    fireEvent.click(screen.getByText('Add source'))
    fireEvent.change(screen.getByLabelText('External id source'), {
      target: { value: 'tmdb' },
    })
    expect(onDirtyChange).toHaveBeenLastCalledWith(true)
  })

  it('reports true when an existing source is removed', () => {
    const onDirtyChange = vi.fn()
    renderWithClient(
      <ExternalIdsEditor
        id="e1"
        externalIds={{ tmdb: '603' }}
        onDirtyChange={onDirtyChange}
      />,
    )
    fireEvent.click(screen.getByLabelText('Remove tmdb'))
    expect(onDirtyChange).toHaveBeenLastCalledWith(true)
  })

  it('goes back to false after a successful "Save IDs"', async () => {
    const m = putIds(() => okJson({ id: 'e1' }, 200))
    const onDirtyChange = vi.fn()
    renderWithClient(
      <ExternalIdsEditor
        id="e1"
        externalIds={{ tmdb: '603' }}
        onDirtyChange={onDirtyChange}
      />,
    )
    fireEvent.click(screen.getByLabelText('Remove tmdb'))
    expect(onDirtyChange).toHaveBeenLastCalledWith(true)
    fireEvent.click(screen.getByText('Save IDs'))
    confirmarDialogo()
    await vi.waitFor(() => expect(m).toHaveBeenCalled())
    await vi.waitFor(() =>
      expect(onDirtyChange).toHaveBeenLastCalledWith(false),
    )
  })
})

// "Empty it by accident" guard: saving an empty map is a full-replace
// PUT that wipes the identity of the entity — it should only confirm when it is
// a net removal (there was something saved before).
describe('ExternalIdsEditor — empty map guard (0098, now AlertDialog)', () => {
  it('asks for confirmation when the only source is removed and saved', async () => {
    const m = putIds(() => okJson({ id: 'e1' }, 200))
    renderWithClient(
      <ExternalIdsEditor id="e1" externalIds={{ tmdb: '603' }} />,
    )
    fireEvent.click(screen.getByLabelText('Remove tmdb'))
    fireEvent.click(screen.getByText('Save IDs'))
    // Nothing left the screen before the confirmation.
    expect(m).not.toHaveBeenCalled()
    expect(screen.getByRole('alertdialog')).toBeTruthy()
    confirmarDialogo()
    await vi.waitFor(() => expect(m).toHaveBeenCalled())
    expect(
      (corpoEnviado(m) as { external_ids: Record<string, string> })
        .external_ids,
    ).toEqual({})
  })

  it('cancelling the confirmation does not send the PUT', () => {
    const m = putIds(() => okJson({ id: 'e1' }, 200))
    renderWithClient(
      <ExternalIdsEditor id="e1" externalIds={{ tmdb: '603' }} />,
    )
    fireEvent.click(screen.getByLabelText('Remove tmdb'))
    fireEvent.click(screen.getByText('Save IDs'))
    confirmarDialogo('Cancel')
    expect(m).not.toHaveBeenCalled()
  })

  it('cancelling the confirmation in "Save and re-enrich" calls no route', () => {
    const m = stubIdsEEnrich(
      () => okJson({ id: 'e1' }, 200),
      () => okJson({ id: 'e1' }, 200),
    )
    renderWithClient(
      <ExternalIdsEditor id="e1" externalIds={{ tmdb: '603' }} />,
    )
    fireEvent.click(screen.getByLabelText('Remove tmdb'))
    fireEvent.click(screen.getByText('Save and re-enrich'))
    confirmarDialogo('Cancel')
    expect(m).not.toHaveBeenCalled()
  })

  it('saving a non-empty map does not ask for confirmation', async () => {
    const m = putIds(() => okJson({ id: 'e1' }, 200))
    renderWithClient(
      <ExternalIdsEditor id="e1" externalIds={{ tmdb: '603' }} />,
    )
    editarId('604')
    expect(await salvarECorpo(m)).toEqual({ tmdb: '604' })
    expect(screen.queryByRole('alertdialog')).toBeNull()
  })

  it('item without ids from the start: there is nothing to save yet', () => {
    putIds(() => okJson({ id: 'e1' }, 200))
    renderWithClient(<ExternalIdsEditor id="e1" externalIds={{}} />)
    expect(screen.getByRole('button', { name: 'Save IDs' })).toBeDisabled()
    expect(screen.queryByRole('alertdialog')).toBeNull()
  })
})
