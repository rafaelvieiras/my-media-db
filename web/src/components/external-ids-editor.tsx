import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { KeyRound, Plus, RefreshCw, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { ApiError, enrichEntity, updateExternalIds } from '@/lib/api'
import { useT } from '@/lib/i18n'

// An editable row of the external identity: source (e.g. tmdb) + id IN THE SOURCE. `key` is
// only for React to reconcile the rows when adding/removing (it does not go to the API).
interface Linha {
  key: number
  source: string
  value: string
}

// linhasDe turns the current `external_ids` map into rows ordered by source
// (stable), each one with its own `key`.
function linhasDe(externalIds: Record<string, string>): Linha[] {
  return Object.entries(externalIds)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([source, value], i) => ({ key: i, source, value }))
}

// mapaDe builds the `{source: id}` to send: only rows with a non-empty source; the last
// occurrence of a repeated source wins. An empty id is KEPT (the backend treats it as
// "remove this source" in the full replace).
function mapaDe(linhas: Linha[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const l of linhas) {
    const s = l.source.trim()
    if (s) {
      out[s] = l.value.trim()
    }
  }
  return out
}

// mapaChave normalizes a {source: id} map into a comparable string (the order of
// the keys does not matter) — used to detect the "dirty" state.
function mapaChave(mapa: Record<string, string>): string {
  return JSON.stringify(
    Object.entries(mapa).sort(([a], [b]) => a.localeCompare(b)),
  )
}

// ExternalIdsEditor edits the EXTERNAL IDENTITY of the entity (`external_ids`, MD4): the
// owner fixes which id of each source the entity uses — the basis for synchronizing in the future (the ids
// of the source may change, or the item may be replaced by another one). Full replace: saving writes
// the whole map; leaving an id blank removes that source. On save, it re-resolves the
// entity (it invalidates `['entity', id]`) and the list (the generated columns change). It applies to
// any type (it is identity). Error (ApiError) inline.
export function ExternalIdsEditor({
  id,
  externalIds,
  onDirtyChange,
}: {
  id: string
  externalIds: Record<string, string>
  // "Leave without saving" guard: called only on the transition of the dirty state.
  onDirtyChange?: (sujo: boolean) => void
}) {
  const queryClient = useQueryClient()
  const t = useT()
  const [linhas, setLinhas] = useState<Linha[]>(() => linhasDe(externalIds))
  const [proximaKey, setProximaKey] = useState(() => linhas.length)
  // Baseline of what is stored (loaded or successfully saved) — compared
  // with the current map to know whether there is something dirty.
  const [salvo, setSalvo] = useState(() => mapaDe(linhasDe(externalIds)))
  // Warns when the write of the identity merged a duplicate automatically
  // — the entity without investment from the owner disappears on its own, without the owner having to
  // hunt for it in the library.
  const [avisoFusao, setAvisoFusao] = useState(false)

  const sujo = useMemo(
    () => mapaChave(mapaDe(linhas)) !== mapaChave(salvo),
    [linhas, salvo],
  )

  useEffect(() => {
    onDirtyChange?.(sujo)
  }, [sujo, onDirtyChange])

  function invalidar() {
    queryClient.invalidateQueries({ queryKey: ['entity', id] })
    queryClient.invalidateQueries({ queryKey: ['entities'] })
  }

  const mutation = useMutation({
    mutationFn: (mapa: Record<string, string>) => updateExternalIds(id, mapa),
    onMutate: () => setAvisoFusao(false),
    onSuccess: (resultado, mapa) => {
      invalidar()
      setAvisoFusao(resultado.duplicataMesclada)
      setSalvo(mapa)
    },
  })

  // Saves the ids AND, on success, re-enriches through the source: it closes the
  // synchronization cycle in one click — it writes the identity and already fetches the source data by the
  // newly fixed id (the enrich without a body reads `external_ids[tmdb]` of the entity, which is why it saves
  // BEFORE). A failure of the enrich (e.g. the id is not from the source) appears inline, but the ids are already
  // saved.
  const sincronizar = useMutation({
    mutationFn: async (mapa: Record<string, string>) => {
      const resultado = await updateExternalIds(id, mapa)
      await enrichEntity(id)
      return { duplicataMesclada: resultado.duplicataMesclada, mapa }
    },
    onMutate: () => setAvisoFusao(false),
    onSuccess: ({ duplicataMesclada, mapa }) => {
      invalidar()
      setAvisoFusao(duplicataMesclada)
      setSalvo(mapa)
    },
  })

  const salvando = mutation.isPending || sincronizar.isPending

  function alterar(key: number, campo: 'source' | 'value', valor: string) {
    setLinhas((atual) =>
      atual.map((l) => (l.key === key ? { ...l, [campo]: valor } : l)),
    )
  }

  function remover(key: number) {
    setLinhas((atual) => atual.filter((l) => l.key !== key))
  }

  function adicionar() {
    setLinhas((atual) => [...atual, { key: proximaKey, source: '', value: '' }])
    setProximaKey((k) => k + 1)
  }

  // Guard: saving an empty map is a full-replace PUT that wipes the
  // identity of the entity. It only asks for confirmation when it is a NET REMOVAL
  // (there was something saved before) — an item that never had any id can save
  // empty without warning, it is not a removal.
  function confirmarSeVazio(mapaNovo: Record<string, string>): boolean {
    const removendoTudo =
      Object.keys(mapaNovo).length === 0 && Object.keys(salvo).length > 0
    if (!removendoTudo) return true
    return window.confirm(t('entity.edit.ids.confirmRemoveAll'))
  }

  const erroAtivo = sincronizar.error ?? mutation.error
  const mensagemErro =
    erroAtivo instanceof ApiError
      ? erroAtivo.message
      : mutation.isError || sincronizar.isError
        ? t('entity.edit.ids.saveError')
        : null

  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-col gap-1">
        <h2 className="flex items-center gap-2 font-heading text-lg font-medium">
          <KeyRound className="size-4" aria-hidden />
          {t('entity.edit.ids.title')}
        </h2>
        <p className="text-sm text-muted-foreground">
          {t('entity.edit.ids.description')}
        </p>
      </div>

      <div className="flex flex-col gap-2">
        {linhas.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            {t('entity.edit.ids.empty')}
          </p>
        ) : (
          linhas.map((l) => (
            <div key={l.key} className="flex items-center gap-2">
              <Input
                value={l.source}
                onChange={(e) => alterar(l.key, 'source', e.target.value)}
                placeholder={t('entity.edit.ids.sourcePlaceholder')}
                aria-label={t('entity.edit.ids.sourceLabel')}
                className="max-w-[10rem]"
              />
              <Input
                value={l.value}
                onChange={(e) => alterar(l.key, 'value', e.target.value)}
                placeholder={t('entity.edit.ids.valuePlaceholder')}
                aria-label={t('entity.edit.ids.valueLabel', {
                  source: l.source || t('entity.edit.ids.newSource'),
                })}
              />
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label={t('entity.edit.ids.remove', {
                  source: l.source || t('entity.edit.ids.sourceFallback'),
                })}
                onClick={() => remover(l.key)}
              >
                <X aria-hidden />
              </Button>
            </div>
          ))
        )}
      </div>

      {mensagemErro ? (
        <p role="alert" className="text-sm font-medium text-destructive">
          {mensagemErro}
        </p>
      ) : null}
      {avisoFusao && !mensagemErro ? (
        <p role="status" className="text-sm text-muted-foreground">
          {t('entity.edit.ids.mergeNotice')}
        </p>
      ) : null}

      <div className="flex flex-wrap items-center gap-2">
        <Button type="button" variant="outline" size="sm" onClick={adicionar}>
          <Plus aria-hidden />
          {t('entity.edit.ids.addSource')}
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={salvando}
          onClick={() => {
            const mapa = mapaDe(linhas)
            if (confirmarSeVazio(mapa)) mutation.mutate(mapa)
          }}
        >
          {mutation.isPending
            ? t('common.action.saving')
            : t('entity.edit.ids.save')}
        </Button>
        <Button
          type="button"
          size="sm"
          disabled={salvando}
          onClick={() => {
            const mapa = mapaDe(linhas)
            if (confirmarSeVazio(mapa)) sincronizar.mutate(mapa)
          }}
        >
          <RefreshCw
            className={sincronizar.isPending ? 'animate-spin' : undefined}
            aria-hidden
          />
          {sincronizar.isPending
            ? t('entity.edit.ids.syncing')
            : t('entity.edit.ids.saveAndReenrich')}
        </Button>
      </div>
    </section>
  )
}
