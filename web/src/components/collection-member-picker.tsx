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
import { useT } from '@/lib/i18n'

const DEBOUNCE_MS = 350

// CollectionMemberPicker: searches the LOCAL catalog for an item to ADD to the collection.
// It reuses `GET /search` (it shows only `local` — external results do not
// enter a collection without first becoming an entity). It filters out of the list: the collection itself,
// the members already linked (`excludeIds`) and other collections (a collection does not nest
// another one). When an item is chosen, `addCollectionMember` returns the member list
// UPDATED, passed on to the parent via `onAdded` — the picker stays open to add
// several in a row (the newly added item disappears via `excludeIds`).
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
  const t = useT()
  const [termo, setTermo] = useState('')
  const debounced = useDebounced(termo.trim(), DEBOUNCE_MS)

  const busca = useQuery(searchQueryOptions(debounced, undefined, lang))

  const mutation = useMutation({
    mutationFn: (childId: string) => addCollectionMember(collectionId, childId),
    onSuccess: (membros) => onAdded(membros),
  })

  // Candidates: only the local ones that are not the collection itself, nor already members, nor
  // another collection.
  const candidatos = (busca.data?.local ?? []).filter(
    (c) =>
      c.id !== collectionId && !excludeIds.has(c.id) && c.type !== 'collection',
  )

  const mensagemErro =
    mutation.error instanceof ApiError
      ? mutation.error.message
      : mutation.isError
        ? t('collections.picker.addFailed')
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
            aria-label={t('collections.picker.searchAriaLabel')}
            placeholder={t('collections.picker.searchPlaceholder')}
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
          aria-label={t('collections.picker.done')}
        >
          <X aria-hidden />
          {t('collections.picker.done')}
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
  const t = useT()
  if (query === '') {
    return (
      <p className="text-sm text-muted-foreground">
        {t('collections.picker.hint')}
      </p>
    )
  }
  if (isPending) {
    return <LoadingState label={t('collections.picker.searching')} />
  }
  if (isError) {
    return <ErrorState title={t('collections.picker.searchError')} />
  }
  if (candidatos.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        {t('collections.picker.empty')}
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
                {rotuloTipo(c.type, t)}
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
