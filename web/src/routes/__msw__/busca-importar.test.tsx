import { http, HttpResponse } from 'msw'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { server } from '@/test/msw/server'
import { entidades, entidadeResolvidaPadrao } from '@/test/msw/handlers'
import { renderRota } from '@/test/route-tree-harness'
import type { SearchResult } from '@/lib/api'

// Phase 3 (MSW + jsdom) — journey #1/#2 of the checklist §7.1 in
// miniature, a real MULTI-SCREEN flow (search → import → navigate to the detail), on the
// REAL route tree — something an isolated component test (Vitest +
// stub-fetch.ts) cannot reach.
describe('busca → importar (Fase 3)', () => {
  it('busca externa, importa e navega ao detalhe do item importado', async () => {
    const resultadoBusca: SearchResult = {
      query: 'duna',
      local: [],
      external: [
        {
          source: 'tmdb',
          source_id: '438631',
          type: 'movie',
          title: 'Duna',
          year: '2021',
          overview: 'Um jovem enfrenta seu destino num planeta hostil.',
          poster_url: null,
          saved: false,
        },
      ],
      external_available: true,
    }

    // The import creates the entity id=42 — seeded in `entidades` so the GET of the detail
    // (after navigation) finds it.
    entidades.set(
      '42',
      entidadeResolvidaPadrao({ id: '42', title: 'Duna', type: 'movie' }),
    )

    server.use(
      http.get('/api/v1/search', () => HttpResponse.json(resultadoBusca)),
      http.post('/api/v1/import', () =>
        HttpResponse.json(entidades.get('42'), { status: 201 }),
      ),
    )

    await renderRota('/search')

    const campo = screen.getByRole('searchbox', { name: 'Buscar título' })
    fireEvent.change(campo, { target: { value: 'duna' } })

    // Real 350ms debounce (search-panel.tsx) before the GET /search fires.
    const resultado = await screen.findByText('Duna', undefined, {
      timeout: 2000,
    })
    expect(resultado).toBeInTheDocument()

    const botaoImportar = screen.getByRole('button', { name: /Importar/ })
    fireEvent.click(botaoImportar)

    // It navigated to /entities/42 — the resolved title appears in the detail.
    await waitFor(() => {
      expect(
        screen.getByRole('heading', { level: 1, name: 'Duna' }),
      ).toBeInTheDocument()
    })
  })
})
