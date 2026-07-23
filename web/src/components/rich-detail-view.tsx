import { useState, type ReactNode } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import {
  Clock,
  Download,
  ExternalLink,
  ImageOff,
  Library,
  LoaderCircle,
  Play,
  Star,
} from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  ApiError,
  importExternal,
  type CreditCast,
  type CreditCompany,
  type CreditCrew,
  type ExternalCollection,
  type ExternalSimilar,
  type ExternalVideo,
  type ImportRequest,
} from '@/lib/api'
import { rotuloTipo } from '@/lib/entity-badges'
import { internalIdDe } from '@/lib/internal-id'
import { rotuloIdioma } from '@/lib/languages'
import { rotuloSituacao, type RichDetail } from '@/lib/rich-detail'
import { selosDisponibilidade } from '@/lib/artwork'
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

// BackdropImg é o BANNER 16:9 largo do hero (fanart) com degradê para o `bg-card`.
// Paridade com o `PosterImg` (0044): `loading="lazy"` + `onError` caem no placeholder
// (ícone `ImageOff`) em vez de sumir silenciosamente se a URL falha. A `src` já vem
// PRONTA (via `arteSrc` — proxy p/ crua, base p/ rota local). Só renderiza quando há
// backdrop (o hero decide); por isso a `src` aqui é sempre uma string.
function BackdropImg({ src }: { src: string }) {
  const [falhou, setFalhou] = useState(false)
  if (falhou) {
    return (
      <div className="relative aspect-[16/6] w-full sm:aspect-[16/5]">
        <div
          data-testid="backdrop-placeholder"
          className="flex h-full w-full items-center justify-center bg-muted text-muted-foreground"
        >
          <ImageOff className="size-8" aria-hidden />
        </div>
        <div className="absolute inset-0 bg-gradient-to-t from-card via-card/40 to-transparent" />
      </div>
    )
  }
  return (
    <div className="relative aspect-[16/6] w-full sm:aspect-[16/5]">
      <img
        src={src}
        alt=""
        aria-hidden
        loading="lazy"
        onError={() => setFalhou(true)}
        className="h-full w-full object-cover"
      />
      <div className="absolute inset-0 bg-gradient-to-t from-card via-card/40 to-transparent" />
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

// VideoCard renderiza um vídeo. Para o YouTube, é um player CLICK-TO-PLAY: mostra o
// thumbnail com botão de play e, ao clicar, troca por um <iframe> embutido do domínio
// `youtube-nocookie` (sem cookies de rastreio até o play). Vídeos não-YouTube (raros
// no TMDB) mantêm o link externo. Não pré-carregamos o iframe: evita N players e o
// tráfego/rastreio do YouTube antes de o dono querer assistir.
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
          <iframe
            src={`https://www.youtube-nocookie.com/embed/${video.key}?autoplay=1`}
            title={video.name}
            allow="autoplay; encrypted-media; picture-in-picture"
            allowFullScreen
            className="absolute inset-0 h-full w-full border-0"
          />
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
  // Contagem de artes por kind (0049): a faixa de selos de disponibilidade no hero.
  // Só a tela da ENTITY passa (lê `/artwork`); a do externo omite (sem a faixa).
  availableArtwork?: Record<string, number>
  // Seções extras anexadas (editor de override, galeria, arquivos) — entity.
  children?: ReactNode
}) {
  // Selo de SITUAÇÃO da série (0045): só existe quando a fonte informou a situação
  // (série enriquecida). Para filme/episódio o campo é ausente → `situacao` é null e
  // o selo não aparece — sem gating por tipo, só por presença do dado.
  const situacao = rotuloSituacao(detail.airingStatus)
  return (
    <div className="flex flex-col gap-6">
      {back}

      {/* Hero: o backdrop é um BANNER no topo com degradê para o `bg-card`; o
          conteúdo (pôster + texto) fica sobre o card SÓLIDO abaixo. Assim o texto
          nunca compete com a arte — o contraste dos metadados é o do tema (AA), em
          claro e escuro — e o backdrop segue cinematográfico. Sem backdrop, é só o
          card com pôster + texto. */}
      <div className="overflow-hidden rounded-2xl bg-card ring-1 ring-foreground/10">
        {/* Episódio (0042): o STILL 16:9 é o herói do topo (via proxy, com placeholder),
            no lugar do backdrop — e o pôster 2:3 some (o episódio não tem pôster). Os
            demais tipos mantêm o backdrop cinematográfico + pôster 2:3 de sempre. */}
        {detail.type === 'episode' ? (
          <StillFrame src={detail.stillUrl} alt={`Still de ${detail.title}`} />
        ) : detail.backdropUrl ? (
          <BackdropImg src={detail.backdropUrl} />
        ) : null}
        <div className="flex flex-col gap-4 p-4 sm:flex-row sm:p-6">
          {detail.type === 'episode' ? null : (
            <PosterImg
              src={detail.posterUrl}
              alt={`Pôster de ${detail.title}`}
              className="w-32 shrink-0 sm:w-40"
            />
          )}
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
              {situacao ? (
                <Badge variant={situacao.variant}>{situacao.label}</Badge>
              ) : null}
              {detail.runtime ? (
                <span className="inline-flex items-center gap-1 text-sm text-muted-foreground">
                  <Clock className="size-4" aria-hidden />
                  {formatarDuracao(detail.runtime)}
                </span>
              ) : null}
              {/* Nota LOCAL do dono (ADR-0012): estrela CHEIA âmbar, destacada do
                  texto. Complementa a nota da fonte (que segue exibida ao lado). Usa
                  `!= null` para admitir a nota 0. */}
              {detail.localRating != null ? (
                <span
                  className="inline-flex items-center gap-1 text-sm font-medium text-foreground"
                  aria-label={`Sua nota: ${detail.localRating.toFixed(1)}`}
                  title="Sua nota"
                >
                  <Star
                    className="size-4 fill-amber-500 text-amber-500"
                    aria-hidden
                  />
                  {detail.localRating.toFixed(1)}
                </span>
              ) : null}
              {detail.voteAverage ? (
                <span
                  className="inline-flex items-center gap-1 text-sm text-muted-foreground"
                  title={
                    detail.localRating != null ? 'Nota da fonte' : undefined
                  }
                >
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
            <SelosArtes available={availableArtwork} />
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
