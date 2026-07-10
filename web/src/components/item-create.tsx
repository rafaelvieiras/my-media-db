import { useState } from 'react'
import { FilmIcon } from 'lucide-react'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { createItem } from '@/lib/api'
import { CreateFormShell, CreateToggle } from '@/components/inline-create-form'
import { useCriarEntidade } from '@/lib/use-criar-entidade'

// ItemCreate: cria um item LOCAL (filme ou série) do zero, SEM fonte externa (0027).
// Reusa o scaffold de criação inline (CreateToggle/CreateFormShell) — só injeta os campos
// (tipo, título e opcionais) e o corpo. Ao criar, o hook invalida o grid e navega para
// `/entities/{id}`, onde o dono pode fixar os IDs externos e re-enriquecer para sincronizar.
export function ItemCreate() {
  return (
    <CreateToggle label="Novo item" icon={<FilmIcon aria-hidden />}>
      {(onClose) => <ItemCreateForm onClose={onClose} />}
    </CreateToggle>
  )
}

function ItemCreateForm({ onClose }: { onClose: () => void }) {
  const [type, setType] = useState<'movie' | 'tv'>('movie')
  const [title, setTitle] = useState('')
  const [originalTitle, setOriginalTitle] = useState('')
  const [description, setDescription] = useState('')
  const [releaseDate, setReleaseDate] = useState('')

  const { mutation, mensagemErro } = useCriarEntidade(
    createItem,
    'Não foi possível criar o item.',
  )

  function onSubmit() {
    const limpo = title.trim()
    if (!limpo) {
      return
    }
    mutation.mutate({
      type,
      title: limpo,
      original_title: originalTitle.trim() || undefined,
      description: description.trim() || undefined,
      release_date: releaseDate.trim() || undefined,
    })
  }

  return (
    <CreateFormShell
      title="Novo item"
      submitLabel="Criar item"
      submitPendingLabel="Criando…"
      submitIcon={<FilmIcon aria-hidden />}
      isPending={mutation.isPending}
      canSubmit={!!title.trim()}
      mensagemErro={mensagemErro}
      onSubmit={onSubmit}
      onClose={onClose}
    >
      <label className="flex flex-col gap-1 text-sm">
        <span className="text-muted-foreground">Tipo</span>
        <Select
          value={type}
          onChange={(e) => setType(e.target.value as 'movie' | 'tv')}
          aria-label="Tipo do item"
        >
          <option value="movie">Filme</option>
          <option value="tv">Série</option>
        </Select>
      </label>
      <label className="flex flex-col gap-1 text-sm">
        <span className="text-muted-foreground">Título</span>
        <Input
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="Nome do filme ou série"
          aria-label="Título do item"
          aria-invalid={mensagemErro !== null || undefined}
          autoFocus
        />
      </label>
      <label className="flex flex-col gap-1 text-sm">
        <span className="text-muted-foreground">
          Título original (opcional)
        </span>
        <Input
          value={originalTitle}
          onChange={(e) => setOriginalTitle(e.target.value)}
          aria-label="Título original do item"
        />
      </label>
      <label className="flex flex-col gap-1 text-sm">
        <span className="text-muted-foreground">Sinopse (opcional)</span>
        <Input
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          aria-label="Sinopse do item"
        />
      </label>
      <label className="flex flex-col gap-1 text-sm">
        <span className="text-muted-foreground">
          Data de lançamento (opcional)
        </span>
        <Input
          value={releaseDate}
          onChange={(e) => setReleaseDate(e.target.value)}
          placeholder="AAAA-MM-DD"
          aria-label="Data de lançamento do item"
        />
      </label>
    </CreateFormShell>
  )
}
