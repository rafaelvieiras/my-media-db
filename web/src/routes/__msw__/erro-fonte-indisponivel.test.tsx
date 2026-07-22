import { http, HttpResponse } from 'msw'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { server } from '@/test/msw/server'
import { entidadeResolvidaPadrao, entidades } from '@/test/msw/handlers'
import { renderRota } from '@/test/route-tree-harness'

// Fase 3 (MSW + jsdom, briefing 0068) — jornada #17 do checklist 0028 §7.1: a
// fonte externa falha (502) ao re-enriquecer — a UI mostra o erro INLINE, sem
// crash e sem perder a navegação (a tela de edição continua montada e usável).
describe('erro: fonte indisponível ao re-enriquecer (Fase 3)', () => {
  it('mostra mensagem de erro inline em vez de quebrar a tela', async () => {
    const id = '9'

    entidades.set(
      id,
      entidadeResolvidaPadrao({
        id,
        title: 'Arrival',
        type: 'movie',
        external_ids: { tmdb: '329865' },
      }),
    )

    server.use(
      http.put(`/api/v1/entities/${id}/external-ids`, () =>
        HttpResponse.json(entidades.get(id)),
      ),
      // Fonte indisponível: o backend responde 502 com o envelope padrão
      // `{error:{code,message}}` — `enrichEntity` (src/lib/api.ts) vira um
      // `ApiError` com essa `message`.
      http.post(`/api/v1/entities/${id}/enrich`, () =>
        HttpResponse.json(
          {
            error: {
              code: 'fonte_indisponivel',
              message: 'A fonte externa não respondeu. Tente novamente.',
            },
          },
          { status: 502 },
        ),
      ),
    )

    await renderRota(`/entities/${id}/edit`)

    await screen.findByLabelText('Título')

    fireEvent.click(screen.getByText('Salvar e re-enriquecer'))

    // Erro inline, não navegação perdida: a tela de edição segue montada.
    await waitFor(() => {
      expect(
        screen.getByText('A fonte externa não respondeu. Tente novamente.'),
      ).toBeInTheDocument()
    })
    expect(screen.getByText('Salvar e re-enriquecer')).toBeInTheDocument()
    expect(screen.getByLabelText('Título')).toBeInTheDocument()
  })
})
