import { useRef, useState, type ChangeEvent } from 'react'
import {
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from '@tanstack/react-query'
import { Star, Upload } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Select } from '@/components/ui/select'
import { ErrorState } from '@/components/query-states'
import { ArtLightbox, type AcoesLightbox } from '@/components/art-lightbox'
import {
  ApiError,
  arteSrc,
  deleteAsset,
  kindsParaTipo,
  preferAsset,
  selectArtwork,
  uploadAsset,
  MIMES_ARTE,
  type ArtworkItem,
  type KindArte,
  type SelectArtworkRequest,
} from '@/lib/api'
import {
  infoKind,
  larguraKind,
  linhasDeArtwork,
  rotuloKind,
  type LinhaArte,
} from '@/lib/artwork'
import { artworkQueryOptions } from '@/lib/queries'

// Galeria de artes (0049, reescrita do 0017/0044): consome o endpoint AGRUPADO
// `GET /artwork` (0048) e renderiza UMA LINHA (carrossel horizontal) por kind
// presente, cada card no ASPECT correto do tipo (pôster 2:3, fanart/thumb/still
// 16:9, logo/clearart wide transparente, disc/character quadrado, banner faixa).
// É AGNÓSTICA DE FONTE: TMDB, fanart.tv e assets do dono entram nas mesmas linhas;
// a origem vira só um selo. Clicar numa arte abre o lightbox (alta + zoom +
// navegação + ações). O dono ainda pode ENVIAR uma arte custom (upload do 0017).
//
// Ações e roteamento (contrato 0048):
//   • Definir preferida → asset LOCAL: PUT .../assets/{asset_id}/prefer;
//     INVENTÁRIO (não-local): POST .../artwork/select {url: src_url, prefer:true}.
//   • Baixar (materializar sem preferir) → POST .../artwork/select {prefer:false}.
//   • Remover (só do DONO, source==='user') → DELETE .../assets/{asset_id}.

// Aceite do <input file> e da validação no cliente (mesmo conjunto do backend).
const ACCEPT = MIMES_ARTE.join(',')

// Invalidação após mudar a arte: a galeria/selos (`['artwork', id]`), o detalhe
// (`['entity', id]`, prefixo — todas as variações de lang) e o grid (`['entities']`)
// — todos leem o pôster servido, que segue a nova precedência.
function invalidarAposArte(qc: QueryClient, id: string) {
  qc.invalidateQueries({ queryKey: ['artwork', id] })
  qc.invalidateQueries({ queryKey: ['entity', id] })
  qc.invalidateQueries({ queryKey: ['entities'] })
}

// Estado do lightbox: o kind aberto e o índice dentro dele (ou null = fechado).
interface Aberto {
  kind: string
  index: number
}

// LinhaGaleria renderiza UMA linha (kind): rótulo + scroll horizontal de cards no
// aspect do tipo. Cada card é um botão que abre o lightbox naquele índice. A arte
// preferida ganha anel + selo. Extraído para o markup do card não duplicar (jscpd).
function LinhaGaleria({
  linha,
  onAbrir,
}: {
  linha: LinhaArte
  onAbrir: (kind: string, index: number) => void
}) {
  const info = infoKind(linha.kind)
  const ajuste = info.contain ? 'object-contain' : 'object-cover'
  const fundo = info.contain ? 'bg-muted/40' : 'bg-muted'
  return (
    <section className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <h3 className="text-sm font-medium">{rotuloKind(linha.kind)}</h3>
        <Badge variant="secondary">{linha.items.length}</Badge>
      </div>
      <ul className="flex gap-3 overflow-x-auto pb-2">
        {linha.items.map((item, i) => (
          <li key={`${item.url}:${i}`} className="shrink-0">
            <button
              type="button"
              onClick={() => onAbrir(linha.kind, i)}
              aria-label={`Ver ${rotuloKind(linha.kind)} ${i + 1}${
                item.is_preferred ? ' (preferida)' : ''
              }`}
              className={`group relative block ${larguraKind(linha.kind)} overflow-hidden rounded-lg ${fundo} ring-1 transition ${
                item.is_preferred
                  ? 'ring-2 ring-amber-500'
                  : 'ring-foreground/10 hover:ring-foreground/30'
              }`}
            >
              <img
                src={item.url}
                alt={`${rotuloKind(linha.kind)} ${i + 1}`}
                loading="lazy"
                className={`${info.aspect} w-full ${ajuste}`}
              />
              {item.is_preferred ? (
                <span className="absolute left-1.5 top-1.5 flex size-6 items-center justify-center rounded-full bg-amber-500 text-white shadow">
                  <Star className="size-3.5 fill-current" aria-hidden />
                </span>
              ) : null}
            </button>
          </li>
        ))}
      </ul>
    </section>
  )
}

export function ArtGallery({
  entityId,
  type = 'movie',
  stillUrl = null,
}: {
  entityId: string
  // Tipo da entity: decide os kinds oferecidos no UPLOAD (episódio → só `still`) e
  // se há um still do provider a exibir. Default `movie` — compat.
  type?: string
  // Still do EPISÓDIO vindo do detalhe (0044): CRU do provider ou rota local do
  // custom. Card read-only "provider" quando o inventário ainda não tem still.
  stillUrl?: string | null
}) {
  const qc = useQueryClient()
  const { data, isPending, isError, error } = useQuery(
    artworkQueryOptions(entityId),
  )

  // Upload do dono (0017): kinds conforme o tipo; erro de validação no cliente.
  const kindsUpload = kindsParaTipo(type)
  const [kindUpload, setKindUpload] = useState<KindArte>(kindsUpload[0])
  const [erroCliente, setErroCliente] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  // Lightbox aberto (kind + índice) ou null.
  const [aberto, setAberto] = useState<Aberto | null>(null)

  const enviar = useMutation({
    mutationFn: ({ file, kind }: { file: File; kind: KindArte }) =>
      uploadAsset(entityId, file, kind),
    onSuccess: () => invalidarAposArte(qc, entityId),
  })

  // Definir preferida: asset LOCAL desempata via prefer; inventário baixa+prefere.
  const preferir = useMutation({
    mutationFn: ({ kind, item }: { kind: string; item: ArtworkItem }) =>
      item.is_local && item.asset_id
        ? preferAsset(entityId, item.asset_id)
        : selectArtwork(entityId, {
            kind: kind as SelectArtworkRequest['kind'],
            url: item.src_url ?? item.url,
            prefer: true,
          }),
    onSuccess: () => invalidarAposArte(qc, entityId),
  })

  // Baixar (materializar sem preferir) — só arte de inventário tem `src_url`.
  const baixar = useMutation({
    mutationFn: ({ kind, item }: { kind: string; item: ArtworkItem }) =>
      selectArtwork(entityId, {
        kind: kind as SelectArtworkRequest['kind'],
        url: item.src_url ?? item.url,
        prefer: false,
      }),
    onSuccess: () => invalidarAposArte(qc, entityId),
  })

  // Remover (só do dono): DELETE do asset.
  const remover = useMutation({
    mutationFn: ({ item }: { kind: string; item: ArtworkItem }) =>
      deleteAsset(entityId, item.asset_id ?? ''),
    onSuccess: () => invalidarAposArte(qc, entityId),
  })

  function onEscolherArquivo(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    e.target.value = ''
    if (!file) {
      return
    }
    if (!(MIMES_ARTE as readonly string[]).includes(file.type)) {
      setErroCliente('Tipo de imagem nao suportado (use PNG, JPEG ou WebP).')
      return
    }
    setErroCliente(null)
    enviar.mutate({ file, kind: kindUpload })
  }

  const ocupado = preferir.isPending || baixar.isPending || remover.isPending

  // Erro das AÇÕES do lightbox (pt-BR do envelope) — exibido no rodapé do lightbox.
  const erroAcao =
    preferir.error instanceof ApiError
      ? preferir.error.message
      : baixar.error instanceof ApiError
        ? baixar.error.message
        : remover.error instanceof ApiError
          ? remover.error.message
          : preferir.isError || baixar.isError || remover.isError
            ? 'Nao foi possivel atualizar a arte.'
            : null

  // Erro do UPLOAD (cliente > envelope > genérico).
  const erroUpload =
    erroCliente ??
    (enviar.error instanceof ApiError
      ? enviar.error.message
      : enviar.isError
        ? 'Nao foi possivel enviar a arte.'
        : null)

  const linhas = linhasDeArtwork(data)
  // Still do PROVIDER (episódio, 0044): card read-only enquanto o inventário não
  // tem still (nem do dono, nem baixado). Mostrado só no episódio, com stillUrl.
  const temStill = linhas.some((l) => l.kind === 'still')
  const mostrarProviderStill = type === 'episode' && !temStill && !!stillUrl

  // Linha aberta no lightbox (recalculada a cada render — reflete o pós-mutação).
  const linhaAberta = aberto
    ? linhas.find((l) => l.kind === aberto.kind)
    : undefined
  const acoes: AcoesLightbox = {
    onPreferir: (item) =>
      aberto && preferir.mutate({ kind: aberto.kind, item }),
    onBaixar: (item) => aberto && baixar.mutate({ kind: aberto.kind, item }),
    onRemover: (item) => aberto && remover.mutate({ kind: aberto.kind, item }),
    ocupado,
    erro: erroAcao,
  }

  return (
    <section className="mt-6 flex flex-col gap-4">
      <h2 className="font-heading text-lg font-medium">Artes</h2>

      {/* Upload do dono: seletor de kind + input de arquivo (0017). */}
      <div className="flex flex-wrap items-end gap-2 rounded-xl bg-card p-4 ring-1 ring-foreground/10">
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-muted-foreground">Tipo</span>
          <Select
            value={kindUpload}
            onChange={(e) => setKindUpload(e.target.value as KindArte)}
            aria-label="Tipo de arte"
            className="w-full sm:w-36"
          >
            {kindsUpload.map((k) => (
              <option key={k} value={k}>
                {rotuloKind(k)}
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
          disabled={enviar.isPending}
        >
          <Upload aria-hidden />
          {enviar.isPending ? 'Enviando…' : 'Enviar arte'}
        </Button>
      </div>

      {erroUpload ? (
        <p role="alert" className="text-sm font-medium text-destructive">
          {erroUpload}
        </p>
      ) : null}

      {isPending ? (
        // Skeleton: uma linha de placeholders (a galeria carrega em bloco).
        <div className="flex flex-col gap-2" aria-hidden>
          <div className="h-4 w-24 animate-pulse rounded bg-muted" />
          <div className="flex gap-3">
            {[0, 1, 2, 3].map((i) => (
              <div
                key={i}
                className="aspect-[2/3] w-52 shrink-0 animate-pulse rounded-lg bg-muted"
              />
            ))}
          </div>
        </div>
      ) : isError ? (
        <ErrorState
          title="Nao foi possivel carregar as artes"
          message={error instanceof Error ? error.message : undefined}
        />
      ) : linhas.length === 0 && !mostrarProviderStill ? (
        <p className="text-sm text-muted-foreground">
          {type === 'episode'
            ? 'Nenhuma arte ainda. Envie um still para este episódio.'
            : 'Nenhuma arte disponível ainda.'}
        </p>
      ) : (
        <div className="flex flex-col gap-5">
          {/* Still do provider (episódio): read-only, não abre lightbox nem tem ações. */}
          {mostrarProviderStill ? (
            <section className="flex flex-col gap-2">
              <div className="flex items-center gap-2">
                <h3 className="text-sm font-medium">{rotuloKind('still')}</h3>
                <Badge variant="outline">provider</Badge>
              </div>
              <div className="w-80 overflow-hidden rounded-lg bg-muted ring-1 ring-foreground/10">
                <img
                  src={arteSrc(stillUrl) ?? undefined}
                  alt="Still do provider"
                  loading="lazy"
                  className="aspect-video w-full object-cover"
                />
              </div>
            </section>
          ) : null}
          {linhas.map((linha) => (
            <LinhaGaleria
              key={linha.kind}
              linha={linha}
              onAbrir={(kind, index) => setAberto({ kind, index })}
            />
          ))}
        </div>
      )}

      {/* Lightbox: só quando há linha aberta (some sozinho se a linha esvaziar). */}
      {aberto && linhaAberta ? (
        <ArtLightbox
          kind={aberto.kind}
          items={linhaAberta.items}
          index={Math.min(aberto.index, linhaAberta.items.length - 1)}
          onIndex={(i) => setAberto({ kind: aberto.kind, index: i })}
          onClose={() => setAberto(null)}
          acoes={acoes}
        />
      ) : null}
    </section>
  )
}
