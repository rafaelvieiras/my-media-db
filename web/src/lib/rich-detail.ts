import {
  arteSrc,
  posterSrc,
  type ExternalCollection,
  type ExternalDetail,
  type ExternalSimilar,
  type ExternalVideo,
  type ResolvedEntity,
} from '@/lib/api'

// Modelo NORMALIZADO da tela rica compartilhada (RichDetailView) e os adapters das
// duas fontes. Fica fora do arquivo de componente para não disparar
// `react-refresh/only-export-components` (o módulo de componente só exporta
// componentes). O detalhe do externo (0022) e a entity resolvida (paridade
// 0022/0023) servem o MESMO shape rico no backend; aqui cada DTO vira `RichDetail`.

// RichDetail é o shape que a RichDetailView consome. Os campos traduzíveis já vêm
// resolvidos no idioma ativo (o backend resolve). URLs de imagem já saem PRONTAS
// para o `<img>` (o adapter aplica o proxy local no pôster da entity; o externo usa
// a URL direta da fonte).
export interface RichDetail {
  type: string
  title: string
  originalTitle: string | null
  description: string | null
  tagline: string | null
  year: string | null
  runtime: number | null
  certification: string | null
  voteAverage: number | null
  // Nota LOCAL do dono (ADR-0012): a avaliação própria, gravada no override, que
  // complementa/sobrepõe a nota da fonte. null quando não avaliada (ou na tela do
  // externo, que não tem nota local — a UI então mostra só a nota da fonte).
  localRating: number | null
  originalLanguage: string | null
  posterUrl: string | null
  backdropUrl: string | null
  // Still 16:9 do EPISÓDIO (0042), já pronto para o `<img>` (via image proxy). null
  // para não-episódios e para o detalhe do externo (que não tem still) — a UI só
  // renderiza o frame de still quando o tipo é `episode`.
  stillUrl: string | null
  genres: string[]
  keywords: string[]
  videos: ExternalVideo[]
  collection: ExternalCollection | null
  similar: ExternalSimilar[]
  externalLinks: Record<string, string>
}

// deExternalDetail normaliza o DTO do detalhe EXTERNO. Pôster/backdrop do hero podem
// vir como ROTA DO PROXY LOCAL (relativa, cacheada — 0024) ou como URL DIRETA da fonte
// (degradação): `srcDeImagem` resolve os dois (relativa recebe a base, absoluta passa).
export function deExternalDetail(d: ExternalDetail): RichDetail {
  return {
    type: d.type,
    title: d.title,
    originalTitle: d.original_title,
    description: d.description,
    tagline: d.tagline,
    year: d.year,
    runtime: d.runtime,
    certification: d.certification,
    voteAverage: d.vote_average,
    // O detalhe do externo não tem nota local (não é um item do dono ainda).
    localRating: null,
    originalLanguage: d.original_language,
    posterUrl: srcDeImagem(d.poster_url),
    backdropUrl: srcDeImagem(d.backdrop_url),
    // O detalhe do externo não tem still de episódio.
    stillUrl: null,
    genres: d.genres,
    keywords: d.keywords,
    videos: d.videos,
    collection: d.collection,
    similar: d.similar,
    externalLinks: d.external_links,
  }
}

// deResolvedEntity normaliza a ENTITY resolvida. Os GÊNEROS são as `tags` (que já os
// carregam). O PÔSTER é a rota LOCAL cacheada (via `posterSrc`); o BACKDROP vem da
// camada externa como URL direta (não é cacheado localmente nesta fase).
export function deResolvedEntity(e: ResolvedEntity): RichDetail {
  return {
    type: e.type,
    title: e.title,
    originalTitle: e.original_title,
    description: e.description,
    tagline: e.tagline,
    year: anoDeData(e.release_date),
    runtime: e.runtime,
    certification: e.certification,
    voteAverage: e.vote_average,
    localRating: e.local_rating,
    originalLanguage: e.original_language,
    // `arteSrc` (0044) resolve os três num único caminho: rota local (asset servível)
    // recebe a base; URL crua da fonte passa pelo image proxy.
    posterUrl: arteSrc(e.poster_url),
    // Backdrop: ROTA LOCAL quando cacheado (fanart própria/herdada, 0015/0044) ou
    // URL CRUA da fonte no fallback — a crua agora passa pelo proxy (não bate no CDN).
    backdropUrl: arteSrc(e.backdrop_url),
    // Still 16:9 do episódio: ROTA LOCAL quando há still CUSTOM do dono (0044,
    // /assets/still) ou URL CRUA do provider (0042, via proxy). null para
    // não-episódios — o front só o exibe no detalhe do episódio.
    stillUrl: arteSrc(e.still_url),
    genres: e.tags,
    keywords: e.keywords,
    videos: e.videos,
    collection: e.collection,
    similar: e.similar,
    externalLinks: e.external_links,
  }
}

// anoDeData extrai o ano (4 primeiros dígitos) de uma data ISO, ou null.
function anoDeData(data: string | null): string | null {
  return data && data.length >= 4 ? data.slice(0, 4) : null
}

// srcDeImagem resolve uma URL de imagem que pode ser ABSOLUTA (URL direta da
// fonte, ex.: TMDB) ou uma ROTA LOCAL relativa (cache de imagem 0015 / proxy 0024):
// a absoluta passa direto; a relativa recebe a base via `posterSrc` (respeita
// `VITE_API_BASE`). `null`/vazia → null. Exportado para o backdrop de outras telas
// (ex.: a página de coleção) resolverem a arte do mesmo jeito.
export function srcDeImagem(url: string | null): string | null {
  if (!url) {
    return null
  }
  return /^https?:\/\//.test(url) ? url : posterSrc(url)
}
