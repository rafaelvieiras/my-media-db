import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Plus, Search, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { ErrorState, LoadingState } from '@/components/query-states'
import { searchQueryOptions } from '@/lib/queries'
import { useDebounced } from '@/lib/use-debounced'
import { useLanguage } from '@/lib/language-context'
import {
  ApiError,
  addCollectionMember,
  posterSrc,
  type EntityCard,
} from '@/lib/api'
import { aspectPorTipo, rotuloTipo } from '@/lib/entity-badges'

const DEBOUNCE_MS = 350

// CollectionMemberPicker: busca no catálogo LOCAL um item para ADICIONAR à coleção
// (0025 Fase B). Reusa `GET /search` (mostra só `local` — resultados externos não
// entram numa coleção sem antes virar entity). Filtra da lista: a própria coleção,
// os membros já ligados (`excludeIds`) e outras coleções (uma coleção não aninha
// outra). Ao escolher um item, `addCollectionMember` devolve a lista de membros
// ATUALIZADA, repassada ao pai por `onAdded` — o picker fica aberto para adicionar
// vários em sequência (o item recém-adicionado some via `excludeIds`).
export function CollectionMemberPicker({
  collectionId,
  excludeIds,
  onAdded,
  onClose,
}: {
  collectionId: string
  excludeIds: Set<string>
  onAdded: (membros: EntityCard[]) => void
  onClose: () => void
}) {
  const { lang } = useLanguage()
  const [termo, setTermo] = useState('')
  const debounced = useDebounced(termo.trim(), DEBOUNCE_MS)

  const busca = useQuery(searchQueryOptions(debounced, undefined, lang))

  const mutation = useMutation({
    mutationFn: (childId: string) => addCollectionMember(collectionId, childId),
    onSuccess: (membros) => onAdded(membros),
  })

  // Candidatos: só os locais que não são a própria coleção, nem já-membros, nem
  // outra coleção.
  const candidatos = (busca.data?.local ?? []).filter(
    (c) =>
      c.id !== collectionId && !excludeIds.has(c.id) && c.type !== 'collection',
  )

  const mensagemErro =
    mutation.error instanceof ApiError
      ? mutation.error.message
      : mutation.isError
        ? 'Não foi possível adicionar o item.'
        : null

  return (
    <div className="flex flex-col gap-3 rounded-xl bg-card p-4 ring-1 ring-foreground/10">
      <div className="flex items-center gap-2">
        <div className="relative min-w-0 flex-1">
          <Search
            className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground"
            aria-hidden
          />
          <Input
            type="search"
            aria-label="Buscar item para adicionar"
            placeholder="Buscar um item do seu catálogo…"
            value={termo}
            onChange={(e) => setTermo(e.target.value)}
            className="h-9 pl-8"
            autoFocus
          />
        </div>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={onClose}
          aria-label="Concluir"
        >
          <X aria-hidden />
          Concluir
        </Button>
      </div>

      {mensagemErro ? (
        <p role="alert" className="text-sm font-medium text-destructive">
          {mensagemErro}
        </p>
      ) : null}

      <PickerResultados
        query={debounced}
        isPending={busca.isPending}
        isError={busca.isError}
        candidatos={candidatos}
        adicionando={mutation.isPending ? mutation.variables : null}
        onEscolher={(childId) => mutation.mutate(childId)}
      />
    </div>
  )
}

function PickerResultados({
  query,
  isPending,
  isError,
  candidatos,
  adicionando,
  onEscolher,
}: {
  query: string
  isPending: boolean
  isError: boolean
  candidatos: EntityCard[]
  adicionando: string | null
  onEscolher: (childId: string) => void
}) {
  if (query === '') {
    return (
      <p className="text-sm text-muted-foreground">
        Digite para buscar um item do seu catálogo e adicioná-lo à coleção.
      </p>
    )
  }
  if (isPending) {
    return <LoadingState label="Buscando…" />
  }
  if (isError) {
    return <ErrorState title="Não foi possível buscar" />
  }
  if (candidatos.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        Nenhum item disponível para adicionar.
      </p>
    )
  }
  return (
    <ul className="flex flex-col gap-1">
      {candidatos.map((c) => (
        <li key={c.id}>
          <button
            type="button"
            disabled={adicionando !== null}
            onClick={() => onEscolher(c.id)}
            className="flex w-full items-center gap-3 rounded-lg p-2 text-left hover:bg-muted disabled:opacity-50"
          >
            <Miniatura card={c} />
            <span className="flex min-w-0 flex-1 flex-col">
              <span className="truncate text-sm font-medium">{c.title}</span>
              <span className="text-xs text-muted-foreground">
                {rotuloTipo(c.type)}
              </span>
            </span>
            <Plus
              className="size-4 shrink-0 text-muted-foreground"
              aria-hidden
            />
          </button>
        </li>
      ))}
    </ul>
  )
}

function Miniatura({ card }: { card: EntityCard }) {
  const src = posterSrc(card.poster_url)
  return (
    <span
      className={`${aspectPorTipo(card.type)} h-12 shrink-0 overflow-hidden rounded bg-muted ring-1 ring-foreground/10`}
    >
      {src ? (
        <img
          src={src}
          alt=""
          loading="lazy"
          className="h-full w-full object-cover"
        />
      ) : null}
    </span>
  )
}
