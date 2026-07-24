import { useRef, useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import {
  Clock,
  Download,
  ExternalLink,
  ImageOff,
  Library,
  LoaderCircle,
  Music,
  Pause,
  Play,
  Star,
  X,
} from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  ApiError,
  assetFileSrc,
  importExternal,
  themeSrc,
  type Asset,
  type CreditCast,
  type CreditCompany,
  type CreditCrew,
  type ExternalCollection,
  type ExternalSimilar,
  type ExternalVideo,
  type ImportRequest,
} from '@/lib/api'
import { assetsPreferidosDoHero, selosDisponibilidade } from '@/lib/artwork'
import { rotuloTipo } from '@/lib/entity-badges'
import { internalIdDe } from '@/lib/internal-id'
import { rotuloIdioma } from '@/lib/languages'
import { assetsQueryOptions, entityThemeQueryOptions } from '@/lib/queries'
import {
  rotuloSituacao,
  type RichDetail,
  type RotuloSituacao,
} from '@/lib/rich-detail'
import { cn } from '@/lib/utils'

// RichDetailView é a tela RICA COMPARTILHADA entre o detalhe do item EXTERNO (antes
// de importar, 0022/0023) e o detalhe da ENTITY importada (catálogo). As duas telas
// têm o MESMO padrão visual e de informação — hero IMERSIVO full-bleed (backdrop +
// overlay + pôster + sinopse, briefing 0082), barra de ações acima do hero, gêneros,
// palavras-chave, vídeos, coleção, similares e links — e diferem só nos SLOTS: a
// externa traz o botão Importar; a entity traz os selos de override, o editor, a
// galeria/arquivos e (via `entityId`) a logo/banner/disco preferidos e o player do
// tema musical local. O backend serve o MESMO shape rico nos dois DTOs (internal/
// richfields), então um único adapter normaliza cada um em `RichDetail` e o
// componente é agnóstico à origem.

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

// StillFrame renderiza o STILL 16:9 do EPISÓDIO (0042) no topo do detalhe: a imagem já
// vem servida pelo image proxy (busca+cacheia sob demanda) e ocupa um frame `aspect-
// video` próprio — não o pôster 2:3 —, porque o still é paisagem. `loading="lazy"` +
// `onError` caem no placeholder (como o PosterImg). Sem still (src null) mostra o
// placeholder no mesmo frame. Só o detalhe do episódio o renderiza.
export function StillFrame({ src, alt }: { src: string | null; alt: string }) {
  const [falhou, setFalhou] = useState(false)
  const mostrarPlaceholder = !src || falhou
  return (
    <div className="relative aspect-video w-full overflow-hidden bg-muted">
      {mostrarPlaceholder ? (
        <div
          data-testid="still-placeholder"
          className="flex h-full w-full items-center justify-center text-muted-foreground"
        >
          <ImageOff className="size-8" aria-hidden />
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

// HeroBackdropFundo é o FUNDO full-bleed do hero imersivo (0082) para os tipos
// COM pôster (tudo, exceto episódio — que usa o StillFrame acima no lugar): uma
// caixa com altura em torno de 40% da viewport (`min-h`, não mais o `aspect-[16/6]`
// fixo do 0079), preenchida pela imagem (`object-cover`) ou, sem backdrop/em erro,
// pelo placeholder (`ImageOff`, paridade com PosterImg/StillFrame). O degradê de
// legibilidade é aplicado pelo CALLER (uma única camada por cima de qualquer
// fundo — imagem, placeholder ou nenhum) para não duplicar o overlay por ramo.
function HeroBackdropFundo({ src }: { src: string | null }) {
  const [falhou, setFalhou] = useState(false)
  const mostrarPlaceholder = !src || falhou
  return (
    <div className="relative h-full min-h-[32vh] w-full bg-muted sm:min-h-[40vh]">
      {mostrarPlaceholder ? (
        <div
          data-testid="backdrop-placeholder"
          className="absolute inset-0 flex items-center justify-center text-muted-foreground"
        >
          <ImageOff className="size-10" aria-hidden />
        </div>
      ) : (
        <img
          src={src ?? undefined}
          alt=""
          aria-hidden
          loading="lazy"
          onError={() => setFalhou(true)}
          className="absolute inset-0 h-full w-full object-cover"
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
  label = 'Importar',
}: {
  source: string
  sourceId: string
  type: string
  size?: 'sm' | 'default' | 'lg'
  label?: string
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
        {importMutation.isPending ? 'Importando…' : label}
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

// ehYouTube diz se o vídeo pode ser EMBUTIDO: precisa ser do YouTube e ter `key`
// (a `url` é dispensável — o embed é montado da key). Só esses viram player local;
// os demais caem no link externo (quando têm url).
function ehYouTube(video: ExternalVideo): boolean {
  return video.site === 'YouTube' && !!video.key
}

// YouTubeEmbedFrame é o <iframe> do domínio `youtube-nocookie` (sem cookies de
// rastreio) compartilhado pelo player CLICK-TO-PLAY da `VideosRail` (VideoCard) e
// pelo botão "Assistir trailer" do hero (0082, `TrailerModal` abaixo) — um único
// lugar que monta a URL de embed, nunca duplicado entre os dois usos.
function YouTubeEmbedFrame({
  videoKey,
  title,
}: {
  videoKey: string
  title: string
}) {
  return (
    <iframe
      src={`https://www.youtube-nocookie.com/embed/${videoKey}?autoplay=1`}
      title={title}
      allow="autoplay; encrypted-media; picture-in-picture"
      allowFullScreen
      className="absolute inset-0 h-full w-full border-0"
    />
  )
}

// VideoCard renderiza um vídeo. Para o YouTube, é um player CLICK-TO-PLAY: mostra o
// thumbnail com botão de play e, ao clicar, troca pelo `YouTubeEmbedFrame`. Vídeos
// não-YouTube (raros no TMDB) mantêm o link externo. Não pré-carregamos o iframe:
// evita N players e o tráfego/rastreio do YouTube antes de o dono querer assistir.
function VideoCard({ video }: { video: ExternalVideo }) {
  const [tocando, setTocando] = useState(false)

  if (!ehYouTube(video)) {
    // Não-embutível: link externo. A `VideosRail` só encaminha para cá vídeos com
    // `url` (o filtro é a fonte única da verdade), então aqui a url está presente.
    return (
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
    )
  }

  return (
    <figure className="flex flex-col gap-1.5">
      <div className="relative aspect-video overflow-hidden rounded-xl bg-muted ring-1 ring-foreground/10">
        {tocando ? (
          <YouTubeEmbedFrame videoKey={video.key} title={video.name} />
        ) : (
          <button
            type="button"
            onClick={() => setTocando(true)}
            aria-label={`Reproduzir ${video.name}`}
            className="group absolute inset-0 h-full w-full cursor-pointer"
          >
            <img
              src={`https://img.youtube.com/vi/${video.key}/hqdefault.jpg`}
              alt=""
              aria-hidden
              loading="lazy"
              className="h-full w-full object-cover"
            />
            <span className="absolute inset-0 flex items-center justify-center bg-black/30 transition group-hover:bg-black/40">
              <span className="flex size-14 items-center justify-center rounded-full bg-background/90 text-foreground shadow-lg transition group-hover:scale-105">
                <Play className="size-6 fill-current" aria-hidden />
              </span>
            </span>
          </button>
        )}
      </div>
      <figcaption className="flex items-center gap-2 text-sm">
        <span className="line-clamp-1 font-medium">{video.name}</span>
        <Badge variant="outline" className="shrink-0">
          {video.type}
        </Badge>
      </figcaption>
    </figure>
  )
}

function VideosRail({ videos }: { videos: ExternalVideo[] }) {
  // Renderável = embutível (YouTube+key) OU com link externo. Os demais somem.
  const renderaveis = videos.filter((v) => ehYouTube(v) || v.url)
  if (renderaveis.length === 0) {
    return null
  }
  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-medium">Vídeos</h2>
      <ul className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {renderaveis.map((video) => (
          <li key={`${video.site}:${video.key}`}>
            <VideoCard video={video} />
          </li>
        ))}
      </ul>
    </section>
  )
}

// primeiroTrailer acha o primeiro vídeo REPRODUZÍVEL (`ehYouTube`) do tipo
// "Trailer" (convenção TMDB) — só trailer de verdade, sem fallback para Teaser/
// Clip (briefing 0082 §4.3). `null` quando não há nenhum.
function primeiroTrailer(videos: ExternalVideo[]): ExternalVideo | null {
  return videos.find((v) => v.type === 'Trailer' && ehYouTube(v)) ?? null
}

// TrailerModal é o overlay de tela cheia que abre ao clicar em "Assistir trailer"
// na barra de ações do hero (0082): reusa o MESMO `YouTubeEmbedFrame` da
// `VideosRail`/`VideoCard` (sem duplicar a lógica de embed), já em modo tocando (o
// clique no botão já expressa a intenção — sem o passo extra de clicar no
// thumbnail). Fecha ao clicar fora ou no X.
function TrailerModal({
  video,
  onClose,
}: {
  video: ExternalVideo
  onClose: () => void
}) {
  return (
    <div
      role="dialog"
      aria-label={`Trailer de ${video.name}`}
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-4"
      onClick={onClose}
    >
      <div
        className="relative aspect-video w-full max-w-3xl overflow-hidden rounded-xl bg-black shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <YouTubeEmbedFrame videoKey={video.key} title={video.name} />
      </div>
      <button
        type="button"
        onClick={onClose}
        aria-label="Fechar trailer"
        className="absolute right-4 top-4 flex size-10 items-center justify-center rounded-full bg-background/90 text-foreground shadow-lg"
      >
        <X className="size-5" aria-hidden />
      </button>
    </div>
  )
}

// TrailerButton é o botão "Assistir trailer" da barra de ações (0082 §4.3): some
// quando não há trailer reproduzível (`primeiroTrailer` null) — sem fallback para
// Teaser/Clip.
function TrailerButton({ videos }: { videos: ExternalVideo[] }) {
  const [aberto, setAberto] = useState(false)
  const trailer = primeiroTrailer(videos)
  if (!trailer) {
    return null
  }
  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setAberto(true)}>
        <Play aria-hidden />
        Assistir trailer
      </Button>
      {aberto ? (
        <TrailerModal video={trailer} onClose={() => setAberto(false)} />
      ) : null}
    </>
  )
}

// ColecaoBand mostra a coleção a que o item pertence. Interno-first (0026): quando a
// coleção JÁ é entity (`internal_id`), o card linka para `/entities/{id}` (o destino
// INTERNO, centralizado); senão, para a página externa da coleção (`/collection/
// $source/$id`, 0024). O card inteiro é o destino, com o mesmo visual nos dois casos.
function ColecaoBand({
  source,
  colecao,
}: {
  source: string
  colecao: ExternalCollection
}) {
  const internalId = internalIdDe(colecao)
  const classeCard =
    'group flex items-center gap-3 rounded-xl bg-card p-3 ring-1 ring-foreground/10 transition hover:ring-foreground/30'
  const conteudo = (
    <>
      <PosterImg
        src={colecao.poster_url}
        alt={`Pôster da coleção ${colecao.name}`}
        className="w-16 shrink-0"
      />
      <span className="font-medium group-hover:underline">{colecao.name}</span>
    </>
  )
  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-medium">Coleção</h2>
      {internalId != null ? (
        <Link
          to="/entities/$id"
          params={{ id: internalId }}
          className={classeCard}
        >
          {conteudo}
        </Link>
      ) : (
        <Link
          to="/collection/$source/$id"
          params={{ source, id: colecao.source_id }}
          className={classeCard}
        >
          {conteudo}
        </Link>
      )}
    </section>
  )
}

// SimilaresRail: cada card navega ao similar. Interno-first (0026, estendido no 0027):
// quando o similar JÁ é entity (`internal_id`), o card linka para `/entities/{id}` (o
// destino INTERNO, centralizado) e ganha o selo "Na biblioteca"; senão, para o detalhe
// externo do alvo (`/external/$source/$type/$id`), navegável e sem catalogar (o 0022
// persiste na 1ª visita). Vale para as duas telas (externo e entity).
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
            <SimilarCard source={source} item={item} />
          </li>
        ))}
      </ul>
    </section>
  )
}

// SimilarCard renderiza UM similar. O conteúdo (pôster + título + ano) é idêntico nos
// dois destinos — só a rota muda (interno × externo) e o item interno exibe o selo
// "Na biblioteca". Extraído para não duplicar o markup entre os dois ramos do Link.
function SimilarCard({
  source,
  item,
}: {
  source: string
  item: ExternalSimilar
}) {
  const internalId = internalIdDe(item)
  const conteudo = (
    <>
      <PosterImg
        src={item.poster_url}
        alt={`Pôster de ${item.title}`}
        className="transition group-hover:ring-2 group-hover:ring-foreground/30"
      />
      <span className="line-clamp-2 text-xs font-medium group-hover:text-foreground">
        {item.title}
      </span>
      {internalId != null ? (
        <Badge variant="secondary" className="w-fit">
          Na biblioteca
        </Badge>
      ) : item.year ? (
        <span className="text-xs text-muted-foreground">{item.year}</span>
      ) : null}
    </>
  )
  if (internalId != null) {
    return (
      <Link
        to="/entities/$id"
        params={{ id: internalId }}
        className="group flex flex-col gap-1"
      >
        {conteudo}
      </Link>
    )
  }
  return (
    <Link
      to="/external/$source/$type/$id"
      params={{ source, type: item.type, id: item.source_id }}
      className="group flex flex-col gap-1"
    >
      {conteudo}
    </Link>
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

// FaixaStatusPoster é a faixa compacta de SITUAÇÃO (0045/0079) ancorada na base do
// pôster, no padrão visual do TMDB (que mostra o status junto da capa, não solto no
// bloco de texto). Só aparece quando `situacao` existe (série/temporada enriquecida);
// filme/coleção sem `airing_status` seguem sem faixa — mesmo gating de antes, só que
// visualmente movido do bloco de metadados para o pôster. Sobrepõe via `absolute`
// num wrapper `relative` em volta do `PosterImg` (que não ganha prop nova).
function FaixaStatusPoster({ situacao }: { situacao: RotuloSituacao | null }) {
  if (!situacao) {
    return null
  }
  return (
    <div className="absolute inset-x-0 bottom-0 rounded-b-xl bg-gradient-to-t from-black/85 via-black/55 to-transparent px-2 pt-6 pb-1.5">
      <span className="block truncate text-center text-[11px] font-semibold tracking-wide text-white">
        {situacao.label}
      </span>
    </div>
  )
}

// NotaMedalhao é o "medalhão" circular da nota do hero (0079), no padrão visual do
// círculo de avaliação do TMDB — SVG simples com `stroke-dasharray`, sem dependência
// nova. Prioriza a nota LOCAL do dono (ADR-0012, contorno âmbar); o CALLER decide o
// fallback para a nota da FONTE (contorno neutro) quando não há nota local — o mesmo
// comportamento de antes (ícone de estrela + número), só que neste componente.
function NotaMedalhao({
  value,
  variant,
}: {
  value: number
  variant: 'local' | 'fonte'
}) {
  const ehLocal = variant === 'local'
  const raio = 16
  const circunferencia = 2 * Math.PI * raio
  const pctPreenchido = Math.max(0, Math.min(1, value / 10))
  const tracoPreenchido = pctPreenchido * circunferencia
  return (
    <div
      className="relative flex size-11 shrink-0 items-center justify-center"
      aria-label={ehLocal ? `Sua nota: ${value.toFixed(1)}` : undefined}
      title={ehLocal ? 'Sua nota' : 'Nota da fonte'}
    >
      <svg viewBox="0 0 40 40" className="size-11 -rotate-90" aria-hidden>
        <circle
          cx="20"
          cy="20"
          r={raio}
          strokeWidth="4"
          fill="none"
          className="stroke-muted"
        />
        <circle
          cx="20"
          cy="20"
          r={raio}
          strokeWidth="4"
          strokeLinecap="round"
          fill="none"
          strokeDasharray={`${tracoPreenchido} ${circunferencia}`}
          className={ehLocal ? 'stroke-amber-500' : 'stroke-foreground'}
        />
      </svg>
      <span className="absolute text-xs font-semibold text-foreground">
        {value.toFixed(1)}
      </span>
    </div>
  )
}

// SelosArtes é a FAIXA de disponibilidade do card do topo (0049): lê o
// `available: {kind: count}` do endpoint `/artwork` (0048) e mostra um selo por kind
// que EXISTE, com rótulo pt-BR + contagem (ex.: "Pôster 23 · Fanart 40 · Logo 17").
// Vazio/ausente (filme sem inventário, tela do externo) → null: a faixa não aparece.
function SelosArtes({
  available,
}: {
  available: Record<string, number> | undefined
}) {
  const selos = selosDisponibilidade(available)
  if (selos.length === 0) {
    return null
  }
  return (
    <div className="flex flex-wrap items-center gap-1.5 pt-1">
      {selos.map((selo) => (
        <Badge key={selo.kind} variant="outline">
          {selo.label} {selo.count}
        </Badge>
      ))}
    </div>
  )
}

// ── Logo, banner, disco e tema musical (0082) ────────────────────────────────
//
// As três artes e o tema abaixo só aparecem na tela da ENTITY (via `entityId`,
// passado só por `/entities/$id`) — o externo (antes de importar) não tem assets
// próprios nem pasta local ainda (§4.2/§4.4 do briefing).

// LogoHero substitui o TÍTULO EM TEXTO pela logo preferida (`kind=logo`, 0048),
// sobreposta ao backdrop no estilo Jellyfin. Sem logo, o CALLER cai no `<h1>` de
// sempre — mesmo comportamento de hoje, sem regressão.
function LogoHero({ logo, alt }: { logo: Asset; alt: string }) {
  return (
    <img
      src={assetFileSrc(logo.url)}
      alt={alt}
      loading="lazy"
      className="max-h-16 w-auto max-w-full object-contain drop-shadow-lg sm:max-h-24"
    />
  )
}

// BannerFaixa mostra o banner preferido (`kind=banner`) como uma faixa larga e
// baixa — só quando existe; sem gating por tipo além de "existe ou não".
function BannerFaixa({ banner }: { banner: Asset }) {
  return (
    <img
      src={assetFileSrc(banner.url)}
      alt=""
      aria-hidden
      loading="lazy"
      className="aspect-[1000/185] w-56 shrink-0 overflow-hidden rounded-md object-cover shadow-lg sm:w-72"
    />
  )
}

// DiscoGiratorio é o disco (`kind=disc`) decorativo perto do pôster, com rotação
// contínua lenta em CSS puro (`animate-spin`, arbitrário para uma volta a cada 9s —
// bem mais lenta que o spinner padrão) — `motion-reduce:animate-none` desliga a
// animação para quem pediu `prefers-reduced-motion: reduce` (variante nativa do
// Tailwind, sem JS extra). Só aparece com asset preferido do kind.
function DiscoGiratorio({ disc }: { disc: Asset }) {
  return (
    <div
      title="Disco"
      className="absolute -bottom-3 -right-3 size-14 overflow-hidden rounded-full shadow-lg ring-2 ring-background sm:size-16"
    >
      <img
        src={assetFileSrc(disc.url)}
        alt=""
        aria-hidden
        loading="lazy"
        className="h-full w-full animate-spin object-cover [animation-duration:9s] motion-reduce:animate-none"
      />
    </div>
  )
}

// TemaMusicalPlayer é o player compacto do tema local (`theme.mp3`/`.ogg`/`.flac`,
// briefing 0082 §4.4): a query (`entityThemeQueryOptions`, HEAD — sem baixar o
// binário inteiro) confere a existência UMA vez por carga da tela; sem tema
// (404/erro), o componente não renderiza nada — sem erro, sem espaço reservado.
// Play/pause é SEMPRE uma ação do dono (nenhum navegador autoplay áudio com som, e
// seria intrusivo mesmo se permitisse); o `loop` fica ligado no elemento o tempo
// todo (só importa enquanto toca).
function TemaMusicalPlayer({ entityId }: { entityId: string }) {
  const { data: temTema } = useQuery(entityThemeQueryOptions(entityId))
  const audioRef = useRef<HTMLAudioElement>(null)
  const [tocando, setTocando] = useState(false)

  if (!temTema) {
    return null
  }

  function alternar() {
    const audio = audioRef.current
    if (!audio) {
      return
    }
    if (tocando) {
      audio.pause()
    } else {
      void audio.play()
    }
  }

  return (
    <div className="flex items-center gap-2">
      <Button
        type="button"
        variant="outline"
        size="sm"
        onClick={alternar}
        aria-label={tocando ? 'Pausar tema musical' : 'Tocar tema musical'}
      >
        {tocando ? <Pause aria-hidden /> : <Music aria-hidden />}
        {tocando ? 'Pausar tema' : 'Tocar tema'}
      </Button>
      <audio
        ref={audioRef}
        src={themeSrc(entityId)}
        loop
        onPlay={() => setTocando(true)}
        onPause={() => setTocando(false)}
      />
    </div>
  )
}

// ── Créditos: elenco, equipe e produtoras (0029, fase B, read-only) ──────────

// TETO_ELENCO: quantos atores o Elenco mostra ANTES do "Ver todos". O backend
// persiste ~top 50 por bilhagem; a UI abre com os ~24 mais bilhados e expande sob
// demanda (grades enormes — filme com 76, série com 781 — não valem o scroll inicial).
const TETO_ELENCO = 24

// Base da página da pessoa no TMDB. Nesta fase o card é, no máximo, um link para a
// fonte (a página da pessoa própria é follow-up — briefing §7). Os `source_id` dos
// créditos vêm do TMDB (credits/aggregate_credits), então o link é válido.
const TMDB_PESSOA = 'https://www.themoviedb.org/person/'

// sufixoEpisodios formata a contagem de episódios do crédito (só série): "12 eps"
// (singular "1 ep"). Ausente/zero/filme → null (a UI não mostra o sufixo).
function sufixoEpisodios(n: number | null | undefined): string | null {
  if (!n || n <= 0) {
    return null
  }
  return `${n} ${n === 1 ? 'ep' : 'eps'}`
}

// SeloNaBiblioteca é o mesmo selo interno-first já usado em coleção/similares
// (`Badge variant="secondary"`, texto "Na biblioteca") — aqui compacto (px/py/texto
// menores) para caber no card apertado do elenco, com o ícone `Library` reforçando o
// sentido sem repetir texto. Puramente decorativo (`aria-hidden`): o destino já é
// anunciado pelo `aria-label` do Link que o envolve.
function SeloNaBiblioteca() {
  return (
    <Badge
      variant="secondary"
      className="w-fit gap-1 px-1.5 py-0 text-[10px]"
      aria-hidden
    >
      <Library className="size-3" aria-hidden />
      Na biblioteca
    </Badge>
  )
}

// PessoaCard é o card reusável de pessoa (foto 2:3 + nome + papel), usado na grade do
// Elenco. Reusa o `PosterImg` (lazy + placeholder). A foto já vem PROXYADA do backend
// (uso direto, sem reproxyar). Em série, o papel ganha o sufixo "· N eps" (episódios
// do personagem).
//
// Interno-first (0029, fase D): quando o crédito traz `personId` (a pessoa já foi
// persistida por algum import), o card linka para a página INTERNA da pessoa
// (`/person/{id}`) e ganha o selo "Na biblioteca" (mesmo padrão do similar/coleção,
// 0026/0027). Sem `personId`, o card degrada para o link externo de sempre (TMDB, nova
// aba), sem selo — comportamento inalterado.
function PessoaCard({
  sourceId,
  name,
  profileUrl,
  papel,
  eps,
  personId,
}: {
  sourceId: string
  name: string
  profileUrl: string | null
  papel: string | null
  eps: number | null | undefined
  personId?: string | null
}) {
  const sufEps = sufixoEpisodios(eps)
  const conteudo = (
    <>
      <PosterImg
        src={profileUrl}
        alt={`Foto de ${name}`}
        className="transition group-hover:ring-2 group-hover:ring-foreground/30"
      />
      <span className="line-clamp-2 text-xs font-medium group-hover:text-foreground">
        {name}
      </span>
      {papel ? (
        <span className="line-clamp-2 text-xs text-muted-foreground">
          {papel}
          {sufEps ? ` · ${sufEps}` : null}
        </span>
      ) : sufEps ? (
        <span className="text-xs text-muted-foreground">{sufEps}</span>
      ) : null}
      {personId ? <SeloNaBiblioteca /> : null}
    </>
  )
  if (personId) {
    return (
      <Link
        to="/person/$personId"
        params={{ personId }}
        aria-label={`Ver ${name} na biblioteca`}
        className="group flex flex-col gap-1"
      >
        {conteudo}
      </Link>
    )
  }
  return (
    <a
      href={`${TMDB_PESSOA}${sourceId}`}
      target="_blank"
      rel="noreferrer"
      className="group flex flex-col gap-1"
    >
      {conteudo}
    </a>
  )
}

// ElencoGrid mostra o elenco em grade, na ordem de bilhagem (`order`). Abre com os
// TETO_ELENCO mais bilhados; havendo mais, um botão "Ver todos (N)" expande a lista
// inteira (e "Ver menos" recolhe). Vazio → a seção some.
function ElencoGrid({ cast }: { cast: CreditCast[] }) {
  const [expandido, setExpandido] = useState(false)
  if (cast.length === 0) {
    return null
  }
  // Ordena por bilhagem (menor `order` = mais alto). O backend já entrega ordenado,
  // mas reforçamos aqui para a exibição não depender disso. `slice()` mantém a fonte
  // intacta; o sort é estável no V8.
  const ordenado = [...cast].sort((a, b) => a.order - b.order)
  const temMais = ordenado.length > TETO_ELENCO
  const visiveis = expandido ? ordenado : ordenado.slice(0, TETO_ELENCO)
  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-medium">Elenco</h2>
      <ul className="grid grid-cols-3 gap-3 sm:grid-cols-4 md:grid-cols-6">
        {visiveis.map((p, i) => (
          <li key={`${p.source_id}:${i}`}>
            <PessoaCard
              sourceId={p.source_id}
              name={p.name}
              profileUrl={p.profile_url ?? null}
              papel={p.character || null}
              eps={p.episode_count}
              personId={p.person_id ?? null}
            />
          </li>
        ))}
      </ul>
      {temMais ? (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="w-fit"
          onClick={() => setExpandido((v) => !v)}
        >
          {expandido ? 'Ver menos' : `Ver todos (${ordenado.length})`}
        </Button>
      ) : null}
    </section>
  )
}

// Departamentos que aparecem PRIMEIRO na Equipe (Direção, depois Roteiro),
// reconhecidos por chaves normalizadas em pt-BR e en (a fonte pode não traduzir o
// rótulo). Os demais departamentos saem em ordem alfabética.
const PRIORIDADE_DEPARTAMENTO: string[][] = [
  ['direção', 'direcao', 'directing', 'direction'],
  ['roteiro', 'escrita', 'writing'],
]

// rankDepartamento devolve a posição de prioridade do departamento (0 = primeiro).
// Departamento fora da lista → rank alto (ordenado alfabeticamente entre si depois).
function rankDepartamento(dep: string): number {
  const norm = dep.trim().toLowerCase()
  const i = PRIORIDADE_DEPARTAMENTO.findIndex((chaves) => chaves.includes(norm))
  return i === -1 ? PRIORIDADE_DEPARTAMENTO.length : i
}

interface GrupoEquipe {
  department: string
  membros: CreditCrew[]
}

// agruparEquipe agrupa a equipe por `department` preservando a ordem de chegada dos
// membros dentro do grupo, e ordena os grupos: Direção/Roteiro primeiro, demais em
// ordem alfabética (pt-BR). Departamento vazio cai num grupo "Equipe".
function agruparEquipe(crew: CreditCrew[]): GrupoEquipe[] {
  const mapa = new Map<string, CreditCrew[]>()
  for (const m of crew) {
    const dep = m.department?.trim() || 'Equipe'
    const lista = mapa.get(dep)
    if (lista) {
      lista.push(m)
    } else {
      mapa.set(dep, [m])
    }
  }
  return [...mapa.entries()]
    .map(([department, membros]) => ({ department, membros }))
    .sort((a, b) => {
      const ra = rankDepartamento(a.department)
      const rb = rankDepartamento(b.department)
      return ra !== rb
        ? ra - rb
        : a.department.localeCompare(b.department, 'pt-BR')
    })
}

// MembroNome é o nome do membro de equipe, linkado. Interno-first (0029, fase D):
// com `personId`, linka para a página INTERNA da pessoa (com um ícone `Library`
// discreto e `aria-label` descritivo); sem `personId`, degrada para o link externo de
// sempre (TMDB, nova aba) — comportamento inalterado. Extraído do `EquipeGrupos` para
// não inflar o JSX da lista.
function MembroNome({
  name,
  sourceId,
  personId,
}: {
  name: string
  sourceId: string
  personId?: string | null
}) {
  if (personId) {
    return (
      <Link
        to="/person/$personId"
        params={{ personId }}
        aria-label={`Ver ${name} na biblioteca`}
        className="inline-flex items-center gap-1 font-medium hover:underline"
      >
        <Library className="size-3 text-muted-foreground" aria-hidden />
        {name}
      </Link>
    )
  }
  return (
    <a
      href={`${TMDB_PESSOA}${sourceId}`}
      target="_blank"
      rel="noreferrer"
      className="font-medium hover:underline"
    >
      {name}
    </a>
  )
}

// grupoTemFoto decide se o DEPARTAMENTO ganha foto na Equipe (0076): os mesmos
// priorizados no agrupamento (`PRIORIDADE_DEPARTAMENTO` — Direção/Roteiro),
// reaproveitando `rankDepartamento` sem alterar essa lista/lógica em si. Curadoria
// deliberada — não é TODOS os ~40 nomes de equipe (pesaria a página sem ganho
// proporcional; ver briefing 0076 §6), só os papéis que o dono reconhece de cara,
// no mesmo padrão visual do Elenco.
function grupoTemFoto(department: string): boolean {
  return rankDepartamento(department) < PRIORIDADE_DEPARTAMENTO.length
}

// GrupoEquipeFotos mostra UM departamento priorizado como grade de cartões com
// foto — reusa o MESMO `PessoaCard` do `ElencoGrid` (paridade visual completa,
// inclusive selo "Na biblioteca" e degradê gracioso sem `profile_url`).
function GrupoEquipeFotos({ grupo }: { grupo: GrupoEquipe }) {
  return (
    <div className="flex flex-col gap-2">
      <h3 className="text-sm font-medium text-foreground/70">
        {grupo.department}
      </h3>
      <ul className="grid grid-cols-3 gap-3 sm:grid-cols-4 md:grid-cols-6">
        {grupo.membros.map((m, i) => (
          <li key={`${m.source_id}:${m.job}:${i}`}>
            <PessoaCard
              sourceId={m.source_id}
              name={m.name}
              profileUrl={m.profile_url ?? null}
              papel={m.job || null}
              eps={m.episode_count}
              personId={m.person_id ?? null}
            />
          </li>
        ))}
      </ul>
    </div>
  )
}

// GrupoEquipeTexto é a lista textual compacta ORIGINAL (nome + cargo, com "· N
// eps" na série) — inalterada, só extraída para alternar com `GrupoEquipeFotos`.
function GrupoEquipeTexto({ grupo }: { grupo: GrupoEquipe }) {
  return (
    <div className="flex flex-col gap-1 sm:flex-row sm:gap-3">
      <h3 className="shrink-0 text-sm font-medium text-foreground/70 sm:w-40">
        {grupo.department}
      </h3>
      <ul className="flex flex-wrap gap-x-3 gap-y-0.5 text-sm">
        {grupo.membros.map((m, i) => {
          const sufEps = sufixoEpisodios(m.episode_count)
          return (
            <li key={`${m.source_id}:${m.job}:${i}`}>
              <MembroNome
                name={m.name}
                sourceId={m.source_id}
                personId={m.person_id}
              />
              <span className="text-muted-foreground">
                {' '}
                — {m.job}
                {sufEps ? ` · ${sufEps}` : null}
              </span>
            </li>
          )
        })}
      </ul>
    </div>
  )
}

// EquipeGrupos mostra a equipe agrupada por departamento: os priorizados
// (Direção/Roteiro) em grade com foto (0076, paridade com o Elenco); os demais em
// lista textual compacta, como já era. Vazio → a seção some.
function EquipeGrupos({ crew }: { crew: CreditCrew[] }) {
  if (crew.length === 0) {
    return null
  }
  const grupos = agruparEquipe(crew)
  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-medium">Equipe</h2>
      <div className="flex flex-col gap-4">
        {grupos.map((grupo) =>
          grupoTemFoto(grupo.department) ? (
            <GrupoEquipeFotos key={grupo.department} grupo={grupo} />
          ) : (
            <GrupoEquipeTexto key={grupo.department} grupo={grupo} />
          ),
        )}
      </div>
    </section>
  )
}

// ProdutorasBand mostra as produtoras/estúdios numa faixa: logo (já proxyado) num
// chip de fundo claro (para logos escuros aparecerem no tema escuro) ou o nome num
// `<Badge>` quando não há logo. Vazio → a seção some.
function ProdutorasBand({ companies }: { companies: CreditCompany[] }) {
  if (companies.length === 0) {
    return null
  }
  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-medium">Produtoras</h2>
      <ul className="flex flex-wrap items-center gap-3">
        {companies.map((c, i) => (
          <li key={`${c.source_id}:${i}`}>
            {c.logo_url ? (
              <span className="flex h-10 items-center justify-center rounded-md bg-white px-3 ring-1 ring-foreground/10">
                <img
                  src={c.logo_url}
                  alt={c.name}
                  loading="lazy"
                  className="max-h-8 w-auto object-contain"
                />
              </span>
            ) : (
              <Badge variant="outline">{c.name}</Badge>
            )}
          </li>
        ))}
      </ul>
    </section>
  )
}

// BarraAcoesHero é a barra de ações ACIMA do hero (0082 §4.1): os MESMOS botões de
// hoje (`actions` — Importar no externo, `EntityActionButtons` na entity), agora
// reposicionados para o TOPO da tela, mais o botão "Assistir trailer" (§4.3),
// quando houver. Sem nenhum dos dois, a barra inteira some (sem faixa vazia).
function BarraAcoesHero({
  actions,
  videos,
}: {
  actions?: ReactNode
  videos: ExternalVideo[]
}) {
  if (!actions && !primeiroTrailer(videos)) {
    return null
  }
  return (
    <div className="flex flex-wrap items-center justify-end gap-2">
      <TrailerButton videos={videos} />
      {actions}
    </div>
  )
}

// HeroImersivo é o hero FULL-BLEED (0082): o fundo (backdrop/still) e a camada de
// conteúdo (pôster + texto) ocupam a MESMA célula de um grid de uma linha só —
// técnica robusta de "sobrepor sem `position: absolute`" que deixa a ALTURA da
// linha ser o maior dos dois (o fundo tem uma altura MÍNIMA de ~40% da viewport;
// o conteúdo cresce livremente além disso quando o texto é longo, sem cortar
// nada — diferente de conteúdo `absolute`, que não empurra a altura do pai). A
// largura bleeda até a borda da TELA (`w-screen` + margem negativa de 50vw), mas o
// conteúdo interno volta a ficar centralizado no `max-w-4xl` de sempre. Um
// degradê escurece o fundo por trás do texto (contraste AA em claro/escuro,
// critério do 0079 mantido).
function HeroImersivo({
  detail,
  situacao,
  titleBadges,
  descriptionExtra,
  genresExtra,
  availableArtwork,
  logo,
  banner,
  disc,
  entityId,
}: {
  detail: RichDetail
  situacao: RotuloSituacao | null
  titleBadges?: ReactNode
  descriptionExtra?: ReactNode
  genresExtra?: ReactNode
  availableArtwork?: Record<string, number>
  logo?: Asset
  banner?: Asset
  disc?: Asset
  entityId?: string
}) {
  return (
    <div className="relative left-1/2 right-1/2 grid w-screen -mx-[50vw]">
      {/* Fundo: Episódio (0042) usa o STILL 16:9 no lugar do backdrop (o pôster
          some — episódio não tem); os demais tipos usam o backdrop, com altura
          mínima de ~40vh (HeroBackdropFundo) mesmo sem imagem (placeholder).
          `z-0`/`z-10`/`z-20` explícitos (em vez de confiar na ordem do DOM)
          garantem a pilha fundo→degradê→conteúdo sem ambiguidade entre os
          itens do grid sobrepostos. */}
      <div className="relative z-0 col-start-1 row-start-1">
        {detail.type === 'episode' ? (
          <StillFrame src={detail.stillUrl} alt={`Still de ${detail.title}`} />
        ) : (
          <HeroBackdropFundo src={detail.backdropUrl} />
        )}
      </div>

      {/* Overlay: degradê vertical (o texto mora na base) + horizontal em telas
          largas (mais escuro à esquerda, no padrão Jellyfin/TMDB). O tom sólido
          no fim do degradê é o `background` do tema — garante AA em claro e
          escuro sem depender da imagem por trás. */}
      <div className="relative z-10 col-start-1 row-start-1 bg-gradient-to-t from-background via-background/75 to-background/10" />
      <div className="relative z-10 col-start-1 row-start-1 hidden bg-gradient-to-r from-background/90 via-background/25 to-transparent sm:block" />

      {/* Conteúdo: re-centralizado no max-w-4xl de sempre, ancorado na base da
          célula do grid (`items-end`) — cresce para cima sem cortar quando o
          texto é mais alto que a altura mínima do fundo. */}
      <div className="relative z-20 col-start-1 row-start-1 flex items-end">
        <div className="mx-auto flex w-full max-w-4xl flex-col gap-4 px-4 py-6 sm:flex-row sm:items-end sm:gap-6 sm:py-8">
          {detail.type === 'episode' ? null : (
            <div className="relative w-40 shrink-0 sm:w-56">
              <PosterImg
                src={detail.posterUrl}
                alt={`Pôster de ${detail.title}`}
              />
              <FaixaStatusPoster situacao={situacao} />
              {disc ? <DiscoGiratorio disc={disc} /> : null}
            </div>
          )}
          <div className="flex min-w-0 flex-col gap-2">
            {banner ? <BannerFaixa banner={banner} /> : null}

            {/* Linha do título: a LOGO (0082, quando preferida) substitui o texto
                do título — sem ela, cai no `<h1>` de sempre (sem regressão). Ano
                e tipo seguem ao lado, como antes. */}
            <div className="flex flex-wrap items-end gap-2">
              {logo ? (
                <LogoHero logo={logo} alt={detail.title} />
              ) : (
                <h1 className="font-heading text-2xl font-semibold text-foreground sm:text-3xl">
                  {detail.title}
                </h1>
              )}
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

            {/* Gêneros (0079): logo abaixo do título — texto/badges compactos,
                sem heading "Gêneros" própria. */}
            {detail.genres.length > 0 ? (
              <div className="flex flex-wrap items-center gap-1.5">
                {detail.genres.map((g) => (
                  <Badge key={g} variant="outline" className="font-normal">
                    {g}
                  </Badge>
                ))}
                {genresExtra}
              </div>
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
              {detail.originalLanguage ? (
                <span className="text-sm text-muted-foreground">
                  {rotuloIdioma(detail.originalLanguage)}
                </span>
              ) : null}
            </div>

            {/* Nota (0079): medalhão circular. Prioriza a nota LOCAL do dono
                (ADR-0012, contorno âmbar); sem ela, cai na nota da FONTE
                (contorno neutro). Com as DUAS, a da fonte segue ao lado. */}
            {detail.localRating != null || detail.voteAverage ? (
              <div className="flex flex-wrap items-center gap-2 pt-1">
                {detail.localRating != null ? (
                  <NotaMedalhao value={detail.localRating} variant="local" />
                ) : detail.voteAverage ? (
                  <NotaMedalhao value={detail.voteAverage} variant="fonte" />
                ) : null}
                {detail.localRating != null && detail.voteAverage ? (
                  <span
                    className="inline-flex items-center gap-1 text-sm text-muted-foreground"
                    title="Nota da fonte"
                  >
                    <Star className="size-4" aria-hidden />
                    {detail.voteAverage.toFixed(1)}
                  </span>
                ) : null}
              </div>
            ) : null}

            {detail.tagline ? (
              <p className="text-base font-medium italic text-foreground/80">
                “{detail.tagline}”
              </p>
            ) : null}

            {/* Sinopse (0082): move para DENTRO do hero — não é mais uma seção
                separada abaixo do card, sem heading própria (parágrafo
                proeminente sobre o overlay, no padrão Jellyfin/TMDB). */}
            {detail.description ? (
              <p className="max-w-3xl text-sm text-foreground/90 sm:text-base">
                {detail.description}
              </p>
            ) : null}
            {descriptionExtra}

            {entityId ? <TemaMusicalPlayer entityId={entityId} /> : null}

            <SelosArtes available={availableArtwork} />
          </div>
        </div>
      </div>
    </div>
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
  availableArtwork,
  entityId,
  children,
}: {
  detail: RichDetail
  // source da fonte (ex.: 'tmdb') para montar os links dos similares.
  source: string
  // Área de "voltar" (Link ou botão) — cada tela decide o destino.
  back?: ReactNode
  // Selos ao lado do título (status, "editado") — entity.
  titleBadges?: ReactNode
  // Ações da barra ACIMA do hero (Importar no externo; EntityActionButtons na
  // entity) — o trailer entra ao lado, calculado aqui (0082).
  actions?: ReactNode
  // Anotações após a sinopse (selos de fallback/editado) — entity.
  descriptionExtra?: ReactNode
  // Anotações após os gêneros — entity.
  genresExtra?: ReactNode
  // Contagem de artes por kind (0049): a faixa de selos de disponibilidade no hero.
  // Só a tela da ENTITY passa (lê `/artwork`); a do externo omite (sem a faixa).
  availableArtwork?: Record<string, number>
  // Id INTERNO da entity (0082): habilita a logo/banner/disco preferidos (lê
  // `GET .../assets`) e o player do tema musical local (`GET .../theme`) — só a
  // tela da ENTITY passa; o externo (antes de importar) não tem assets/pasta
  // local ainda, então omite (sem regressão, mesmo gating de `availableArtwork`).
  entityId?: string
  // Seções extras anexadas (editor de override, galeria, arquivos) — entity.
  children?: ReactNode
}) {
  // Selo de SITUAÇÃO da série (0045): só existe quando a fonte informou a situação
  // (série enriquecida). Para filme/episódio o campo é ausente → `situacao` é null e
  // o selo não aparece — sem gating por tipo, só por presença do dado.
  const situacao = rotuloSituacao(detail.airingStatus)

  // Logo/banner/disco preferidos (0082 §4.2): a MESMA chamada a `GET .../assets`
  // que a galeria (art-gallery.tsx) usa, aqui só para extrair os três kinds do
  // hero. `enabled: !!entityId` mantém a tela do externo sem chamada alguma
  // (sem assets próprios antes de importar).
  const assetsQuery = useQuery({
    ...assetsQueryOptions(entityId ?? ''),
    enabled: !!entityId,
  })
  const heroAssets = assetsPreferidosDoHero(
    entityId ? assetsQuery.data : undefined,
  )

  return (
    <div className="flex flex-col gap-6">
      {back}

      <BarraAcoesHero actions={actions} videos={detail.videos} />

      <HeroImersivo
        detail={detail}
        situacao={situacao}
        titleBadges={titleBadges}
        descriptionExtra={descriptionExtra}
        genresExtra={genresExtra}
        availableArtwork={availableArtwork}
        logo={heroAssets.logo}
        banner={heroAssets.banner}
        disc={heroAssets.disc}
        entityId={entityId}
      />

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
      {detail.collection ? (
        <ColecaoBand source={source} colecao={detail.collection} />
      ) : null}
      <SimilaresRail source={source} similares={detail.similar} />
      <ElencoGrid cast={detail.cast} />
      <EquipeGrupos crew={detail.crew} />
      <ProdutorasBand companies={detail.companies} />
      <LinksExternos links={detail.externalLinks} />

      {children}
    </div>
  )
}
