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
      submitLabel="Salvar"
      submitPendingLabel="Salvando…"
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
      submitLabel="Salvar"
      submitPendingLabel="Salvando…"
      isPending={false}
      mensagemErro={null}
      lang="pt-BR"
      {...over}
    />,
  )
}

describe('SchemaForm — render por widget', () => {
  it('renderiza um input por widget a partir dos descritores', () => {
    renderCriacao(descritoresPadrao.filter((d) => d.creatable))
    // text, textarea and date become inputs with the label of the descriptor.
    expect(screen.getByLabelText('Título')).toBeInTheDocument()
    expect(screen.getByLabelText('Sinopse')).toBeInTheDocument()
    expect(screen.getByLabelText('Título original')).toBeInTheDocument()
    expect(screen.getByLabelText('Data de lançamento')).toBeInTheDocument()
  })

  it('o widget number vem com min/max/step do descritor', () => {
    renderCriacao([
      { ...descritoresPadrao[5], creatable: true }, // local_rating
    ])
    const input = screen.getByLabelText('Sua nota')
    expect(input).toHaveAttribute('type', 'number')
    expect(input).toHaveAttribute('min', '0')
    expect(input).toHaveAttribute('max', '10')
  })

  it('EXTENSIBILIDADE: um descritor NOVO renderiza sem tocar JSX de campo', () => {
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

describe('SchemaForm — interações', () => {
  it('o submit entrega os valores digitados', async () => {
    const user = userEvent.setup()
    const { onSubmit } = renderCriacao(
      descritoresPadrao.filter((d) => d.creatable),
    )
    await user.type(screen.getByLabelText('Título'), 'Duna')
    await user.click(screen.getByRole('button', { name: 'Salvar' }))
    await vi.waitFor(() =>
      expect(onSubmit).toHaveBeenCalledWith(
        expect.objectContaining({ title: 'Duna' }),
      ),
    )
  })

  it('não submete com o obrigatório vazio (botão desabilitado)', () => {
    renderCriacao(descritoresPadrao.filter((d) => d.creatable))
    expect(screen.getByRole('button', { name: 'Salvar' })).toBeDisabled()
  })

  it('o widget de tags adiciona chips (edição)', async () => {
    const user = userEvent.setup()
    renderEdicao()
    await user.type(screen.getByLabelText('Adicionar tag'), 'Clássico')
    await user.click(screen.getByRole('button', { name: 'Adicionar' }))
    expect(screen.getByText('Clássico')).toBeInTheDocument()
  })

  it('o botão "Limpar" aparece na edição e reporta campo + translatable', async () => {
    const user = userEvent.setup()
    const onClear = vi.fn()
    renderEdicao({ onClear })
    await user.click(screen.getByRole('button', { name: 'Limpar título' }))
    expect(onClear).toHaveBeenCalledWith('title', true)
    await user.click(
      screen.getByRole('button', { name: 'Limpar título original' }),
    )
    expect(onClear).toHaveBeenCalledWith('original_title', false)
  })
})

// "Leave without saving" guard: onDirtyChange mirrors `form.state.isDirty`,
// called only on the transition (not on every keystroke).
describe('SchemaForm — onDirtyChange (0073)', () => {
  it('reporta false ao montar (form pristino)', () => {
    const onDirtyChange = vi.fn()
    renderEdicao({ onDirtyChange })
    expect(onDirtyChange).toHaveBeenCalledWith(false)
    expect(onDirtyChange).toHaveBeenCalledTimes(1)
  })

  it('reporta true na primeira alteração de um campo', async () => {
    const user = userEvent.setup()
    const onDirtyChange = vi.fn()
    renderEdicao({ onDirtyChange })
    await user.type(screen.getByLabelText('Título'), 'D')
    await vi.waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(true))
  })

  it('não repete a chamada em teclas seguintes (só na transição)', async () => {
    const user = userEvent.setup()
    const onDirtyChange = vi.fn()
    renderEdicao({ onDirtyChange })
    await user.type(screen.getByLabelText('Título'), 'Duna')
    await vi.waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(true))
    // false (mount) + true (1st key) — the next 3 keys do not repeat it.
    expect(onDirtyChange).toHaveBeenCalledTimes(2)
  })
})
