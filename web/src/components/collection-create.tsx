import { useState, type FormEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { FolderPlus, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { ApiError, createCollection, type ResolvedEntity } from '@/lib/api'

// CollectionCreate: cria uma coleção LOCAL (sem source) do zero (0025 Fase B). Um
// botão "Nova coleção" abre um formulário inline (título obrigatório + descrição
// opcional) — não há primitivo de Dialog no projeto, então o form expande no lugar.
// Ao criar: invalida `['entities']` (a coleção nova é uma entity e entra no grid) e
// NAVEGA para `/entities/{id}` (a página da coleção, onde o dono adiciona membros).
// Erro da API (ex.: 422 título vazio) aparece INLINE com a `message` pt-BR.
export function CollectionCreate() {
  const [aberto, setAberto] = useState(false)

  if (!aberto) {
    return (
      <Button variant="outline" size="sm" onClick={() => setAberto(true)}>
        <FolderPlus aria-hidden />
        Nova coleção
      </Button>
    )
  }
  return <CollectionCreateForm onClose={() => setAberto(false)} />
}

function CollectionCreateForm({ onClose }: { onClose: () => void }) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')

  const mutation = useMutation({
    mutationFn: (body: { title: string; description?: string }) =>
      createCollection(body),
    onSuccess: (entity: ResolvedEntity) => {
      queryClient.invalidateQueries({ queryKey: ['entities'] })
      navigate({ to: '/entities/$id', params: { id: entity.id } })
    },
  })

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    const limpo = title.trim()
    if (!limpo) {
      return
    }
    const desc = description.trim()
    mutation.mutate({ title: limpo, description: desc || undefined })
  }

  const mensagemErro =
    mutation.error instanceof ApiError
      ? mutation.error.message
      : mutation.isError
        ? 'Não foi possível criar a coleção.'
        : null

  return (
    <form
      onSubmit={onSubmit}
      className="flex w-full max-w-md flex-col gap-3 rounded-xl bg-card p-4 ring-1 ring-foreground/10"
    >
      <div className="flex items-center justify-between">
        <span className="font-medium">Nova coleção</span>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          aria-label="Cancelar"
          onClick={onClose}
        >
          <X aria-hidden />
        </Button>
      </div>
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
      {mensagemErro ? (
        <p role="alert" className="text-sm font-medium text-destructive">
          {mensagemErro}
        </p>
      ) : null}
      <div className="flex justify-end">
        <Button type="submit" disabled={mutation.isPending || !title.trim()}>
          <FolderPlus aria-hidden />
          {mutation.isPending ? 'Criando…' : 'Criar coleção'}
        </Button>
      </div>
    </form>
  )
}
