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
})
