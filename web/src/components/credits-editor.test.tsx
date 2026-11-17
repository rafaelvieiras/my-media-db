import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen } from '@testing-library/react'
import { CreditsEditor } from '@/components/credits-editor'
import type {
  CreditCast,
  CreditCompany,
  CreditCrew,
  OverrideBlob,
  ResolvedEntity,
} from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson } from '@/test/detail-harness'
import { corpoEnviado, stubRota } from '@/test/stub-fetch'

// Tests the credits editor: it reorders/removes/edits/adds
// cast/crew/production companies locally and builds the DELTA patch (only the keys of the kind
// that changed) via PATCH /entities/{id}/override.

function cast(over: Partial<CreditCast> = {}): CreditCast {
  return {
    source_id: '1',
    name: 'Ana',
    character: 'Protagonista',
    order: 0,
    ...over,
  }
}

function crew(over: Partial<CreditCrew> = {}): CreditCrew {
  return {
    source_id: '10',
    name: 'Dirigente',
    department: 'Directing',
    job: 'Director',
    ...over,
  }
}

function company(over: Partial<CreditCompany> = {}): CreditCompany {
  return { source_id: '100', name: 'StudioX', ...over }
}

// doisAtores is the two-person cast reused by the reordering tests
// (arrows and batch) — it avoids repeating the same Ana/Beto pair in
// every test.
function doisAtores(): CreditCast[] {
  return [
    cast({ source_id: '1', name: 'Ana' }),
    cast({ source_id: '2', name: 'Beto' }),
  ]
}

// renderEditor mounts the CreditsEditor with the given credits (the absent
// keys become `[]`) — it eliminates the repeated block of renderWithClient/fixed
// props (id/lang/blob) in every test.
function renderEditor(
  entity: Partial<Pick<ResolvedEntity, 'cast' | 'crew' | 'companies'>>,
  blob: OverrideBlob = {},
) {
  return renderWithClient(
    <CreditsEditor
      id="e1"
      lang="pt-BR"
      blob={blob}
      entity={{ cast: [], crew: [], companies: [], ...entity }}
    />,
  )
}

// patchOverride routes the PATCH /entities/{id}/override to `resp`.
const patchOverrideRota = (resp: () => Response) =>
  stubRota((p, m) => /\/override(\?|$)/.test(p) && m === 'PATCH', resp)

async function salvarECorpo(m: ReturnType<typeof vi.fn>) {
  fireEvent.click(screen.getByText('Save credits'))
  await vi.waitFor(() => expect(m).toHaveBeenCalled())
  return corpoEnviado(m) as { credits: Record<string, unknown> }
}

afterEach(() => vi.restoreAllMocks())

describe('CreditsEditor', () => {
  it('shows the current resolved credits', () => {
    patchOverrideRota(() => okJson({}, 200))
    renderEditor({
      cast: [cast({ source_id: '1', name: 'Ana', character: 'Heroína' })],
    })
    expect(screen.getByText('Ana')).toBeTruthy()
    expect(
      (screen.getByLabelText('Character of Ana') as HTMLInputElement).value,
    ).toBe('Heroína')
  })

  it('shows the empty messages when there are no credits', () => {
    renderEditor({})
    expect(screen.getByText('No cast yet.')).toBeTruthy()
    expect(screen.getByText('No crew yet.')).toBeTruthy()
    expect(screen.getByText('No production companies yet.')).toBeTruthy()
  })

  it('reordering the cast swaps the position and sends reorder in the patch', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({ cast: doisAtores() })
    fireEvent.click(screen.getAllByLabelText('Move down')[0])
    const corpo = await salvarECorpo(m)
    expect(corpo.credits.cast).toEqual({ reorder: ['2', '1'] })
  })

  it('batch reordering (move to position) sends the complete final reorder (briefing 0076)', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({
      cast: [...doisAtores(), cast({ source_id: '3', name: 'Carla' })],
    })
    // Carla (3rd position) goes to position 1 in a single step, without one click per
    // intermediate position.
    const campo = screen.getByLabelText('Move Carla to position (of 3)')
    fireEvent.change(campo, { target: { value: '1' } })
    fireEvent.blur(campo)
    const corpo = await salvarECorpo(m)
    expect(corpo.credits.cast).toEqual({ reorder: ['3', '1', '2'] })
  })

  it('moving to a position via Enter also applies the reordering', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({ cast: doisAtores() })
    const campo = screen.getByLabelText('Move Ana to position (of 2)')
    fireEvent.change(campo, { target: { value: '2' } })
    fireEvent.keyDown(campo, { key: 'Enter' })
    const corpo = await salvarECorpo(m)
    expect(corpo.credits.cast).toEqual({ reorder: ['2', '1'] })
  })

  it('moving to a position beyond the total is clamped to the end of the list', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({ cast: doisAtores() })
    const campo = screen.getByLabelText('Move Ana to position (of 2)')
    fireEvent.change(campo, { target: { value: '99' } })
    fireEvent.blur(campo)
    const corpo = await salvarECorpo(m)
    expect(corpo.credits.cast).toEqual({ reorder: ['2', '1'] })
  })

  it('removing from the cast sends remove in the patch', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({ cast: doisAtores() })
    fireEvent.click(screen.getByLabelText('Remove Ana from the cast'))
    const corpo = await salvarECorpo(m)
    expect(corpo.credits.cast).toEqual({ reorder: ['2'], remove: ['1'] })
  })

  it('editing the character sends edit in the patch', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({
      cast: [cast({ source_id: '1', name: 'Ana', character: 'Heroína' })],
    })
    fireEvent.change(screen.getByLabelText('Character of Ana'), {
      target: { value: 'Vilã' },
    })
    const corpo = await salvarECorpo(m)
    expect(corpo.credits.cast).toEqual({
      reorder: ['1'],
      edit: { '1': { character: 'Vilã' } },
    })
  })

  it('adding a new cast member sends add in the patch', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({})
    fireEvent.change(screen.getByLabelText('Name of the new cast member'), {
      target: { value: 'Carlos' },
    })
    fireEvent.change(
      screen.getByLabelText('Character of the new cast member'),
      {
        target: { value: 'Coadjuvante' },
      },
    )
    fireEvent.click(screen.getByText('Add to the cast'))
    const corpo = await salvarECorpo(m)
    const bloco = corpo.credits.cast as {
      reorder: string[]
      add: { source_id: string; name: string; character: string }[]
    }
    expect(bloco.add).toHaveLength(1)
    expect(bloco.add[0].name).toBe('Carlos')
    expect(bloco.add[0].character).toBe('Coadjuvante')
    expect(bloco.add[0].source_id.startsWith('custom-')).toBe(true)
    expect(bloco.reorder).toEqual([bloco.add[0].source_id])
  })

  it('nothing changed: saving does not hit the API', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({ cast: [cast({ source_id: '1', name: 'Ana' })] })
    fireEvent.click(screen.getByText('Save credits'))
    await new Promise((r) => setTimeout(r, 10))
    expect(m).not.toHaveBeenCalled()
  })

  it('clearing all credits calls DELETE /override/credits', async () => {
    const m = stubRota(
      (p, method) => /\/override\/credits$/.test(p) && method === 'DELETE',
      () => okJson({ id: 'e1' }, 200),
    )
    renderEditor({ cast: [cast({ source_id: '1', name: 'Ana' })] })
    fireEvent.click(screen.getByText('Clear all credits'))
    await vi.waitFor(() => expect(m).toHaveBeenCalled())
  })

  it('reordering the crew sends reorder in the patch (source_id|job key)', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({
      crew: [
        crew({ source_id: '10', job: 'Director' }),
        crew({
          source_id: '20',
          name: 'Produtor',
          department: 'Production',
          job: 'Producer',
        }),
      ],
    })
    fireEvent.click(screen.getAllByLabelText('Move down')[0])
    const corpo = await salvarECorpo(m)
    expect(corpo.credits.crew).toEqual({
      reorder: ['20|Producer', '10|Director'],
    })
  })

  it('editing the job/department of the crew sends edit in the patch', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({ crew: [crew()] })
    fireEvent.change(screen.getByLabelText('Job of Dirigente'), {
      target: { value: 'Diretor Geral' },
    })
    const corpo = await salvarECorpo(m)
    expect(corpo.credits.crew).toEqual({
      reorder: ['10|Director'],
      edit: { '10|Director': { job: 'Diretor Geral' } },
    })
  })

  it('removing from the crew sends remove in the patch', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({ crew: [crew()] })
    fireEvent.click(screen.getByLabelText('Remove Dirigente from the crew'))
    const corpo = await salvarECorpo(m)
    expect(corpo.credits.crew).toEqual({ reorder: [], remove: ['10|Director'] })
  })

  it('adding a new crew member sends add in the patch', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({})
    fireEvent.change(screen.getByLabelText('Name of the new crew member'), {
      target: { value: 'Fulano' },
    })
    fireEvent.change(
      screen.getByLabelText('Department of the new crew member'),
      {
        target: { value: 'Sound' },
      },
    )
    fireEvent.change(screen.getByLabelText('Job of the new crew member'), {
      target: { value: 'Mixagem' },
    })
    fireEvent.click(screen.getByText('Add to the crew'))
    const corpo = await salvarECorpo(m)
    const bloco = corpo.credits.crew as {
      add: {
        source_id: string
        name: string
        department: string
        job: string
      }[]
    }
    expect(bloco.add).toHaveLength(1)
    expect(bloco.add[0]).toMatchObject({
      name: 'Fulano',
      department: 'Sound',
      job: 'Mixagem',
    })
  })

  it('editing the name of a production company sends edit in the patch', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({ companies: [company()] })
    fireEvent.change(screen.getByLabelText('Production company name'), {
      target: { value: 'StudioX Renomeado' },
    })
    const corpo = await salvarECorpo(m)
    expect(corpo.credits.companies).toEqual({
      reorder: ['100'],
      edit: { '100': { name: 'StudioX Renomeado' } },
    })
  })

  it('removing a production company sends remove in the patch', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({ companies: [company()] })
    fireEvent.click(screen.getByLabelText('Remove StudioX'))
    const corpo = await salvarECorpo(m)
    expect(corpo.credits.companies).toEqual({ reorder: [], remove: ['100'] })
  })

  it('adding a new production company sends add in the patch', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({})
    fireEvent.change(
      screen.getByLabelText('Name of the new production company'),
      {
        target: { value: 'Estúdio Novo' },
      },
    )
    fireEvent.click(
      screen.getByRole('button', { name: 'Add production company' }),
    )
    const corpo = await salvarECorpo(m)
    const bloco = corpo.credits.companies as {
      add: { source_id: string; name: string }[]
    }
    expect(bloco.add).toHaveLength(1)
    expect(bloco.add[0].name).toBe('Estúdio Novo')
    expect(bloco.add[0].source_id.startsWith('custom-')).toBe(true)
  })

  it('shows an inline error when the PATCH fails', async () => {
    patchOverrideRota(() =>
      erroJson(422, 'invalid_patch', 'patch de creditos invalido'),
    )
    renderEditor({ cast: [cast({ source_id: '1', name: 'Ana' })] })
    fireEvent.change(screen.getByLabelText('Character of Ana'), {
      target: { value: 'Nova' },
    })
    fireEvent.click(screen.getByText('Save credits'))
    expect(await screen.findByText('patch de creditos invalido')).toBeTruthy()
  })
})

// "Leave without saving" guard: onDirtyChange reflects the SAME rule used
// to build the patch per kind (reorder/edit/remove/add) — only on the transition.
describe('CreditsEditor — onDirtyChange (0073)', () => {
  function renderEditorComOnDirtyChange(
    entity: Partial<Pick<ResolvedEntity, 'cast' | 'crew' | 'companies'>>,
    onDirtyChange: (sujo: boolean) => void,
  ) {
    return renderWithClient(
      <CreditsEditor
        id="e1"
        lang="pt-BR"
        blob={{}}
        entity={{ cast: [], crew: [], companies: [], ...entity }}
        onDirtyChange={onDirtyChange}
      />,
    )
  }

  it('reports false on mount (nothing edited)', () => {
    const onDirtyChange = vi.fn()
    renderEditorComOnDirtyChange(
      { cast: [cast({ source_id: '1', name: 'Ana' })] },
      onDirtyChange,
    )
    expect(onDirtyChange).toHaveBeenCalledWith(false)
    expect(onDirtyChange).toHaveBeenCalledTimes(1)
  })

  it('reports true when the cast is reordered', () => {
    const onDirtyChange = vi.fn()
    renderEditorComOnDirtyChange({ cast: doisAtores() }, onDirtyChange)
    fireEvent.click(screen.getAllByLabelText('Move down')[0])
    expect(onDirtyChange).toHaveBeenLastCalledWith(true)
  })

  it('reports true when batch reordering (move to position, briefing 0076)', () => {
    const onDirtyChange = vi.fn()
    renderEditorComOnDirtyChange({ cast: doisAtores() }, onDirtyChange)
    const campo = screen.getByLabelText('Move Ana to position (of 2)')
    fireEvent.change(campo, { target: { value: '2' } })
    fireEvent.blur(campo)
    expect(onDirtyChange).toHaveBeenLastCalledWith(true)
  })

  it('reports true when a crew field is edited', () => {
    const onDirtyChange = vi.fn()
    renderEditorComOnDirtyChange({ crew: [crew()] }, onDirtyChange)
    fireEvent.change(screen.getByLabelText('Job of Dirigente'), {
      target: { value: 'Diretor Geral' },
    })
    expect(onDirtyChange).toHaveBeenLastCalledWith(true)
  })

  it('reports true when a production company is removed', () => {
    const onDirtyChange = vi.fn()
    renderEditorComOnDirtyChange({ companies: [company()] }, onDirtyChange)
    fireEvent.click(screen.getByLabelText('Remove StudioX'))
    expect(onDirtyChange).toHaveBeenLastCalledWith(true)
  })

  it('reports true when a new cast member is added', () => {
    const onDirtyChange = vi.fn()
    renderEditorComOnDirtyChange({}, onDirtyChange)
    fireEvent.change(screen.getByLabelText('Name of the new cast member'), {
      target: { value: 'Carlos' },
    })
    fireEvent.click(screen.getByText('Add to the cast'))
    expect(onDirtyChange).toHaveBeenLastCalledWith(true)
  })
})
