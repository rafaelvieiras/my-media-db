import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { EntityDetailScreen } from '@/routes/entity-detail'
import { LanguageProvider } from '@/lib/language-context'
import type { ResolvedEntity } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import type { EntityCard, OverrideBlob } from '@/lib/api'
import { entityCardBase as card } from '@/test/detail-harness'
import {
  baseEntity,
  okFields,
  okJson,
  settingsPadrao,
} from '@/test/entity-fixtures'

// Mocka o router: `useParams` fixa o id da rota; `Link` vira um <a>.
vi.mock('@tanstack/react-router', () => ({
  useParams: () => ({ id: 'e1' }),
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
}))

// Roteia o fetch: settings, descritores (para o selo "editado" saber o que é
// traduzível, 0030), a entity resolvida (`?lang=`), os arquivos e o blob de
// override (vazio por padrão; alguns testes passam um blob para o selo "editado").
function fetchRouter(
  entity: ResolvedEntity,
  blob: OverrideBlob = {},
  membros: EntityCard[] = [],
) {
  return vi.fn(async (url: string) => {
    const u = String(url)
    if (u === '/api/v1/settings/languages') {
      return okJson(settingsPadrao)
    }
    if (u === '/api/v1/schema/entity-fields') {
      return okFields()
    }
    if (u.startsWith('/api/v1/entities/e1/override')) {
      return okJson(blob)
    }
    if (u.startsWith('/api/v1/entities/e1/files')) {
      return okJson({ items: [] })
    }
    if (u.startsWith('/api/v1/entities/e1/assets')) {
      return okJson([])
    }
    if (u.startsWith('/api/v1/entities/e1/members')) {
      return okJson(membros)
    }
    if (u.startsWith('/api/v1/entities/e1')) {
      return okJson(entity)
    }
    throw new Error(`rota nao mockada: ${u}`)
  })
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  vi.restoreAllMocks()
})

function renderDetail(
  entity: ResolvedEntity,
  blob?: OverrideBlob,
  membros?: EntityCard[],
) {
  vi.stubGlobal('fetch', fetchRouter(entity, blob, membros))
  return renderWithClient(
    <LanguageProvider>
      <EntityDetailScreen />
    </LanguageProvider>,
  )
}

describe('EntityDetailScreen — selo de fallback', () => {
  it('renderiza as tags como badges', async () => {
    renderDetail(
      baseEntity({
        tags: ['Ficção científica', 'Aventura'],
        resolved_languages: { description: 'pt-BR', tags: 'pt-BR' },
      }),
    )

    await waitFor(() => expect(screen.getByText('Dune')).toBeInTheDocument())
    expect(screen.getByText('Ficção científica')).toBeInTheDocument()
    expect(screen.getByText('Aventura')).toBeInTheDocument()
    // Idioma efetivo == lido (pt-BR): sem selo.
    expect(screen.queryByText(/^em /)).not.toBeInTheDocument()
  })

  it('mostra o selo quando a descricao cai no fallback (idioma != lido)', async () => {
    renderDetail(
      baseEntity({
        description: 'Paul Atreides arrives on Arrakis.',
        resolved_languages: { description: 'en' },
      }),
    )

    await waitFor(() => expect(screen.getByText('Dune')).toBeInTheDocument())
    // Lendo em pt-BR, mas a descricao veio em en → selo discreto.
    expect(screen.getByText('em Inglês')).toBeInTheDocument()
  })

  it('mostra o selo ao lado das tags quando elas caem no fallback', async () => {
    renderDetail(
      baseEntity({
        tags: ['Science Fiction'],
        resolved_languages: { tags: 'en' },
      }),
    )

    await waitFor(() => expect(screen.getByText('Dune')).toBeInTheDocument())
    expect(screen.getByText('Science Fiction')).toBeInTheDocument()
    expect(screen.getByText('em Inglês')).toBeInTheDocument()
  })
})

describe('EntityDetailScreen — selo "editado" e nota local', () => {
  it('mostra o selo quando o blob tem override no idioma atual', async () => {
    // title overridado em pt-BR (idioma corrente) → selo; en não contaria.
    renderDetail(baseEntity(), { title: { 'pt-BR': 'Duna' } })

    await waitFor(() => expect(screen.getByText('Dune')).toBeInTheDocument())
    expect(screen.getByText('editado')).toBeInTheDocument()
  })

  it('sem override no idioma atual não há selo "editado"', async () => {
    // Override só em en; lendo em pt-BR → nenhum selo.
    renderDetail(baseEntity(), { title: { en: 'Dune (EN)' } })

    await waitFor(() => expect(screen.getByText('Dune')).toBeInTheDocument())
    expect(screen.queryByText('editado')).not.toBeInTheDocument()
  })

  it('a nota local resolvida aparece no hero (distinta da nota da fonte)', async () => {
    renderDetail(baseEntity({ local_rating: 9.5, vote_average: 8.2 }))

    await waitFor(() => expect(screen.getByText('Dune')).toBeInTheDocument())
    expect(screen.getByLabelText('Sua nota: 9.5')).toBeInTheDocument()
    expect(screen.getByText('8.2')).toBeInTheDocument()
  })

  it('"Editar" é um link para a rota de edição da entity', async () => {
    renderDetail(baseEntity())

    await waitFor(() => expect(screen.getByText('Dune')).toBeInTheDocument())
    // No mock do Link, `to` vira o href; o botão usa asChild sobre o <Link>.
    const editar = screen.getByRole('link', { name: 'Editar' })
    expect(editar).toBeInTheDocument()
  })
})

describe('EntityDetailScreen — árvore de séries (0031)', () => {
  it('série (tv) lista as Temporadas e não mostra Arquivos', async () => {
    renderDetail(baseEntity({ type: 'tv', title: 'Os Simpsons' }), {}, [
      card({ id: 's1', title: 'Temporada 1', type: 'season' }),
      card({ id: 's2', title: 'Temporada 2', type: 'season' }),
    ])

    await waitFor(() =>
      expect(screen.getByText('Os Simpsons')).toBeInTheDocument(),
    )
    expect(
      screen.getByRole('heading', { name: 'Temporadas' }),
    ).toBeInTheDocument()
    expect(await screen.findByText('Temporada 1')).toBeInTheDocument()
    expect(screen.getByText('Temporada 2')).toBeInTheDocument()
    // Read-only (Fase 1): sem o add/remove editável da coleção.
    expect(screen.queryByText('Adicionar item')).toBeNull()
    // Série agrupa temporadas — sem arquivo próprio.
    expect(screen.queryByRole('heading', { name: 'Arquivos' })).toBeNull()
  })

  it('temporada (season) lista os Episódios numerados, sem Arquivos', async () => {
    renderDetail(baseEntity({ type: 'season', title: 'Temporada 1' }), {}, [
      card({ id: 'e1', title: 'Piloto', type: 'episode' }),
      card({ id: 'e2', title: 'Segundo', type: 'episode' }),
    ])

    await waitFor(() =>
      expect(
        screen.getByRole('heading', { name: 'Episódios' }),
      ).toBeInTheDocument(),
    )
    expect(await screen.findByText('Piloto')).toBeInTheDocument()
    expect(screen.getByText('Episódio 1')).toBeInTheDocument()
    expect(screen.getByText('Episódio 2')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Arquivos' })).toBeNull()
  })

  it('episódio (episode) mostra Arquivos e não é um nó pai', async () => {
    renderDetail(baseEntity({ type: 'episode', title: 'Piloto' }))

    await waitFor(() => expect(screen.getByText('Piloto')).toBeInTheDocument())
    expect(
      screen.getByRole('heading', { name: 'Arquivos' }),
    ).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Temporadas' })).toBeNull()
    expect(screen.queryByRole('heading', { name: 'Episódios' })).toBeNull()
  })
})
