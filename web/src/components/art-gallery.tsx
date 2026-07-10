import { useRef, useState, type ChangeEvent } from 'react'
import {
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from '@tanstack/react-query'
import { Star, Trash2, Upload } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Select } from '@/components/ui/select'
import { EmptyState, ErrorState, LoadingState } from '@/components/query-states'
import {
  ApiError,
  assetFileSrc,
  deleteAsset,
  preferAsset,
  uploadAsset,
  KINDS_ARTE,
  MIMES_ARTE,
  type Asset,
  type KindArte,
} from '@/lib/api'
import { assetsQueryOptions } from '@/lib/queries'

// Galeria de arte da entity (briefing 0017): miniaturas com selo de origem (`sua`
// para o dono × `externa` para o cache) e selo `preferida`; upload do dono
// (`origin=user`), com validação de tipo NO CLIENTE antes de enviar; ações
// "Preferir" (desempata entre as do dono) e "Remover" (só nas do dono).
//
// Precedência de serviço do pôster (calculada no backend): preferida → user →
// external → 404. Subir arte já a faz vencer a externa; marcar preferida só
// desempata. Por isso, ao mudar a arte, invalidamos TAMBÉM `['entity', id]` e
// `['entities']` — o pôster do detalhe e do grid refletem a nova escolha.

// Rótulo pt-BR da origem: só `user` (do dono) e o resto (externo/cache). O
// backend também emite `local` (scanner); tratamos tudo que não é `user` como
// "externa" para a UI (nenhuma dessas é removível pelo dono).
function ehDoDono(asset: Asset): boolean {
  return asset.origin === 'user'
}

// Invalidação após mudar a arte: três chaves independentes. `['assets', id]`
// recarrega a galeria; `['entity', id]` (PREFIXO) derruba o detalhe em todas as
// variações de `lang`; `['entities']` atualiza os cards do grid — todos apontam
// para o endpoint de pôster, que agora serve a nova precedência.
function invalidarAposArte(qc: QueryClient, id: string) {
  qc.invalidateQueries({ queryKey: ['assets', id] })
  qc.invalidateQueries({ queryKey: ['entity', id] })
  qc.invalidateQueries({ queryKey: ['entities'] })
}

// Aceite do <input file> e da validação no cliente — o mesmo conjunto que o
// backend valida pelos magic bytes.
const ACCEPT = MIMES_ARTE.join(',')

export function ArtGallery({ entityId }: { entityId: string }) {
  const qc = useQueryClient()
  const { data, isPending, isError, error } = useQuery(
    assetsQueryOptions(entityId),
  )
  const [kind, setKind] = useState<KindArte>('poster')
  // Erro de validação no cliente (tipo não suportado) — distinto do erro da API.
  const [erroCliente, setErroCliente] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  const enviar = useMutation({
    mutationFn: ({ file, kind: k }: { file: File; kind: KindArte }) =>
      uploadAsset(entityId, file, k),
    onSuccess: () => invalidarAposArte(qc, entityId),
  })

  const preferir = useMutation({
    mutationFn: (assetId: string) => preferAsset(entityId, assetId),
    onSuccess: () => invalidarAposArte(qc, entityId),
  })

  const remover = useMutation({
    mutationFn: (assetId: string) => deleteAsset(entityId, assetId),
    onSuccess: () => invalidarAposArte(qc, entityId),
  })

  // Validação NO CLIENTE: rejeita o que não é imagem suportada ANTES de enviar
  // (erro inline, sem bater na API). O `<input accept>` só filtra o diálogo; a
  // checagem explícita cobre drag-and-drop e navegadores que ignoram o accept.
  function onEscolherArquivo(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    // Permite reescolher o mesmo arquivo depois (reseta o value do input).
    e.target.value = ''
    if (!file) {
      return
    }
    if (!(MIMES_ARTE as readonly string[]).includes(file.type)) {
      setErroCliente('Tipo de imagem nao suportado (use PNG, JPEG ou WebP).')
      return
    }
    setErroCliente(null)
    enviar.mutate({ file, kind })
  }

  // Erro exibido inline: o do cliente tem prioridade; senão a `message` pt-BR do
  // envelope (415/413/422); senão um genérico das mutações.
  const mensagemErro =
    erroCliente ??
    (enviar.error instanceof ApiError
      ? enviar.error.message
      : preferir.error instanceof ApiError
        ? preferir.error.message
        : remover.error instanceof ApiError
          ? remover.error.message
          : enviar.isError || preferir.isError || remover.isError
            ? 'Nao foi possivel atualizar a arte.'
            : null)

  const ocupado = enviar.isPending || preferir.isPending || remover.isPending

  return (
    <section className="mt-6 flex flex-col gap-3">
      <h2 className="font-heading text-lg font-medium">Arte</h2>

      {/* Upload: seletor de kind + input de arquivo. */}
      <div className="flex flex-wrap items-end gap-2 rounded-xl bg-card p-4 ring-1 ring-foreground/10">
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-muted-foreground">Tipo</span>
          <Select
            value={kind}
            onChange={(e) => setKind(e.target.value as KindArte)}
            aria-label="Tipo de arte"
            className="w-full sm:w-36"
          >
            {KINDS_ARTE.map((k) => (
              <option key={k} value={k}>
                {k === 'poster' ? 'Poster' : 'Fanart'}
              </option>
            ))}
          </Select>
        </label>
        <input
          ref={inputRef}
          type="file"
          accept={ACCEPT}
          aria-label="Arquivo de arte"
          className="hidden"
          onChange={onEscolherArquivo}
        />
        <Button
          type="button"
          onClick={() => inputRef.current?.click()}
          disabled={ocupado}
        >
          <Upload aria-hidden />
          {enviar.isPending ? 'Enviando…' : 'Enviar arte'}
        </Button>
      </div>

      {mensagemErro ? (
        <p role="alert" className="text-sm font-medium text-destructive">
          {mensagemErro}
        </p>
      ) : null}

      {isPending ? (
        <LoadingState label="Carregando arte…" />
      ) : isError ? (
        <ErrorState
          title="Nao foi possivel carregar a arte"
          message={error instanceof Error ? error.message : undefined}
        />
      ) : data.length === 0 ? (
        <EmptyState
          title="Nenhuma arte ainda"
          description="Envie um poster ou fanart para esta entity."
        />
      ) : (
        <ul className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4">
          {data.map((asset) => (
            <li
              key={asset.id}
              className="flex flex-col gap-2 rounded-xl bg-card p-2 ring-1 ring-foreground/10"
            >
              <div className="overflow-hidden rounded-lg bg-muted">
                <img
                  src={assetFileSrc(asset.url)}
                  alt={`${asset.kind} ${ehDoDono(asset) ? 'sua' : 'externa'}`}
                  className="aspect-[2/3] w-full object-cover"
                />
              </div>
              <div className="flex flex-wrap items-center gap-1.5">
                <Badge variant="outline">{asset.kind}</Badge>
                <Badge variant={ehDoDono(asset) ? 'default' : 'secondary'}>
                  {ehDoDono(asset) ? 'sua' : 'externa'}
                </Badge>
                {asset.preferred ? (
                  <Badge variant="secondary">preferida</Badge>
                ) : null}
              </div>
              <div className="flex flex-wrap gap-1.5">
                {!asset.preferred ? (
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => preferir.mutate(asset.id)}
                    disabled={ocupado}
                  >
                    <Star aria-hidden />
                    Preferir
                  </Button>
                ) : null}
                {ehDoDono(asset) ? (
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    aria-label={`Remover arte ${asset.kind}`}
                    onClick={() => remover.mutate(asset.id)}
                    disabled={ocupado}
                  >
                    <Trash2 aria-hidden />
                    Remover
                  </Button>
                ) : null}
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
