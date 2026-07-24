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

// Fase 3 (MSW + jsdom, briefing 0068) — jornada #6 do checklist 0028 §7.1,
// CRÍTICA (ADR-0012): re-enriquecer uma entity com título overridado e uma tag
// removida pelo dono não pode reverter esse investimento — só os campos SEM
// override refletem o dado novo da fonte. Testa a árvore de rota real: dispara
// "Salvar e re-enriquecer" na subrota "IDs de terceiros" (0080) e confere o
// resultado navegando para a subrota "Informações básicas" (a sidebar
// compartilhada, `EntityEditShell`, mantém as duas subrotas na mesma árvore).
describe('enrich não-destrutivo (Fase 3)', () => {
  it('preserva título editado e tag removida ao "Salvar e re-enriquecer"', async () => {
    const id = '7'

    // Estado ANTES do re-enrich: o dono já editou o título (override) e removeu
    // a tag "Clássico" (o override só guarda "Ficção" — o resolvido reflete a
    // remoção).
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
      // "Salvar e re-enriquecer" grava os ids (inalterados aqui) ANTES de
      // enriquecer — devolve a entity como está.
      http.put(`/api/v1/entities/${id}/external-ids`, () =>
        HttpResponse.json(entidades.get(id)),
      ),
      // O enrich devolve dado NOVO da fonte (sinopse mudou), mas o título
      // overridado e a tag removida permanecem no resolvido — o backend NUNCA
      // toca override (ADR-0012). O mock representa exatamente essa garantia:
      // o campo SEM override (description) muda; os com override não.
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

    // Confere o estado "antes" na seção "Informações básicas" primeiro.
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

    // Dispara "Salvar e re-enriquecer" na subrota "IDs de terceiros" (0080) —
    // mesma sidebar/cabeçalho compartilhados (`EntityEditShell`), navegação
    // pelo Link real (sem mock nesta suíte MSW).
    fireEvent.click(screen.getByRole('link', { name: 'IDs de terceiros' }))
    await screen.findByText('Salvar e re-enriquecer')

    fireEvent.click(screen.getByText('Salvar e re-enriquecer'))

    await waitFor(() => expect(enrichChamado).toBe(true))

    // Volta para "Informações básicas" para conferir o resultado do enrich.
    fireEvent.click(
      await screen.findByRole('link', { name: 'Informações básicas' }),
    )

    // Dado novo da fonte chegou (placeholder da sinopse, campo SEM override).
    await waitFor(() => {
      expect(screen.getByLabelText('Sinopse')).toHaveAttribute(
        'placeholder',
        'Sinopse nova vinda da fonte (pós-enrich).',
      )
    })

    // Não-destrutivo: título editado PERMANECE e a tag removida NÃO volta.
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
