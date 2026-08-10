import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { EntityDetailScreen } from '@/routes/entity-detail'
import { LanguageProvider } from '@/lib/language-context'
import type { ResolvedEntity } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import {
  baseEntity,
  okFields,
  okJson,
  respostaHistoricoVazia,
  settingsPadrao,
} from '@/test/entity-fixtures'

// Cobre a tela da entity quando é COLEÇÃO (0026): a ação "Excluir coleção" —
// desde o 0086, um item DESTRUTIVO dentro do dropdown "Gerenciar", que abre um
// AlertDialog de confirmação em vez de mutar direto (purga → volta à
// biblioteca) — e a ausência da seção Arquivos. `useNavigate` é spy (a
// exclusão navega para "/"). `Link` usa o mock compartilhado (jscpd — o
// `{...rest}` preserva `role`/demais props que o Radix `asChild` clona no
// filho).
const spies = vi.hoisted(() => ({ navigate: vi.fn() }))

vi.mock('@tanstack/react-router', async () => ({
  useParams: () => ({ id: 'e1' }),
  useSearch: () => ({ ordering: undefined }),
  useNavigate: () => spies.navigate,
  Link: (await import('@/test/link-mock')).LinkMock,
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
    // Histórico de consumo (0092/0094): estado/eventos vazios por padrão (sem
    // selo "Assistido" nem seção "Histórico" — este arquivo não exercita o
    // histórico em si).
    const historico = respostaHistoricoVazia(u)
    if (historico) {
      return historico
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

// abrirMenuEItemExcluir abre o dropdown "Gerenciar" e clica no item
// destrutivo "Excluir coleção" — que abre o AlertDialog de confirmação
// (NÃO purga direto, briefing 0086).
async function abrirMenuEItemExcluir(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole('button', { name: /Gerenciar/ }))
  await user.click(screen.getByRole('menuitem', { name: 'Excluir coleção' }))
  await screen.findByRole('alertdialog', { name: 'Excluir coleção?' })
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  vi.restoreAllMocks()
  spies.navigate.mockReset()
})

describe('EntityDetailScreen — coleção (0026/0086)', () => {
  it('exclui a coleção via AlertDialog, purga e volta à biblioteca', async () => {
    const onDelete = vi.fn()
    renderColecao(onDelete)
    const user = userEvent.setup()

    await abrirMenuEItemExcluir(user)
    // Confirma no AlertDialog: só o botão de ação dispara a purga.
    await user.click(screen.getByRole('button', { name: 'Excluir coleção' }))

    await waitFor(() => expect(onDelete).toHaveBeenCalledTimes(1))
    await waitFor(() =>
      expect(spies.navigate).toHaveBeenCalledWith({ to: '/' }),
    )
  })

  it('cancelar a exclusão fecha o diálogo sem purgar', async () => {
    const onDelete = vi.fn()
    renderColecao(onDelete)
    const user = userEvent.setup()

    await abrirMenuEItemExcluir(user)
    await user.click(screen.getByRole('button', { name: 'Cancelar' }))

    await waitFor(() => {
      expect(screen.queryByRole('alertdialog')).toBeNull()
    })
    expect(onDelete).not.toHaveBeenCalled()
    expect(spies.navigate).not.toHaveBeenCalled()
  })

  it('coleção não mostra a seção de Arquivos', async () => {
    renderColecao(() => {})
    await screen.findByRole('button', { name: /Gerenciar/ })
    expect(screen.queryByRole('heading', { name: 'Arquivos' })).toBeNull()
  })
})
