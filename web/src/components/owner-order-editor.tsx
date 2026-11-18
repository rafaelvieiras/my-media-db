import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { ErrorState, LoadingState } from '@/components/query-states'
import { useLanguage } from '@/lib/language-context'
import { useT } from '@/lib/i18n'
import {
  deleteOwnerOrder,
  putOwnerOrder,
  type OwnerOrder,
  type OwnerOrderUnit,
} from '@/lib/api'
import {
  aPartirDaGrade,
  fundir,
  mover,
  paraPayload,
  removerUnidade,
  renumerar,
  separar,
  type EpisodioDaGrade,
} from '@/lib/owner-order-model'
import {
  entityEpisodesQueryOptions,
  ownerOrderQueryOptions,
} from '@/lib/queries'

// OWNER ORDER EDITOR (ADR-0033). A drawer on the series screen where the owner numbers
// the episodes the way their release does, including the case no source can express: a
// numbered unit that merges several aired episodes. The rules live in
// `@/lib/owner-order-model` (pure, tested); this file is the view and the two mutations.
//
// The episode list comes from the BASE order (the series' default, e.g. `official`), so
// the owner starts from something numbered instead of a blank page.

// gradeDaOrdem flattens the grouped episode response into the editable list: only the
// episodes that carry a number in the base order can be numbered, so the ones without one
// (a draft) are left out instead of being given a made-up position.
function gradeDaOrdem(
  grupos: {
    episodes: {
      id: string
      title: string
      numbering?: { season: number; number: number } | null
    }[]
  }[],
): EpisodioDaGrade[] {
  const out: EpisodioDaGrade[] = []
  for (const grupo of grupos) {
    for (const ep of grupo.episodes) {
      if (!ep.numbering) {
        continue
      }
      out.push({
        id: ep.id,
        title: ep.title,
        season: ep.numbering.season,
        number: ep.numbering.number,
      })
    }
  }
  return out
}

// OwnerOrderEditor is the drawer. `base` is the order the episode list is read in (the
// series' default); it never changes the stored order, only the starting point.
export function OwnerOrderEditor({
  id,
  base,
  open,
  onOpenChange,
}: {
  id: string
  base: string
  open: boolean
  onOpenChange: (aberto: boolean) => void
}) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="w-full gap-4 overflow-y-auto sm:max-w-2xl"
      >
        <SheetHeader>
          <SheetTitle>{useT()('entity.edit.ownerOrder.title')}</SheetTitle>
        </SheetHeader>
        {/* The body is mounted only while open: its draft state is seeded from the
            stored order in the useState initializer, so a cancelled edit never leaks
            into the next opening (and nothing is fetched before the owner asks for it). */}
        {open ? (
          <CorpoDoEditor
            id={id}
            base={base}
            onClose={() => onOpenChange(false)}
          />
        ) : null}
      </SheetContent>
    </Sheet>
  )
}

// CorpoDoEditor is the body of the drawer: it loads the stored order and the episodes of
// the base order, and only then mounts the form with its initial values.
function CorpoDoEditor({
  id,
  base,
  onClose,
}: {
  id: string
  base: string
  onClose: () => void
}) {
  const t = useT()
  const { lang } = useLanguage()
  const ordem = useQuery(ownerOrderQueryOptions(id))
  const episodios = useQuery(entityEpisodesQueryOptions(id, base, lang))

  if (ordem.isPending || episodios.isPending) {
    return <LoadingState label={t('common.loading')} />
  }
  if (ordem.isError || episodios.isError || !ordem.data || !episodios.data) {
    return <ErrorState title={t('entity.edit.ownerOrder.loadError')} />
  }
  return (
    <FormularioDaOrdem
      id={id}
      base={base}
      ordemInicial={ordem.data}
      grade={gradeDaOrdem(episodios.data.groups)}
      onClose={onClose}
    />
  )
}

// FormularioDaOrdem is the editable part: the draft lives here, seeded from the stored
// order once (initializer), never from an effect.
function FormularioDaOrdem({
  id,
  base,
  ordemInicial,
  grade,
  onClose,
}: {
  id: string
  base: string
  ordemInicial: OwnerOrder
  grade: EpisodioDaGrade[]
  onClose: () => void
}) {
  const t = useT()
  const qc = useQueryClient()
  const [nome, setNome] = useState(ordemInicial.name)
  const [unidades, setUnidades] = useState<OwnerOrderUnit[]>(() =>
    ordemInicial.units.map((u) => ({ ...u, episodes: [...u.episodes] })),
  )
  const [selecionados, setSelecionados] = useState<string[]>([])

  const salvar = useMutation({
    mutationFn: () => putOwnerOrder(id, paraPayload(nome, base, unidades)),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['entity', id] })
      onClose()
    },
  })
  const apagar = useMutation({
    mutationFn: () => deleteOwnerOrder(id),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['entity', id] })
      onClose()
    },
  })

  const tituloPorId = new Map(grade.map((ep) => [ep.id, ep.title]))

  const alternar = (epId: string) => {
    setSelecionados((atual) =>
      atual.includes(epId) ? atual.filter((i) => i !== epId) : [...atual, epId],
    )
  }

  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-muted-foreground">
        {t('entity.edit.ownerOrder.description')}
      </p>
      <label className="flex flex-col gap-1 text-sm">
        <span className="text-muted-foreground">
          {t('entity.edit.ownerOrder.nameLabel')}
        </span>
        <Input
          value={nome}
          onChange={(e) => setNome(e.target.value)}
          placeholder={t('entity.edit.ownerOrder.namePlaceholder')}
        />
      </label>

      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="button"
          variant="outline"
          disabled={grade.length === 0}
          onClick={() => setUnidades(aPartirDaGrade(grade))}
        >
          {t('entity.edit.ownerOrder.createFrom', { ordering: base })}
        </Button>
        <Button
          type="button"
          disabled={selecionados.length < 2}
          onClick={() => {
            setUnidades(fundir(unidades, selecionados))
            setSelecionados([])
          }}
        >
          {t('entity.edit.ownerOrder.mergeSelected', {
            n: selecionados.length,
          })}
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">
        {unidades.length === 0
          ? t('entity.edit.ownerOrder.createHint')
          : t('entity.edit.ownerOrder.mergeSelectionHint')}
      </p>

      {/* Units in order: editable (season, number), with split/remove/move. */}
      <div className="flex flex-col gap-2">
        <h3 className="text-sm font-medium">
          {t('entity.edit.ownerOrder.units')}
        </h3>
        {unidades.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            {t('entity.edit.ownerOrder.noUnits')}
          </p>
        ) : null}
        {/* Keyed by position, not by the (season, number): editing those fields
                  must not remount the row, which would drop the focus mid-typing. */}
        {unidades.map((unidade, indice) => (
          <div
            key={indice}
            className="flex flex-col gap-2 rounded-md border p-2"
          >
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-xs text-muted-foreground">
                {t('entity.edit.ownerOrder.season')}
              </span>
              <Input
                className="w-16"
                type="number"
                min={0}
                aria-label={t('entity.edit.ownerOrder.season')}
                value={unidade.season}
                onChange={(e) =>
                  setUnidades((atual) =>
                    atual.map((u, i) =>
                      i === indice
                        ? { ...u, season: Number(e.target.value) }
                        : u,
                    ),
                  )
                }
              />
              <span className="text-xs text-muted-foreground">
                {t('entity.edit.ownerOrder.number')}
              </span>
              <Input
                className="w-16"
                type="number"
                min={1}
                aria-label={t('entity.edit.ownerOrder.number')}
                value={unidade.number}
                onChange={(e) =>
                  setUnidades((atual) =>
                    atual.map((u, i) =>
                      i === indice
                        ? { ...u, number: Number(e.target.value) }
                        : u,
                    ),
                  )
                }
              />
              <span className="text-xs text-muted-foreground">
                {unidade.episodes.length === 1
                  ? t('entity.edit.ownerOrder.single')
                  : t('entity.edit.ownerOrder.merges', {
                      n: unidade.episodes.length,
                    })}
              </span>
              <div className="ml-auto flex gap-1">
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  disabled={indice === 0}
                  onClick={() => setUnidades(mover(unidades, indice, -1))}
                >
                  {t('entity.edit.ownerOrder.moveUp')}
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  disabled={indice === unidades.length - 1}
                  onClick={() => setUnidades(mover(unidades, indice, 1))}
                >
                  {t('entity.edit.ownerOrder.moveDown')}
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  disabled={unidade.episodes.length < 2}
                  onClick={() => setUnidades(separar(unidades, indice))}
                >
                  {t('entity.edit.ownerOrder.split')}
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  onClick={() => setUnidades(removerUnidade(unidades, indice))}
                >
                  {t('entity.edit.ownerOrder.remove')}
                </Button>
              </div>
            </div>
            <ul className="flex flex-wrap gap-1 text-xs">
              {unidade.episodes.map((epId) => (
                <li
                  key={epId}
                  className="rounded bg-muted px-1.5 py-0.5 text-muted-foreground"
                >
                  {tituloPorId.get(epId) ?? epId}
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>

      {/* Episode list of the BASE order, with the multi-select that feeds "merge". */}
      <div className="flex flex-col gap-2">
        <h3 className="text-sm font-medium">
          {t('entity.edit.ownerOrder.episodes')} ({base})
        </h3>
        <ul className="grid grid-cols-1 gap-1 sm:grid-cols-2">
          {grade.map((ep) => (
            <li key={ep.id}>
              <label className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={selecionados.includes(ep.id)}
                  onChange={() => alternar(ep.id)}
                />
                <span className="text-muted-foreground">
                  S{String(ep.season).padStart(2, '0')}E
                  {String(ep.number).padStart(2, '0')}
                </span>
                <span className="truncate">{ep.title}</span>
              </label>
            </li>
          ))}
        </ul>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="button"
          disabled={salvar.isPending || unidades.length === 0}
          onClick={() => salvar.mutate()}
        >
          {salvar.isPending
            ? t('entity.edit.ownerOrder.saving')
            : t('entity.edit.ownerOrder.save')}
        </Button>
        <Button
          type="button"
          variant="outline"
          onClick={() => setUnidades(renumerar(unidades))}
        >
          {t('entity.edit.ownerOrder.units')} 1..N
        </Button>
        <Button
          type="button"
          variant="ghost"
          disabled={apagar.isPending || ordemInicial.units.length === 0}
          onClick={() => apagar.mutate()}
        >
          {apagar.isPending
            ? t('entity.edit.ownerOrder.deleting')
            : t('entity.edit.ownerOrder.delete')}
        </Button>
      </div>
      {salvar.isError || apagar.isError ? (
        <p className="text-sm text-destructive">
          {t('entity.edit.ownerOrder.saveError')}
        </p>
      ) : null}
    </div>
  )
}
