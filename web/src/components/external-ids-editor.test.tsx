import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen } from '@testing-library/react'
import { ExternalIdsEditor } from '@/components/external-ids-editor'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson } from '@/test/detail-harness'
import { corpoEnviado, stubRota } from '@/test/stub-fetch'

// Testa o editor de identidade externa (0027): mostra os ids atuais, salva o mapa via
// PUT /entities/{id}/external-ids, adiciona/remove fontes, e mostra erro inline.

// putIds roteia o PUT /api/v1/entities/{id}/external-ids para `resp`.
const putIds = (resp: () => Response) =>
  stubRota((p, m) => /\/external-ids$/.test(p) && m === 'PUT', resp)

// stubIdsEEnrich atende AS DUAS rotas do "Salvar e re-enriquecer": o PUT dos ids e o
// POST do enrich, cada uma com sua resposta. Devolve o mock (para asserir a ordem).
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

// salvarECorpo clica em "Salvar IDs", espera a chamada e devolve o `external_ids` enviado.
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
    // Ordem: primeiro o PUT dos ids, depois o POST do enrich (o enrich le o id salvo).
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
