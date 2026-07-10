import { useState, type ReactNode } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import {
  Clock,
  Download,
  ExternalLink,
  ImageOff,
  LoaderCircle,
  Play,
  Star,
} from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  ApiError,
  importExternal,
  type ExternalCollection,
  type ExternalSimilar,
  type ExternalVideo,
  type ImportRequest,
} from '@/lib/api'
import { rotuloTipo } from '@/lib/entity-badges'
import { rotuloIdioma } from '@/lib/languages'
import type { RichDetail } from '@/lib/rich-detail'
import { cn } from '@/lib/utils'

// RichDetailView é a tela RICA COMPARTILHADA entre o detalhe do item EXTERNO (antes
// de importar, 0022/0023) e o detalhe da ENTITY importada (catálogo). As duas telas
// têm o MESMO padrão visual e de informação — hero (backdrop+pôster), slogan em
// destaque, metadados, gêneros, palavras-chave, vídeos, coleção, similares e links —
// e diferem só nos SLOTS: a externa traz o botão Importar; a entity traz os selos de
// override, o editor e a galeria/arquivos. O backend serve o MESMO shape rico nos
// dois DTOs (internal/richfields), então um único adapter normaliza cada um em
// `RichDetail` e o componente é agnóstico à origem.

// formatarDuracao converte minutos em `2h 35min` (`45min` quando < 1h).
function formatarDuracao(min: number): string {
  const horas = Math.floor(min / 60)
  const resto = min % 60
  if (horas === 0) {
    return `${resto}min`
  }
  return resto === 0 ? `${horas}h` : `${horas}h ${resto}min`
}

// Rótulos de links externos conhecidos.
const ROTULO_LINK: Record<string, string> = { tmdb: 'TMDB', imdb: 'IMDb' }

// ── Pôster reutilizável ──────────────────────────────────────────────────────

// PosterImg é o pôster com placeholder, compartilhado pelo hero, coleção,
// similares e pelo card de busca. `src` é uma URL já pronta (direta ou proxied);
// erro/ausência cai no ícone. Centralizar mata a duplicação de markup (jscpd).
export function PosterImg({
  src,
  alt,
  className,
}: {
  src: string | null
  alt: string
  className?: string
}) {
  const [falhou, setFalhou] = useState(false)
  const mostrarPlaceholder = !src || falhou
  return (
    <div
      className={cn(
        'relative aspect-[2/3] overflow-hidden rounded-xl bg-muted',
        className,
      )}
    >
      {mostrarPlaceholder ? (
        <div
          data-testid="poster-placeholder"
          className="flex h-full w-full items-center justify-center text-muted-foreground"
        >
          <ImageOff className="size-6" aria-hidden />
        </div>
      ) : (
        <img
          src={src ?? undefined}
          alt={alt}
          loading="lazy"
          onError={() => setFalhou(true)}
          className="h-full w-full object-cover"
        />
      )}
    </div>
  )
}

// ── Botão Importar compartilhado ─────────────────────────────────────────────

// ImportarButton importa um item externo (find-or-create, 0015) e leva ao detalhe
// do catálogo. Compartilhado pelo card de busca e pela tela do externo. Dono da
// própria mutation; invalida `['entities']` e navega no sucesso; erro inline.
export function ImportarButton({
  source,
  sourceId,
  type,
  size = 'default',
}: {
  source: string
  sourceId: string
  type: string
  size?: 'sm' | 'default' | 'lg'
}) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const importMutation = useMutation({
    mutationFn: () =>
      importExternal({
        source,
        source_id: sourceId,
        type: type as ImportRequest['type'],
      }),
    onSuccess: (entity) => {
      queryClient.invalidateQueries({ queryKey: ['entities'] })
      navigate({ to: '/entities/$id', params: { id: entity.id } })
    },
  })
  const mensagemErro =
    importMutation.error instanceof ApiError
      ? importMutation.error.message
      : null

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Button
        size={size}
        disabled={importMutation.isPending}
        onClick={() => importMutation.mutate()}
      >
        {importMutation.isPending ? (
          <LoaderCircle className="animate-spin" aria-hidden />
        ) : (
          <Download aria-hidden />
        )}
        {importMutation.isPending ? 'Importando…' : 'Importar'}
      </Button>
      {mensagemErro ? (
        <span role="alert" className="text-sm font-medium text-destructive">
          {mensagemErro}
        </span>
      ) : null}
    </div>
  )
}

// ── Seções ricas ─────────────────────────────────────────────────────────────

function VideosRail({ videos }: { videos: ExternalVideo[] }) {
  const comLink = videos.filter((v) => v.url)
  if (comLink.length === 0) {
    return null
  }
  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-medium">Vídeos</h2>
      <ul className="flex flex-col gap-2">
        {comLink.map((video) => (
          <li key={`${video.site}:${video.key}`}>
            <a
              href={video.url ?? undefined}
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"
            >
              <Play className="size-4 shrink-0" aria-hidden />
              <span className="line-clamp-1">{video.name}</span>
              <Badge variant="outline" className="shrink-0">
                {video.type}
              </Badge>
            </a>
          </li>
        ))}
      </ul>
    </section>
  )
}

// ColecaoBand mostra a coleção a que o item pertence. É informativa nesta versão
// (abrir a coleção inteira como página é follow-up, §7 do 0023).
function ColecaoBand({ colecao }: { colecao: ExternalCollection }) {
  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-medium">Coleção</h2>
      <div className="flex items-center gap-3 rounded-xl bg-card p-3 ring-1 ring-foreground/10">
        <PosterImg
          src={colecao.poster_url}
          alt={`Pôster da coleção ${colecao.name}`}
          className="w-16 shrink-0"
        />
        <span className="font-medium">{colecao.name}</span>
      </div>
    </section>
  )
}

// SimilaresRail: cada card é um Link para a MESMA rota do detalhe externo do alvo
// (`/external/$source/$type/$id`), navegável e sem catalogar (o 0022 persiste na
// 1ª visita). Vale para as duas telas: os similares são sempre itens EXTERNOS.
function SimilaresRail({
  source,
  similares,
}: {
  source: string
  similares: ExternalSimilar[]
}) {
  if (similares.length === 0) {
    return null
  }
  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-medium">Similares</h2>
      <ul className="grid grid-cols-3 gap-3 sm:grid-cols-4 md:grid-cols-6">
        {similares.map((item) => (
          <li key={`${item.type}:${item.source_id}`}>
            <Link
              to="/external/$source/$type/$id"
              params={{ source, type: item.type, id: item.source_id }}
              className="group flex flex-col gap-1"
            >
              <PosterImg
                src={item.poster_url}
                alt={`Pôster de ${item.title}`}
                className="transition group-hover:ring-2 group-hover:ring-foreground/30"
              />
              <span className="line-clamp-2 text-xs font-medium group-hover:text-foreground">
                {item.title}
              </span>
              {item.year ? (
                <span className="text-xs text-muted-foreground">
                  {item.year}
                </span>
              ) : null}
            </Link>
          </li>
        ))}
      </ul>
    </section>
  )
}

function LinksExternos({ links }: { links: Record<string, string> }) {
  const entradas = Object.entries(links)
  if (entradas.length === 0) {
    return null
  }
  return (
    <section className="flex flex-wrap items-center gap-2">
      {entradas.map(([chave, url]) => (
        <a
          key={chave}
          href={url}
          target="_blank"
          rel="noreferrer"
          className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
        >
          <ExternalLink className="size-4" aria-hidden />
          {ROTULO_LINK[chave] ?? chave}
        </a>
      ))}
    </section>
  )
}

// ── Componente principal ─────────────────────────────────────────────────────

export function RichDetailView({
  detail,
  source,
  back,
  titleBadges,
  actions,
  descriptionExtra,
  genresExtra,
  children,
}: {
  detail: RichDetail
  // source da fonte (ex.: 'tmdb') para montar os links dos similares.
  source: string
  // Área de "voltar" (Link ou botão) — cada tela decide o destino.
  back?: ReactNode
  // Selos/ações ao lado do título (status, "editado", botão Editar) — entity.
  titleBadges?: ReactNode
  // Ação primária no hero (Importar) — externo.
  actions?: ReactNode
  // Anotações após a sinopse (selos de fallback/editado) — entity.
  descriptionExtra?: ReactNode
  // Anotações após os gêneros — entity.
  genresExtra?: ReactNode
  // Seções extras anexadas (editor de override, galeria, arquivos) — entity.
  children?: ReactNode
}) {
  return (
    <div className="flex flex-col gap-6">
      {back}

      {/* Hero: o backdrop é um BANNER no topo com degradê para o `bg-card`; o
          conteúdo (pôster + texto) fica sobre o card SÓLIDO abaixo. Assim o texto
          nunca compete com a arte — o contraste dos metadados é o do tema (AA), em
          claro e escuro — e o backdrop segue cinematográfico. Sem backdrop, é só o
          card com pôster + texto. */}
      <div className="overflow-hidden rounded-2xl bg-card ring-1 ring-foreground/10">
        {detail.backdropUrl ? (
          <div className="relative aspect-[16/6] w-full sm:aspect-[16/5]">
            <img
              src={detail.backdropUrl}
              alt=""
              aria-hidden
              className="h-full w-full object-cover"
            />
            <div className="absolute inset-0 bg-gradient-to-t from-card via-card/40 to-transparent" />
          </div>
        ) : null}
        <div className="flex flex-col gap-4 p-4 sm:flex-row sm:p-6">
          <PosterImg
            src={detail.posterUrl}
            alt={`Pôster de ${detail.title}`}
            className="w-32 shrink-0 sm:w-40"
          />
          <div className="flex min-w-0 flex-col gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="font-heading text-2xl font-semibold">
                {detail.title}
              </h1>
              {detail.year ? (
                <span className="text-lg text-muted-foreground">
                  {detail.year}
                </span>
              ) : null}
              <Badge variant="outline">{rotuloTipo(detail.type)}</Badge>
              {titleBadges}
            </div>
            {detail.originalTitle && detail.originalTitle !== detail.title ? (
              <p className="text-sm text-muted-foreground">
                {detail.originalTitle}
              </p>
            ) : null}
            {detail.tagline ? (
              <p className="text-base font-medium italic text-foreground/80">
                “{detail.tagline}”
              </p>
            ) : null}
            <div className="flex flex-wrap items-center gap-2 pt-1">
              {detail.certification ? (
                <Badge variant="secondary">{detail.certification}</Badge>
              ) : null}
              {detail.runtime ? (
                <span className="inline-flex items-center gap-1 text-sm text-muted-foreground">
                  <Clock className="size-4" aria-hidden />
                  {formatarDuracao(detail.runtime)}
                </span>
              ) : null}
              {detail.voteAverage ? (
                <span className="inline-flex items-center gap-1 text-sm text-muted-foreground">
                  <Star className="size-4" aria-hidden />
                  {detail.voteAverage.toFixed(1)}
                </span>
              ) : null}
              {detail.originalLanguage ? (
                <span className="text-sm text-muted-foreground">
                  {rotuloIdioma(detail.originalLanguage)}
                </span>
              ) : null}
            </div>
            {actions ? <div className="pt-2">{actions}</div> : null}
          </div>
        </div>
      </div>

      {detail.description ? (
        <section className="flex flex-col gap-2">
          <h2 className="font-heading text-lg font-medium">Sinopse</h2>
          <p className="text-sm text-muted-foreground">{detail.description}</p>
          {descriptionExtra}
        </section>
      ) : null}

      {detail.genres.length > 0 ? (
        <section className="flex flex-col gap-2">
          <h2 className="font-heading text-lg font-medium">Gêneros</h2>
          <div className="flex flex-wrap items-center gap-1.5">
            {detail.genres.map((g) => (
              <Badge key={g} variant="outline">
                {g}
              </Badge>
            ))}
            {genresExtra}
          </div>
        </section>
      ) : null}

      {detail.keywords.length > 0 ? (
        <section className="flex flex-col gap-2">
          <h2 className="font-heading text-lg font-medium">Palavras-chave</h2>
          <div className="flex flex-wrap gap-1.5">
            {detail.keywords.map((k) => (
              <Badge key={k} variant="secondary">
                {k}
              </Badge>
            ))}
          </div>
        </section>
      ) : null}

      <VideosRail videos={detail.videos} />
      {detail.collection ? <ColecaoBand colecao={detail.collection} /> : null}
      <SimilaresRail source={source} similares={detail.similar} />
      <LinksExternos links={detail.externalLinks} />

      {children}
    </div>
  )
}
