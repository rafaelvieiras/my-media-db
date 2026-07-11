import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { EntityDetailScreen } from '@/routes/entity-detail'
import { LanguageProvider } from '@/lib/language-context'
import type { ResolvedEntity } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import {
  baseEntity,
  okFields,
  okJson,
  settingsPadrao,
} from '@/test/entity-fixtures'

// Cobre a tela da entity quando é COLEÇÃO (0026): a ação "Excluir coleção" (duas
// etapas → purga → volta à biblioteca) e a ausência da seção Arquivos. `useNavigate`
// é spy (a exclusão navega para "/"). `Link` vira um <a> simples.
const spies = vi.hoisted(() => ({ navigate: vi.fn() }))

vi.mock('@tanstack/react-router', () => ({
  useParams: () => ({ id: 'e1' }),
  useNavigate: () => spies.navigate,
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
}))

// Roteia o fetch: settings, membros (vazio), assets (vazio), override (vazio), a
// entity resolvida e o DELETE da purga (por método).
function fetchRouter(entity: ResolvedEntity, onDelete: () => void) {
  return vi.fn(async (url: string, init?: RequestInit) => {
    const u = String(url)
    if (u === '/api/v1/settings/languages') {
      return okJson(settingsPadrao)
    }
    if (u === '/api/v1/schema/entity-fields') {
      return okFields()
    }
    if (u.startsWith('/api/v1/entities/e1/override')) {
      return okJson({})
    }
    if (u.startsWith('/api/v1/entities/e1/members')) {
      return okJson([])
    }
    if (u.startsWith('/api/v1/entities/e1/assets')) {
      return okJson([])
    }
    if (u === '/api/v1/entities/e1' && init?.method === 'DELETE') {
      onDelete()
      return okJson({ purged: 1 })
    }
    if (u.startsWith('/api/v1/entities/e1')) {
      return okJson(entity)
    }
    throw new Error(`rota nao mockada: ${u}`)
  })
}

function renderColecao(onDelete: () => void, over = {}) {
  const entity = baseEntity({
    type: 'collection',
    title: 'Minha coleção',
    ...over,
  })
  vi.stubGlobal('fetch', fetchRouter(entity, onDelete))
  return renderWithClient(
    <LanguageProvider>
      <EntityDetailScreen />
    </LanguageProvider>,
  )
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  vi.restoreAllMocks()
  spies.navigate.mockReset()
})

describe('EntityDetailScreen — coleção (0026)', () => {
  it('exclui a coleção em duas etapas, purga e volta à biblioteca', async () => {
    const onDelete = vi.fn()
    renderColecao(onDelete)
    const user = userEvent.setup()

    const abrir = await screen.findByRole('button', { name: 'Excluir coleção' })
    await user.click(abrir)
    // 2ª etapa: só o botão de confirmação dispara a purga.
    const confirmar = await screen.findByRole('button', {
      name: 'Excluir coleção?',
    })
    await user.click(confirmar)

    await waitFor(() => expect(onDelete).toHaveBeenCalledTimes(1))
    await waitFor(() =>
      expect(spies.navigate).toHaveBeenCalledWith({ to: '/' }),
    )
  })

  it('cancelar a exclusão não purga', async () => {
    const onDelete = vi.fn()
    renderColecao(onDelete)
    const user = userEvent.setup()

    await user.click(
      await screen.findByRole('button', { name: 'Excluir coleção' }),
    )
    await user.click(screen.getByRole('button', { name: 'Cancelar' }))

    expect(
      screen.queryByRole('button', { name: 'Excluir coleção?' }),
    ).toBeNull()
    expect(onDelete).not.toHaveBeenCalled()
  })

  it('coleção não mostra a seção de Arquivos', async () => {
    renderColecao(() => {})
    await screen.findByRole('button', { name: 'Excluir coleção' })
    expect(screen.queryByRole('heading', { name: 'Arquivos' })).toBeNull()
  })
})
