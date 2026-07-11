import { useState, type FormEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { FolderPlus } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { ApiError, createLibrary, type CreateSource } from '@/lib/api'

// Formulário de adição de datasource: caminho + tipo (Filmes/Séries). Erro de path
// inválido vindo da API aparece INLINE (a `message` pt-BR do envelope). Ao criar
// com sucesso, invalida a lista de datasources e limpa o caminho. O seletor define
// tanto `type` (movie/tv, contrato legado) quanto `content_type` (movie/series,
// 0034) — este último liga o fluxo de scan de séries (arquivo→episódio).
export function DatasourceForm() {
  const queryClient = useQueryClient()
  const [path, setPath] = useState('')
  const [type, setType] = useState<CreateSource['type']>('movie')

  const mutation = useMutation({
    mutationFn: (body: CreateSource) => createLibrary(body),
    onSuccess: () => {
      setPath('')
      queryClient.invalidateQueries({ queryKey: ['library'] })
    },
  })

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    const limpo = path.trim()
    if (!limpo) {
      return
    }
    // Séries (type=tv) marca a biblioteca como content_type='series'; Filmes fica
    // 'movie'. É o valor por que o scanner ramifica o fluxo de match (0034).
    const content_type = type === 'tv' ? 'series' : 'movie'
    mutation.mutate({ path: limpo, type, content_type })
  }

  const mensagemErro =
    mutation.error instanceof ApiError
      ? mutation.error.message
      : mutation.isError
        ? 'Não foi possível adicionar o datasource.'
        : null

  return (
    <form
      onSubmit={onSubmit}
      className="flex flex-col gap-3 rounded-xl bg-card p-4 ring-1 ring-foreground/10"
    >
      <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
        <label className="flex flex-1 flex-col gap-1 text-sm">
          <span className="text-muted-foreground">Caminho</span>
          <Input
            value={path}
            onChange={(e) => setPath(e.target.value)}
            placeholder="/mnt/midia/filmes"
            aria-label="Caminho do datasource"
            aria-invalid={mensagemErro !== null || undefined}
          />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-muted-foreground">Tipo</span>
          <Select
            value={type}
            onChange={(e) => setType(e.target.value as CreateSource['type'])}
            aria-label="Tipo do datasource"
            className="w-full sm:w-36"
          >
            <option value="movie">Filmes</option>
            <option value="tv">Séries</option>
          </Select>
        </label>
        <Button type="submit" disabled={mutation.isPending}>
          <FolderPlus aria-hidden />
          {mutation.isPending ? 'Adicionando…' : 'Adicionar'}
        </Button>
      </div>
      {mensagemErro ? (
        <p role="alert" className="text-sm font-medium text-destructive">
          {mensagemErro}
        </p>
      ) : null}
    </form>
  )
}
