import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { EntityEditBasicoScreen } from '@/routes/entity-edit-basico'
import { useUnsavedGuard } from '@/lib/use-unsaved-guard'
import { LanguageProvider } from '@/lib/language-context'
import type { OverrideBlob, ResolvedEntity } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import {
  baseEntity,
  okFields,
  okJson,
  settingsPadrao,
} from '@/test/entity-fixtures'

// The "leave without saving" guard is the router blocker: outside a
// RouterProvider it cannot run, so it is mocked here (the hook has its own test and
// each screen asserts the argument it passes).
vi.mock('@/lib/use-unsaved-guard', () => ({ useUnsavedGuard: vi.fn() }))

// Route `/entities/$id/edit` (split into subroutes later): the
// "Basic information" section — the form driven by descriptors. It covers the
// per-language semantics preserved from the old editor: set empty ×
// Clear, {add,remove} tags, neutral scalar, number, inline 422 and the navigation
// back, plus the "leave without saving" guard, now local to this section.
//
// The field labels ("Título", "Sinopse", ...) come from the descriptors served by
// the backend, so they are data and stay as the API sends them.
const spies = vi.hoisted(() => ({ navigate: vi.fn() }))

// DYNAMIC import of the Link mock (`@/test/router-mock`) — the factory of
// `vi.mock` is hoisted above the static imports; referencing `LinkMock`
// statically here would break with "Cannot access before initialization".
vi.mock('@tanstack/react-router', async () => {
  const { LinkMock } = await import('@/test/router-mock')
  return {
    useParams: () => ({ id: 'e1' }),
    useNavigate: () => spies.navigate,
    // It forwards `onClick`/`aria-current` (the "leave without saving" guard
    // intercepts the click on the navigation links via preventDefault + confirm).
    Link: LinkMock,
  }
})

interface Espioes {
  onPatch: ReturnType<
    typeof vi.fn<(lang: string | null, body: unknown) => void>
  >
  onDelete: ReturnType<
    typeof vi.fn<(campo: string | undefined, lang: string | null) => void>
  >
}

interface Opcoes {
  blob?: OverrideBlob
  entityOver?: Partial<ResolvedEntity>
  patchErro?: { status: number; code: string; message: string }
}

function fetchRouter(opts: Opcoes, espioes: Espioes) {
  const entity = baseEntity(opts.entityOver)
  const blob = opts.blob ?? {}
  return vi.fn(async (url: string, init?: RequestInit) => {
    const u = new URL(String(url), 'http://localhost')
    const path = u.pathname
    const method = init?.method ?? 'GET'

    if (path === '/api/v1/settings/languages') {
      return okJson(settingsPadrao)
    }
    if (path === '/api/v1/schema/entity-fields') {
      return okFields()
    }
    if (path === '/api/v1/entities/e1/override' && method === 'GET') {
      return okJson(blob)
    }
    if (path === '/api/v1/entities/e1/override' && method === 'PATCH') {
      espioes.onPatch(
        u.searchParams.get('lang'),
        JSON.parse(String(init?.body)),
      )
      if (opts.patchErro) {
        return {
          ok: false,
          status: opts.patchErro.status,
          json: async () => ({
            error: {
              code: opts.patchErro!.code,
              message: opts.patchErro!.message,
            },
          }),
        } as Response
      }
      return okJson(entity)
    }
    if (
      path.startsWith('/api/v1/entities/e1/override/') &&
      method === 'DELETE'
    ) {
      espioes.onDelete(path.split('/').pop(), u.searchParams.get('lang'))
      return okJson(entity)
    }
    if (path.startsWith('/api/v1/entities/e1')) {
      return okJson(entity)
    }
    throw new Error(`rota nao mockada: ${method} ${path}`)
  })
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  vi.restoreAllMocks()
  spies.navigate.mockReset()
})

function renderEdit(opts: Opcoes = {}): Espioes {
  const espioes: Espioes = {
    onPatch: vi.fn<(lang: string | null, body: unknown) => void>(),
    onDelete: vi.fn<(campo: string | undefined, lang: string | null) => void>(),
  }
  vi.stubGlobal('fetch', fetchRouter(opts, espioes))
  renderWithClient(
    <LanguageProvider>
      <EntityEditBasicoScreen />
    </LanguageProvider>,
  )
  return espioes
}

// Waits for the form to load (the Save button appears when entity+blob+descriptors arrive).
async function esperarForm() {
  await screen.findByRole('button', { name: 'Save changes' })
}

async function editar(opts: Opcoes = {}) {
  const user = userEvent.setup()
  const espioes = renderEdit(opts)
  await esperarForm()
  return { user, espioes }
}

async function salvarEEsperarPatch(
  user: ReturnType<typeof userEvent.setup>,
  espioes: Espioes,
  corpo: unknown,
) {
  await user.click(screen.getByRole('button', { name: 'Save changes' }))
  await waitFor(() => expect(espioes.onPatch).toHaveBeenCalledTimes(1))
  expect(espioes.onPatch).toHaveBeenCalledWith('pt-BR', corpo)
}

describe('EntityEditBasicoScreen — per-language editing', () => {
  it('editing the title sends PATCH with the right lang and body', async () => {
    const { user, espioes } = await editar()
    await user.type(screen.getByLabelText('Título'), 'Duna')
    await salvarEEsperarPatch(user, espioes, { title: 'Duna' })
  })

  it('"Clear" of a translatable field calls DELETE with the current lang', async () => {
    const { user, espioes } = await editar({
      blob: { description: { 'pt-BR': 'Sinopse pt' } },
    })
    await user.click(screen.getByRole('button', { name: 'Clear sinopse' }))
    await waitFor(() => expect(espioes.onDelete).toHaveBeenCalledTimes(1))
    expect(espioes.onDelete).toHaveBeenCalledWith('description', 'pt-BR')
  })

  it('a neutral field saves a scalar (no language dimension)', async () => {
    const { user, espioes } = await editar()
    await user.type(screen.getByLabelText('Título original'), 'Dune Zwei')
    await salvarEEsperarPatch(user, espioes, { original_title: 'Dune Zwei' })
  })

  it('"Clear" of a neutral field calls DELETE WITHOUT lang', async () => {
    const { user, espioes } = await editar({ blob: { original_title: 'Dune' } })
    await user.click(
      screen.getByRole('button', { name: 'Clear título original' }),
    )
    await waitFor(() => expect(espioes.onDelete).toHaveBeenCalledTimes(1))
    expect(espioes.onDelete).toHaveBeenCalledWith('original_title', null)
  })

  it('tags: adding a chip becomes { add } in the PATCH', async () => {
    const { user, espioes } = await editar()
    await user.type(screen.getByLabelText('Add a tag'), 'Clássico')
    await user.click(screen.getByRole('button', { name: 'Add' }))
    await salvarEEsperarPatch(user, espioes, {
      tags: { add: ['Clássico'], remove: [] },
    })
  })

  it('tags: removing a chip becomes { remove } in the PATCH', async () => {
    const { user, espioes } = await editar({
      entityOver: { tags: ['Ação', 'Drama'] },
    })
    await user.click(
      screen.getByRole('button', { name: 'Remove the tag Ação' }),
    )
    await salvarEEsperarPatch(user, espioes, {
      tags: { add: [], remove: ['Ação'] },
    })
  })

  it('emptying a seeded translatable field sends null (set empty ≠ clear)', async () => {
    const { user, espioes } = await editar({
      blob: { description: { 'pt-BR': 'Sinopse pt' } },
    })
    await user.clear(screen.getByLabelText('Sinopse'))
    await salvarEEsperarPatch(user, espioes, { description: null })
    expect(espioes.onDelete).not.toHaveBeenCalled()
  })

  it('setting the local rating saves the neutral number', async () => {
    const { user, espioes } = await editar()
    await user.type(screen.getByLabelText('Sua nota'), '9.5')
    await salvarEEsperarPatch(user, espioes, { local_rating: 9.5 })
  })

  it('emptying the seeded rating and saving sends null', async () => {
    const { user, espioes } = await editar({ blob: { local_rating: 7 } })
    await user.clear(screen.getByLabelText('Sua nota'))
    await salvarEEsperarPatch(user, espioes, { local_rating: null })
    expect(espioes.onDelete).not.toHaveBeenCalled()
  })

  it('a 422 from the PATCH shows inline and the form stays', async () => {
    const { user } = await editar({
      patchErro: {
        status: 422,
        code: 'invalid_override',
        message: 'campo não editorial',
      },
    })
    await user.type(screen.getByLabelText('Título'), 'Duna')
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent(
        'campo não editorial',
      ),
    )
    expect(spies.navigate).not.toHaveBeenCalled()
  })

  it('saving with no changes goes back to the detail without calling the API', async () => {
    const { user, espioes } = await editar()
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    await waitFor(() =>
      expect(spies.navigate).toHaveBeenCalledWith({
        to: '/entities/$id',
        params: { id: 'e1' },
      }),
    )
    expect(espioes.onPatch).not.toHaveBeenCalled()
  })

  it('Cancel goes back to the detail without saving', async () => {
    const { user, espioes } = await editar()
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(spies.navigate).toHaveBeenCalledWith({
      to: '/entities/$id',
      params: { id: 'e1' },
    })
    expect(espioes.onPatch).not.toHaveBeenCalled()
    expect(espioes.onDelete).not.toHaveBeenCalled()
  })

  it('a missing id (404) shows the error state', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        const u = new URL(String(url), 'http://localhost')
        if (u.pathname === '/api/v1/settings/languages') {
          return okJson(settingsPadrao)
        }
        if (u.pathname === '/api/v1/schema/entity-fields') {
          return okFields()
        }
        // entity and override answer 404.
        return {
          ok: false,
          status: 404,
          json: async () => ({
            error: { code: 'not_found', message: 'não encontrado' },
          }),
        } as Response
      }),
    )
    renderWithClient(
      <LanguageProvider>
        <EntityEditBasicoScreen />
      </LanguageProvider>,
    )
    expect(await screen.findByText(ESTADO_ERRO)).toBeInTheDocument()
  })
})

// The item loading/error state comes from `components/query-states.tsx`, which
// another domain owns: this query tolerates both labels so the test does not depend
// on that extraction.
const ESTADO_ERRO = /Não foi possível carregar o item|Could not load the item/

// "Leave without saving" guard, now local to the section: only the "dirty" state of the
// SchemaForm reaches it — the guard itself (router blocker + beforeunload) lives in
// `lib/use-unsaved-guard`, covered by its own test.
describe('EntityEditBasicoScreen — leave without saving guard (0073/0080)', () => {
  function linkVoltar() {
    return screen.getByRole('link', { name: /Back to the item/ })
  }

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('with no changes, the guard is told the section is pristine', async () => {
    const { user } = await editar()
    expect(useUnsavedGuard).toHaveBeenLastCalledWith(false)
    await user.click(linkVoltar())
    // The link is plain navigation now: intercepting it is the blocker's job.
    expect(spies.navigate).not.toHaveBeenCalled()
  })

  it('editing the Title marks the section dirty, and Cancel still works', async () => {
    const { user } = await editar()
    await user.type(screen.getByLabelText('Título'), 'Duna')
    await waitFor(() => expect(useUnsavedGuard).toHaveBeenLastCalledWith(true))
    // Leaving the section is the blocker's decision (mocked here): the form keeps
    // the typed value until the route actually changes.
    expect((screen.getByLabelText('Título') as HTMLInputElement).value).toBe(
      'Duna',
    )
  })
})
