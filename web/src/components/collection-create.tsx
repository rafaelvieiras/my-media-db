import { useState } from 'react'
import { FolderPlus } from 'lucide-react'
import { Input } from '@/components/ui/input'
import { createCollection } from '@/lib/api'
import { CreateFormShell, CreateToggle } from '@/components/inline-create-form'
import { useCriarEntidade } from '@/lib/use-criar-entidade'

// CollectionCreate: cria uma coleção LOCAL (sem source) do zero (0025 Fase B). Reusa o
// scaffold de criação inline (CreateToggle/CreateFormShell, 0027) — só injeta os campos
// (título obrigatório + descrição opcional) e o corpo. Ao criar, o hook invalida o grid
// e navega para `/entities/{id}` (a página da coleção, onde o dono adiciona membros). O
// 422 (título vazio) aparece inline com a `message` pt-BR.
export function CollectionCreate() {
  return (
    <CreateToggle label="Nova coleção" icon={<FolderPlus aria-hidden />}>
      {(onClose) => <CollectionCreateForm onClose={onClose} />}
    </CreateToggle>
  )
}

function CollectionCreateForm({ onClose }: { onClose: () => void }) {
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')

  const { mutation, mensagemErro } = useCriarEntidade(
    createCollection,
    'Não foi possível criar a coleção.',
  )

  function onSubmit() {
    const limpo = title.trim()
    if (!limpo) {
      return
    }
    const desc = description.trim()
    mutation.mutate({ title: limpo, description: desc || undefined })
  }

  return (
    <CreateFormShell
      title="Nova coleção"
      submitLabel="Criar coleção"
      submitPendingLabel="Criando…"
      submitIcon={<FolderPlus aria-hidden />}
      isPending={mutation.isPending}
      canSubmit={!!title.trim()}
      mensagemErro={mensagemErro}
      onSubmit={onSubmit}
      onClose={onClose}
    >
      <label className="flex flex-col gap-1 text-sm">
        <span className="text-muted-foreground">Título</span>
        <Input
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="Minha coleção"
          aria-label="Título da coleção"
          aria-invalid={mensagemErro !== null || undefined}
          autoFocus
        />
      </label>
      <label className="flex flex-col gap-1 text-sm">
        <span className="text-muted-foreground">Descrição (opcional)</span>
        <Input
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="Uma nota sobre o que reúne esta coleção"
          aria-label="Descrição da coleção"
        />
      </label>
    </CreateFormShell>
  )
}
