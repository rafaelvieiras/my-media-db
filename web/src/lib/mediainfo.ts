// Formatação PURA de mediainfo (fatos técnicos do ffprobe) em rótulos legíveis.
//
// Separado da renderização de propósito: cada formatador é uma função pura,
// fácil de testar (o briefing 0011 exige cobertura honesta da formatação) e de
// manter. A entrada é sempre o shape `MediaInfo` gerado do OpenAPI (0010); a
// saída são strings prontas para exibir (ex.: `1080p · HEVC · HDR10 · 5.1`).
import type { components } from '@/lib/api-types'

export type MediaInfo = components['schemas']['MediaInfo']
type VideoTrack = NonNullable<MediaInfo['video']>[number]
type AudioTrack = NonNullable<MediaInfo['audio']>[number]
type SubtitleTrack = NonNullable<MediaInfo['subtitle']>[number]

// Resolução vertical → rótulo comercial (720p, 1080p, 2160p…). Usa a altura como
// referência principal; se ausente, deriva da largura. `null` quando não dá.
export function formatarResolucao(
  width?: number,
  height?: number,
): string | null {
  const h = height ?? larguraParaAltura(width)
  if (!h || h <= 0) {
    return null
  }
  if (h >= 4320) {
    return '4320p'
  }
  if (h >= 2160) {
    return '2160p'
  }
  if (h >= 1440) {
    return '1440p'
  }
  if (h >= 1080) {
    return '1080p'
  }
  if (h >= 720) {
    return '720p'
  }
  if (h >= 576) {
    return '576p'
  }
  if (h >= 480) {
    return '480p'
  }
  return `${h}p`
}

function larguraParaAltura(width?: number): number | undefined {
  if (!width || width <= 0) {
    return undefined
  }
  if (width >= 7680) {
    return 4320
  }
  if (width >= 3840) {
    return 2160
  }
  if (width >= 2560) {
    return 1440
  }
  if (width >= 1920) {
    return 1080
  }
  if (width >= 1280) {
    return 720
  }
  return 480
}

const CODECS_VIDEO: Record<string, string> = {
  hevc: 'HEVC',
  h265: 'HEVC',
  h264: 'H.264',
  avc: 'H.264',
  avc1: 'H.264',
  av1: 'AV1',
  vp9: 'VP9',
  vc1: 'VC-1',
  mpeg2video: 'MPEG-2',
  mpeg4: 'MPEG-4',
}

const CODECS_AUDIO: Record<string, string> = {
  aac: 'AAC',
  ac3: 'AC3',
  eac3: 'E-AC3',
  dts: 'DTS',
  truehd: 'TrueHD',
  flac: 'FLAC',
  opus: 'Opus',
  mp3: 'MP3',
  vorbis: 'Vorbis',
}

// Normaliza o codec para um rótulo conhecido; desconhecidos viram maiúsculas.
export function formatarCodec(codec?: string): string | null {
  if (!codec) {
    return null
  }
  const chave = codec.toLowerCase()
  return CODECS_VIDEO[chave] ?? CODECS_AUDIO[chave] ?? codec.toUpperCase()
}

const HDR_ROTULOS: Record<string, string> = {
  hdr10: 'HDR10',
  'hdr10+': 'HDR10+',
  hdr10plus: 'HDR10+',
  dolby_vision: 'Dolby Vision',
  dolbyvision: 'Dolby Vision',
  dv: 'Dolby Vision',
  hlg: 'HLG',
}

// Rótulo de HDR. `sdr`/vazio → `null` (não é fato digno de badge).
export function formatarHdr(hdr?: string): string | null {
  if (!hdr) {
    return null
  }
  const chave = hdr.toLowerCase()
  if (chave === 'sdr' || chave === 'none') {
    return null
  }
  return HDR_ROTULOS[chave] ?? hdr.toUpperCase()
}

// Número de canais → layout falado (2 → 2.0, 6 → 5.1, 8 → 7.1).
export function formatarCanais(channels?: number): string | null {
  if (!channels || channels <= 0) {
    return null
  }
  const mapa: Record<number, string> = {
    1: '1.0',
    2: '2.0',
    3: '2.1',
    6: '5.1',
    7: '6.1',
    8: '7.1',
  }
  return mapa[channels] ?? `${channels} canais`
}

// Duração em segundos → `2h 16min` / `45min` / `30s`.
export function formatarDuracao(segundos?: number): string | null {
  if (!segundos || segundos <= 0) {
    return null
  }
  const total = Math.round(segundos)
  const h = Math.floor(total / 3600)
  const min = Math.floor((total % 3600) / 60)
  const s = total % 60
  if (h > 0) {
    return min > 0 ? `${h}h ${min}min` : `${h}h`
  }
  if (min > 0) {
    return `${min}min`
  }
  return `${s}s`
}

const IDIOMAS: Record<string, string> = {
  por: 'Português',
  pt: 'Português',
  eng: 'Inglês',
  en: 'Inglês',
  spa: 'Espanhol',
  es: 'Espanhol',
  jpn: 'Japonês',
  ja: 'Japonês',
  fre: 'Francês',
  fra: 'Francês',
  fr: 'Francês',
  ger: 'Alemão',
  deu: 'Alemão',
  de: 'Alemão',
  ita: 'Italiano',
  it: 'Italiano',
  und: 'Indefinido',
}

// Código ISO-639 → nome do idioma em pt-BR; desconhecido → maiúsculas.
export function formatarIdioma(lang?: string): string | null {
  if (!lang) {
    return null
  }
  const chave = lang.toLowerCase()
  return IDIOMAS[chave] ?? lang.toUpperCase()
}

// Tamanho em bytes → `4,3 GB` / `700 MB`.
export function formatarTamanho(bytes?: number | null): string | null {
  if (!bytes || bytes <= 0) {
    return null
  }
  const unidades = ['B', 'KB', 'MB', 'GB', 'TB']
  let valor = bytes
  let i = 0
  while (valor >= 1024 && i < unidades.length - 1) {
    valor /= 1024
    i++
  }
  const texto =
    valor >= 100 || i === 0 ? Math.round(valor).toString() : valor.toFixed(1)
  return `${texto.replace('.', ',')} ${unidades[i]}`
}

// ── Agregações por arquivo ───────────────────────────────────────────────────

// Chips-resumo da faixa principal: resolução · codec · HDR · canais do 1º áudio.
// É o que aparece ao lado do arquivo (ex.: `1080p · HEVC · HDR10 · 5.1`).
export function chipsDoArquivo(mi: MediaInfo | null): string[] {
  if (!mi) {
    return []
  }
  const video = mi.video?.[0]
  const audio = mi.audio?.[0]
  return [
    formatarResolucao(video?.width, video?.height),
    formatarCodec(video?.codec),
    formatarHdr(video?.hdr),
    formatarCanais(audio?.channels),
  ].filter((x): x is string => x !== null)
}

// Descreve uma faixa de áudio (`AAC · 5.1 · Português`).
export function descreverAudio(faixa: AudioTrack): string {
  return [
    formatarCodec(faixa.codec),
    formatarCanais(faixa.channels),
    formatarIdioma(faixa.language),
  ]
    .filter((x): x is string => x !== null)
    .join(' · ')
}

// Descreve uma faixa de legenda (`Português (SRT)`).
export function descreverLegenda(faixa: SubtitleTrack): string {
  const idioma = formatarIdioma(faixa.language)
  const codec = formatarCodec(faixa.codec)
  if (idioma && codec) {
    return `${idioma} (${codec})`
  }
  return idioma ?? codec ?? 'Legenda'
}

// Linha geral (duração · tamanho) para o cabeçalho do arquivo.
export function linhaGeral(mi: MediaInfo | null): string[] {
  if (!mi?.general) {
    return []
  }
  return [
    formatarDuracao(mi.general.duration_seconds),
    formatarTamanho(mi.general.size_bytes),
  ].filter((x): x is string => x !== null)
}

export type { VideoTrack, AudioTrack, SubtitleTrack }
