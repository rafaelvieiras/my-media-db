import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { EntityDetailScreen } from '@/routes/entity-detail'
import { LanguageProvider } from '@/lib/language-context'
import type { ResolvedEntity } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { baseEntity, okJson, settingsPadrao } from '@/test/entity-fixtures'

// Mocka o router: `useParams` fixa o id da rota; `Link` vira um <a>.
vi.mock('@tanstack/react-router', () => ({
  useParams: () => ({ id: 'e1' }),
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
}))

// Roteia o fetch: settings, a entity resolvida (`?lang=`), os arquivos e o blob
// de override (vazio — estes testes cobrem só o selo de fallback de leitura).
function fetchRouter(entity: ResolvedEntity) {
  return vi.fn(async (url: string) => {
    const u = String(url)
    if (u === '/api/v1/settings/languages') {
      return okJson(settingsPadrao)
    }
    if (u.startsWith('/api/v1/entities/e1/override')) {
      return okJson({})
    }
    if (u.startsWith('/api/v1/entities/e1/files')) {
      return okJson({ items: [] })
    }
    if (u.startsWith('/api/v1/entities/e1/assets')) {
      return okJson([])
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

function renderDetail(entity: ResolvedEntity) {
  vi.stubGlobal('fetch', fetchRouter(entity))
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
