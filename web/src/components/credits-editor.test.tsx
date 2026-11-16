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
  fireEvent.click(screen.getByText('Salvar créditos'))
  await vi.waitFor(() => expect(m).toHaveBeenCalled())
  return corpoEnviado(m) as { credits: Record<string, unknown> }
}

afterEach(() => vi.restoreAllMocks())

describe('CreditsEditor', () => {
  it('mostra os créditos resolvidos atuais', () => {
    patchOverrideRota(() => okJson({}, 200))
    renderEditor({
      cast: [cast({ source_id: '1', name: 'Ana', character: 'Heroína' })],
    })
    expect(screen.getByText('Ana')).toBeTruthy()
    expect(
      (screen.getByLabelText('Personagem de Ana') as HTMLInputElement).value,
    ).toBe('Heroína')
  })

  it('mostra mensagens vazias quando não há créditos', () => {
    renderEditor({})
    expect(screen.getByText('Sem elenco.')).toBeTruthy()
    expect(screen.getByText('Sem equipe.')).toBeTruthy()
    expect(screen.getByText('Sem produtoras.')).toBeTruthy()
  })

  it('reordenar o elenco troca a posição e manda reorder no patch', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({ cast: doisAtores() })
    fireEvent.click(screen.getAllByLabelText('Mover para baixo')[0])
    const corpo = await salvarECorpo(m)
    expect(corpo.credits.cast).toEqual({ reorder: ['2', '1'] })
  })

  it('reordenar em lote (mover para posição) manda o reorder final completo (briefing 0076)', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({
      cast: [...doisAtores(), cast({ source_id: '3', name: 'Carla' })],
    })
    // Carla (3rd position) goes to position 1 in a single step, without one click per
    // intermediate position.
    const campo = screen.getByLabelText('Mover Carla para a posição (de 3)')
    fireEvent.change(campo, { target: { value: '1' } })
    fireEvent.blur(campo)
    const corpo = await salvarECorpo(m)
    expect(corpo.credits.cast).toEqual({ reorder: ['3', '1', '2'] })
  })

  it('mover para posição via Enter também aplica a reordenação', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({ cast: doisAtores() })
    const campo = screen.getByLabelText('Mover Ana para a posição (de 2)')
    fireEvent.change(campo, { target: { value: '2' } })
    fireEvent.keyDown(campo, { key: 'Enter' })
    const corpo = await salvarECorpo(m)
    expect(corpo.credits.cast).toEqual({ reorder: ['2', '1'] })
  })

  it('mover para uma posição além do total grampeia no fim da lista', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({ cast: doisAtores() })
    const campo = screen.getByLabelText('Mover Ana para a posição (de 2)')
    fireEvent.change(campo, { target: { value: '99' } })
    fireEvent.blur(campo)
    const corpo = await salvarECorpo(m)
    expect(corpo.credits.cast).toEqual({ reorder: ['2', '1'] })
  })

  it('remover do elenco manda remove no patch', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({ cast: doisAtores() })
    fireEvent.click(screen.getByLabelText('Remover Ana do elenco'))
    const corpo = await salvarECorpo(m)
    expect(corpo.credits.cast).toEqual({ reorder: ['2'], remove: ['1'] })
  })

  it('editar o personagem manda edit no patch', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({
      cast: [cast({ source_id: '1', name: 'Ana', character: 'Heroína' })],
    })
    fireEvent.change(screen.getByLabelText('Personagem de Ana'), {
      target: { value: 'Vilã' },
    })
    const corpo = await salvarECorpo(m)
    expect(corpo.credits.cast).toEqual({
      reorder: ['1'],
      edit: { '1': { character: 'Vilã' } },
    })
  })

  it('adicionar um novo membro do elenco manda add no patch', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({})
    fireEvent.change(screen.getByLabelText('Nome do novo membro do elenco'), {
      target: { value: 'Carlos' },
    })
    fireEvent.change(
      screen.getByLabelText('Personagem do novo membro do elenco'),
      {
        target: { value: 'Coadjuvante' },
      },
    )
    fireEvent.click(screen.getByText('Adicionar ao elenco'))
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

  it('nada mudou: salvar não bate na API', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({ cast: [cast({ source_id: '1', name: 'Ana' })] })
    fireEvent.click(screen.getByText('Salvar créditos'))
    await new Promise((r) => setTimeout(r, 10))
    expect(m).not.toHaveBeenCalled()
  })

  it('limpar todos os créditos chama DELETE /override/credits', async () => {
    const m = stubRota(
      (p, method) => /\/override\/credits$/.test(p) && method === 'DELETE',
      () => okJson({ id: 'e1' }, 200),
    )
    renderEditor({ cast: [cast({ source_id: '1', name: 'Ana' })] })
    fireEvent.click(screen.getByText('Limpar todos os créditos'))
    await vi.waitFor(() => expect(m).toHaveBeenCalled())
  })

  it('reordenar a equipe manda reorder no patch (chave source_id|job)', async () => {
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
    fireEvent.click(screen.getAllByLabelText('Mover para baixo')[0])
    const corpo = await salvarECorpo(m)
    expect(corpo.credits.crew).toEqual({
      reorder: ['20|Producer', '10|Director'],
    })
  })

  it('editar cargo/departamento da equipe manda edit no patch', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({ crew: [crew()] })
    fireEvent.change(screen.getByLabelText('Cargo de Dirigente'), {
      target: { value: 'Diretor Geral' },
    })
    const corpo = await salvarECorpo(m)
    expect(corpo.credits.crew).toEqual({
      reorder: ['10|Director'],
      edit: { '10|Director': { job: 'Diretor Geral' } },
    })
  })

  it('remover da equipe manda remove no patch', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({ crew: [crew()] })
    fireEvent.click(screen.getByLabelText('Remover Dirigente da equipe'))
    const corpo = await salvarECorpo(m)
    expect(corpo.credits.crew).toEqual({ reorder: [], remove: ['10|Director'] })
  })

  it('adicionar um novo membro da equipe manda add no patch', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({})
    fireEvent.change(screen.getByLabelText('Nome do novo membro da equipe'), {
      target: { value: 'Fulano' },
    })
    fireEvent.change(
      screen.getByLabelText('Departamento do novo membro da equipe'),
      {
        target: { value: 'Sound' },
      },
    )
    fireEvent.change(screen.getByLabelText('Cargo do novo membro da equipe'), {
      target: { value: 'Mixagem' },
    })
    fireEvent.click(screen.getByText('Adicionar à equipe'))
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

  it('editar o nome de uma produtora manda edit no patch', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({ companies: [company()] })
    fireEvent.change(screen.getByLabelText('Nome da produtora'), {
      target: { value: 'StudioX Renomeado' },
    })
    const corpo = await salvarECorpo(m)
    expect(corpo.credits.companies).toEqual({
      reorder: ['100'],
      edit: { '100': { name: 'StudioX Renomeado' } },
    })
  })

  it('remover uma produtora manda remove no patch', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({ companies: [company()] })
    fireEvent.click(screen.getByLabelText('Remover StudioX'))
    const corpo = await salvarECorpo(m)
    expect(corpo.credits.companies).toEqual({ reorder: [], remove: ['100'] })
  })

  it('adicionar uma nova produtora manda add no patch', async () => {
    const m = patchOverrideRota(() => okJson({ id: 'e1' }, 200))
    renderEditor({})
    fireEvent.change(screen.getByLabelText('Nome da nova produtora'), {
      target: { value: 'Estúdio Novo' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Adicionar produtora' }))
    const corpo = await salvarECorpo(m)
    const bloco = corpo.credits.companies as {
      add: { source_id: string; name: string }[]
    }
    expect(bloco.add).toHaveLength(1)
    expect(bloco.add[0].name).toBe('Estúdio Novo')
    expect(bloco.add[0].source_id.startsWith('custom-')).toBe(true)
  })

  it('mostra erro inline quando o PATCH falha', async () => {
    patchOverrideRota(() =>
      erroJson(422, 'invalid_patch', 'patch de creditos invalido'),
    )
    renderEditor({ cast: [cast({ source_id: '1', name: 'Ana' })] })
    fireEvent.change(screen.getByLabelText('Personagem de Ana'), {
      target: { value: 'Nova' },
    })
    fireEvent.click(screen.getByText('Salvar créditos'))
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

  it('reporta false ao montar (nada editado)', () => {
    const onDirtyChange = vi.fn()
    renderEditorComOnDirtyChange(
      { cast: [cast({ source_id: '1', name: 'Ana' })] },
      onDirtyChange,
    )
    expect(onDirtyChange).toHaveBeenCalledWith(false)
    expect(onDirtyChange).toHaveBeenCalledTimes(1)
  })

  it('reporta true ao reordenar o elenco', () => {
    const onDirtyChange = vi.fn()
    renderEditorComOnDirtyChange({ cast: doisAtores() }, onDirtyChange)
    fireEvent.click(screen.getAllByLabelText('Mover para baixo')[0])
    expect(onDirtyChange).toHaveBeenLastCalledWith(true)
  })

  it('reporta true ao reordenar em lote (mover para posição, briefing 0076)', () => {
    const onDirtyChange = vi.fn()
    renderEditorComOnDirtyChange({ cast: doisAtores() }, onDirtyChange)
    const campo = screen.getByLabelText('Mover Ana para a posição (de 2)')
    fireEvent.change(campo, { target: { value: '2' } })
    fireEvent.blur(campo)
    expect(onDirtyChange).toHaveBeenLastCalledWith(true)
  })

  it('reporta true ao editar um campo da equipe', () => {
    const onDirtyChange = vi.fn()
    renderEditorComOnDirtyChange({ crew: [crew()] }, onDirtyChange)
    fireEvent.change(screen.getByLabelText('Cargo de Dirigente'), {
      target: { value: 'Diretor Geral' },
    })
    expect(onDirtyChange).toHaveBeenLastCalledWith(true)
  })

  it('reporta true ao remover uma produtora', () => {
    const onDirtyChange = vi.fn()
    renderEditorComOnDirtyChange({ companies: [company()] }, onDirtyChange)
    fireEvent.click(screen.getByLabelText('Remover StudioX'))
    expect(onDirtyChange).toHaveBeenLastCalledWith(true)
  })

  it('reporta true ao adicionar um novo membro do elenco', () => {
    const onDirtyChange = vi.fn()
    renderEditorComOnDirtyChange({}, onDirtyChange)
    fireEvent.change(screen.getByLabelText('Nome do novo membro do elenco'), {
      target: { value: 'Carlos' },
    })
    fireEvent.click(screen.getByText('Adicionar ao elenco'))
    expect(onDirtyChange).toHaveBeenLastCalledWith(true)
  })
})
