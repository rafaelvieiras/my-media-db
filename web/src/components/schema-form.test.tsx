import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { FieldDescriptor } from '@/lib/api'
import { SchemaForm } from '@/components/schema-form'
import {
  schemaZod,
  valoresDeCriacao,
  valoresDeEdicao,
} from '@/lib/field-schema'
import { baseEntity, descritoresPadrao } from '@/test/entity-fixtures'

// SchemaForm is the GENERIC renderer: the inputs and the validation are born from the
// descriptors. These tests prove that each widget renders and — the central
// acceptance criterion — that injecting a NEW descriptor makes the input appear without touching
// the field JSX.

function renderCriacao(
  fields: FieldDescriptor[],
  over: Partial<Parameters<typeof SchemaForm>[0]> = {},
) {
  const onSubmit = vi.fn()
  render(
    <SchemaForm
      fields={fields}
      defaultValues={valoresDeCriacao(fields)}
      schema={schemaZod(fields, 'criacao')}
      onSubmit={onSubmit}
      onCancel={() => {}}
      submitLabel="Save"
      submitPendingLabel="Saving…"
      isPending={false}
      mensagemErro={null}
      {...over}
    />,
  )
  return { onSubmit }
}

// renderEdicao mounts the form in EDIT mode (current language, seeds resolved
// from the entity) — the same scaffold as the tags tests and of the "Limpar" button, which only
// differ in extra props (`over`, e.g. `onClear`).
function renderEdicao(over: Partial<Parameters<typeof SchemaForm>[0]> = {}) {
  const entity = baseEntity()
  const fields = descritoresPadrao
  render(
    <SchemaForm
      fields={fields}
      defaultValues={valoresDeEdicao(fields, {}, 'pt-BR', entity)}
      schema={schemaZod(fields, 'edicao')}
      onSubmit={() => {}}
      onCancel={() => {}}
      submitLabel="Save"
      submitPendingLabel="Saving…"
      isPending={false}
      mensagemErro={null}
      lang="pt-BR"
      {...over}
    />,
  )
}

describe('SchemaForm — rendering per widget', () => {
  it('renders one input per widget from the descriptors', () => {
    renderCriacao(descritoresPadrao.filter((d) => d.creatable))
    // text, textarea and date become inputs with the label of the descriptor.
    expect(screen.getByLabelText('Título')).toBeInTheDocument()
    expect(screen.getByLabelText('Sinopse')).toBeInTheDocument()
    expect(screen.getByLabelText('Título original')).toBeInTheDocument()
    expect(screen.getByLabelText('Data de lançamento')).toBeInTheDocument()
  })

  it('the number widget comes with min/max/step from the descriptor', () => {
    renderCriacao([
      { ...descritoresPadrao[5], creatable: true }, // local_rating
    ])
    const input = screen.getByLabelText('Sua nota')
    expect(input).toHaveAttribute('type', 'number')
    expect(input).toHaveAttribute('min', '0')
    expect(input).toHaveAttribute('max', '10')
  })

  it('EXTENSIBILITY: a NEW descriptor renders without touching field JSX', () => {
    const novo: FieldDescriptor = {
      name: 'budget',
      label: 'Orçamento',
      group: 'neutro',
      widget: 'number',
      translatable: false,
      kind: 'numero',
      order: 70,
      creatable: true,
      min: 0,
      max: 1000,
      step: 1,
    }
    renderCriacao([...descritoresPadrao.filter((d) => d.creatable), novo])
    expect(screen.getByLabelText('Orçamento')).toBeInTheDocument()
  })
})

describe('SchemaForm — interactions', () => {
  it('the submit delivers the typed values', async () => {
    const user = userEvent.setup()
    const { onSubmit } = renderCriacao(
      descritoresPadrao.filter((d) => d.creatable),
    )
    await user.type(screen.getByLabelText('Título'), 'Duna')
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await vi.waitFor(() =>
      expect(onSubmit).toHaveBeenCalledWith(
        expect.objectContaining({ title: 'Duna' }),
      ),
    )
  })

  it('does not submit with the required field empty (button disabled)', () => {
    renderCriacao(descritoresPadrao.filter((d) => d.creatable))
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  })

  it('the tags widget adds chips (edit)', async () => {
    const user = userEvent.setup()
    renderEdicao()
    await user.type(screen.getByLabelText('Add a tag'), 'Clássico')
    await user.click(screen.getByRole('button', { name: 'Add' }))
    expect(screen.getByText('Clássico')).toBeInTheDocument()
  })

  it('the "Clear" button appears on edit and reports field + translatable', async () => {
    const user = userEvent.setup()
    const onClear = vi.fn()
    renderEdicao({ onClear })
    await user.click(screen.getByRole('button', { name: 'Clear título' }))
    expect(onClear).toHaveBeenCalledWith('title', true)
    await user.click(
      screen.getByRole('button', { name: 'Clear título original' }),
    )
    expect(onClear).toHaveBeenCalledWith('original_title', false)
  })
})

// "Leave without saving" guard: onDirtyChange mirrors `form.state.isDirty`,
// called only on the transition (not on every keystroke).
describe('SchemaForm — onDirtyChange (0073)', () => {
  it('reports false on mount (pristine form)', () => {
    const onDirtyChange = vi.fn()
    renderEdicao({ onDirtyChange })
    expect(onDirtyChange).toHaveBeenCalledWith(false)
    expect(onDirtyChange).toHaveBeenCalledTimes(1)
  })

  it('reports true on the first change of a field', async () => {
    const user = userEvent.setup()
    const onDirtyChange = vi.fn()
    renderEdicao({ onDirtyChange })
    await user.type(screen.getByLabelText('Título'), 'D')
    await vi.waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(true))
  })

  it('does not repeat the call on the following keys (only on the transition)', async () => {
    const user = userEvent.setup()
    const onDirtyChange = vi.fn()
    renderEdicao({ onDirtyChange })
    await user.type(screen.getByLabelText('Título'), 'Duna')
    await vi.waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(true))
    // false (mount) + true (1st key) — the next 3 keys do not repeat it.
    expect(onDirtyChange).toHaveBeenCalledTimes(2)
  })
})
