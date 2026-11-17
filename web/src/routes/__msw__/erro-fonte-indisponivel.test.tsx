import { http, HttpResponse } from 'msw'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { server } from '@/test/msw/server'
import { entidadeResolvidaPadrao, entidades } from '@/test/msw/handlers'
import { renderRota } from '@/test/route-tree-harness'

// Phase 3 (MSW + jsdom) — journey #17 of the checklist §7.1: the
// external source fails (502) when re-enriching — the UI shows the error INLINE, without
// crashing and without losing navigation (the edit screen stays mounted and usable).
describe('error: source unavailable while re-enriching (Phase 3)', () => {
  it('shows an inline error message instead of breaking the screen', async () => {
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
      // Source unavailable: the backend answers 502 with the standard envelope
      // `{error:{code,message}}` — `enrichEntity` (src/lib/api.ts) becomes an
      // `ApiError` with that `message`.
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

    // "External IDs" subroute: it is where "Save and
    // re-enrich" now lives, isolated from the "Basic information" section.
    await renderRota(`/entities/${id}/edit/ids`)

    await screen.findByText('Save and re-enrich')

    fireEvent.click(screen.getByText('Save and re-enrich'))

    // Inline error, not a lost navigation: the edit screen stays mounted.
    await waitFor(() => {
      expect(
        screen.getByText('A fonte externa não respondeu. Tente novamente.'),
      ).toBeInTheDocument()
    })
    expect(screen.getByText('Save and re-enrich')).toBeInTheDocument()
    expect(screen.getByLabelText('External id source')).toBeInTheDocument()
  })
})
