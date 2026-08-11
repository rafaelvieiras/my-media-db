import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { KeyRound, Plus, RefreshCw, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { ApiError, enrichEntity, updateExternalIds } from '@/lib/api'

// Uma linha editável da identidade externa: fonte (ex.: tmdb) + id NA FONTE. `key` é
// só para o React reconciliar as linhas ao adicionar/remover (não vai para a API).
interface Linha {
  key: number
  source: string
  value: string
}

// linhasDe transforma o mapa `external_ids` atual em linhas ordenadas por fonte
// (estável), cada uma com uma `key` própria.
function linhasDe(externalIds: Record<string, string>): Linha[] {
  return Object.entries(externalIds)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([source, value], i) => ({ key: i, source, value }))
}

// mapaDe monta o `{source: id}` a enviar: só linhas com fonte não-vazia; a última
// ocorrência de uma fonte repetida vence. O id vazio é MANTIDO (o backend o trata como
// "remover esta fonte" no full replace).
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

// mapaChave normaliza um mapa {source: id} numa string comparável (ordem de
// chaves não importa) — usada para detectar o estado "sujo" (0073).
function mapaChave(mapa: Record<string, string>): string {
  return JSON.stringify(
    Object.entries(mapa).sort(([a], [b]) => a.localeCompare(b)),
  )
}

// ExternalIdsEditor edita a IDENTIDADE EXTERNA da entity (`external_ids`, MD4, 0027): o
// dono fixa qual id de cada fonte a entity usa — base para sincronizar no futuro (os ids
// da fonte podem mudar, ou o item ser substituído por outro). Full replace: salvar grava
// o mapa inteiro; deixar um id em branco remove aquela fonte. Ao salvar, re-resolve a
// entity (invalida `['entity', id]`) e a lista (as colunas geradas mudam). Vale para
// qualquer type (é identidade). Erro (ApiError) inline.
export function ExternalIdsEditor({
  id,
  externalIds,
  onDirtyChange,
}: {
  id: string
  externalIds: Record<string, string>
  // Guard de "sair sem salvar" (0073): chamado só na transição do estado sujo.
  onDirtyChange?: (sujo: boolean) => void
}) {
  const queryClient = useQueryClient()
  const [linhas, setLinhas] = useState<Linha[]>(() => linhasDe(externalIds))
  const [proximaKey, setProximaKey] = useState(() => linhas.length)
  // Baseline do que está gravado (carregado ou salvo com sucesso) — comparado
  // com o mapa atual para saber se há algo sujo (0073).
  const [salvo, setSalvo] = useState(() => mapaDe(linhasDe(externalIds)))
  // Avisa quando a gravação da identidade fundiu uma duplicata automaticamente
  // (0051) — a entity sem investimento do dono some sozinha, sem o dono precisar
  // caçá-la na biblioteca.
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

  // Salva os ids E, em sucesso, re-enriquece pela fonte (0027): fecha o ciclo de
  // sincronização num clique — grava a identidade e já puxa os dados da fonte pelo id
  // recém-fixado (o enrich sem corpo lê `external_ids[tmdb]` da entity, por isso salva
  // ANTES). Uma falha do enrich (ex.: id não é da fonte) aparece inline, mas os ids já
  // ficaram salvos.
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

  // Guard (0098): salvar um mapa vazio é um PUT de full-replace que zera a
  // identidade da entity. Só pede confirmação quando é uma REMOÇÃO líquida
  // (havia algo salvo antes) — um item que nunca teve id nenhum pode salvar
  // vazio sem aviso, não é uma remoção.
  function confirmarSeVazio(mapaNovo: Record<string, string>): boolean {
    const removendoTudo =
      Object.keys(mapaNovo).length === 0 && Object.keys(salvo).length > 0
    if (!removendoTudo) return true
    return window.confirm(
      'Isso vai remover todos os IDs externos desta entity. Ela pode parar de casar com eventos de webhook e integrações externas até ser re-vinculada. Continuar?',
    )
  }

  const erroAtivo = sincronizar.error ?? mutation.error
  const mensagemErro =
    erroAtivo instanceof ApiError
      ? erroAtivo.message
      : mutation.isError || sincronizar.isError
        ? 'Não foi possível salvar os IDs externos.'
        : null

  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-col gap-1">
        <h2 className="flex items-center gap-2 font-heading text-lg font-medium">
          <KeyRound className="size-4" aria-hidden />
          IDs externos
        </h2>
        <p className="text-sm text-muted-foreground">
          Fixe o id de cada fonte (ex.: tmdb) para sincronizar este item no
          futuro. Deixe o id em branco para remover a fonte; depois você pode
          re-enriquecer.
        </p>
      </div>

      <div className="flex flex-col gap-2">
        {linhas.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            Nenhum id externo definido.
          </p>
        ) : (
          linhas.map((l) => (
            <div key={l.key} className="flex items-center gap-2">
              <Input
                value={l.source}
                onChange={(e) => alterar(l.key, 'source', e.target.value)}
                placeholder="fonte (ex.: tmdb)"
                aria-label="Fonte do id externo"
                className="max-w-[10rem]"
              />
              <Input
                value={l.value}
                onChange={(e) => alterar(l.key, 'value', e.target.value)}
                placeholder="id na fonte"
                aria-label={`Id na fonte ${l.source || '(nova)'}`}
              />
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label={`Remover ${l.source || 'fonte'}`}
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
          Duplicata mesclada automaticamente — o item já importado foi combinado
          com este.
        </p>
      ) : null}

      <div className="flex flex-wrap items-center gap-2">
        <Button type="button" variant="outline" size="sm" onClick={adicionar}>
          <Plus aria-hidden />
          Adicionar fonte
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
          {mutation.isPending ? 'Salvando…' : 'Salvar IDs'}
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
          {sincronizar.isPending ? 'Sincronizando…' : 'Salvar e re-enriquecer'}
        </Button>
      </div>
    </section>
  )
}
