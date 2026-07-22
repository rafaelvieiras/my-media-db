import { http, HttpResponse } from 'msw'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { server } from '@/test/msw/server'
import { entidades, entidadeResolvidaPadrao } from '@/test/msw/handlers'
import { renderRota } from '@/test/route-tree-harness'
import type { SearchResult } from '@/lib/api'

// Fase 3 (MSW + jsdom, briefing 0068) — jornada #1/#2 do checklist 0028 §7.1 em
// miniatura, MULTI-TELA de verdade (busca → import → navega ao detalhe), na
// árvore de rota REAL — o que um teste de componente isolado (Vitest +
// stub-fetch.ts) não alcança.
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

    // Import cria a entity id=42 — semeada em `entidades` para o GET do detalhe
    // (pós-navegação) encontrar.
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

    // Debounce real de 350ms (search-panel.tsx) antes do GET /search disparar.
    const resultado = await screen.findByText('Duna', undefined, {
      timeout: 2000,
    })
    expect(resultado).toBeInTheDocument()

    const botaoImportar = screen.getByRole('button', { name: /Importar/ })
    fireEvent.click(botaoImportar)

    // Navegou para /entities/42 — o título resolvido aparece no detalhe.
    await waitFor(() => {
      expect(
        screen.getByRole('heading', { level: 1, name: 'Duna' }),
      ).toBeInTheDocument()
    })
  })
})
