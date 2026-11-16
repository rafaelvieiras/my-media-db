import { http, HttpResponse } from 'msw'
import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { server } from '@/test/msw/server'
import {
  entidadeResolvidaPadrao,
  entidades,
  overridesPorEntidade,
} from '@/test/msw/handlers'
import { renderRota } from '@/test/route-tree-harness'

// Phase 3 (MSW + jsdom) — journey #6 of the checklist §7.1,
// CRITICAL (ADR-0012): re-enriching an entity with an overridden title and a tag
// removed by the owner cannot revert that investment — only the fields WITHOUT
// override reflect the new data from the source. It tests the real route tree: it fires
// "Salvar e re-enriquecer" in the "IDs de terceiros" subroute and checks the
// result by navigating to the "Informações básicas" subroute (the shared
// sidebar, `EntityEditShell`, keeps the two subroutes in the same tree).
describe('enrich não-destrutivo (Fase 3)', () => {
  it('preserva título editado e tag removida ao "Salvar e re-enriquecer"', async () => {
    const id = '7'

    // State BEFORE the re-enrich: the owner has already edited the title (override) and removed
    // the tag "Clássico" (the override only stores "Ficção" — the resolved value reflects the
    // removal).
    entidades.set(
      id,
      entidadeResolvidaPadrao({
        id,
        title: 'Título Editado pelo Dono',
        type: 'movie',
        description: 'Sinopse antiga da fonte.',
        tags: ['Ficção'],
        external_ids: { tmdb: '603' },
      }),
    )
    overridesPorEntidade.set(id, {
      title: { 'pt-BR': 'Título Editado pelo Dono' },
    })

    let enrichChamado = false

    server.use(
      // "Salvar e re-enriquecer" writes the ids (unchanged here) BEFORE
      // enriching — it returns the entity as it is.
      http.put(`/api/v1/entities/${id}/external-ids`, () =>
        HttpResponse.json(entidades.get(id)),
      ),
      // The enrich returns NEW data from the source (the synopsis changed), but the overridden
      // title and the removed tag stay in the resolved value — the backend NEVER
      // touches the override (ADR-0012). The mock represents exactly that guarantee:
      // the field WITHOUT override (description) changes; the ones with override do not.
      http.post(`/api/v1/entities/${id}/enrich`, () => {
        enrichChamado = true
        const atualizada = entidadeResolvidaPadrao({
          id,
          title: 'Título Editado pelo Dono',
          type: 'movie',
          description: 'Sinopse nova vinda da fonte (pós-enrich).',
          tags: ['Ficção'],
          external_ids: { tmdb: '603' },
        })
        entidades.set(id, atualizada)
        return HttpResponse.json(atualizada)
      }),
    )

    // It checks the "before" state in the "Informações básicas" section first.
    await renderRota(`/entities/${id}/edit`)

    const tituloInput = await screen.findByLabelText('Título')
    expect(tituloInput).toHaveValue('Título Editado pelo Dono')

    const secaoTags = screen.getByText('Tags').closest('div') as HTMLElement
    expect(within(secaoTags).getByText('Ficção')).toBeInTheDocument()
    expect(within(secaoTags).queryByText('Clássico')).not.toBeInTheDocument()

    const sinopseAntes = screen.getByLabelText('Sinopse')
    expect(sinopseAntes).toHaveAttribute(
      'placeholder',
      'Sinopse antiga da fonte.',
    )

    // It fires "Salvar e re-enriquecer" in the "IDs de terceiros" subroute —
    // same shared sidebar/header (`EntityEditShell`), navigation
    // through the real Link (no mock in this MSW suite).
    fireEvent.click(screen.getByRole('link', { name: 'IDs de terceiros' }))
    await screen.findByText('Salvar e re-enriquecer')

    fireEvent.click(screen.getByText('Salvar e re-enriquecer'))

    await waitFor(() => expect(enrichChamado).toBe(true))

    // It goes back to "Informações básicas" to check the result of the enrich.
    fireEvent.click(
      await screen.findByRole('link', { name: 'Informações básicas' }),
    )

    // New data from the source arrived (placeholder of the synopsis, field WITHOUT override).
    await waitFor(() => {
      expect(screen.getByLabelText('Sinopse')).toHaveAttribute(
        'placeholder',
        'Sinopse nova vinda da fonte (pós-enrich).',
      )
    })

    // Non-destructive: the edited title REMAINS and the removed tag does NOT come back.
    expect(screen.getByLabelText('Título')).toHaveValue(
      'Título Editado pelo Dono',
    )
    const secaoTagsDepois = screen
      .getByText('Tags')
      .closest('div') as HTMLElement
    expect(within(secaoTagsDepois).getByText('Ficção')).toBeInTheDocument()
    expect(
      within(secaoTagsDepois).queryByText('Clássico'),
    ).not.toBeInTheDocument()
  })
})
