import { describe, expect, it } from 'vitest'
import {
  chipsDoArquivo,
  descreverAudio,
  descreverLegenda,
  formatarCanais,
  formatarCodec,
  formatarDuracao,
  formatarHdr,
  formatarIdioma,
  formatarResolucao,
  formatarTamanho,
  linhaGeral,
  type MediaInfo,
} from '@/lib/mediainfo'

describe('formatarResolucao', () => {
  it('mapeia alturas comuns para rótulos comerciais', () => {
    expect(formatarResolucao(1920, 1080)).toBe('1080p')
    expect(formatarResolucao(3840, 2160)).toBe('2160p')
    expect(formatarResolucao(1280, 720)).toBe('720p')
    expect(formatarResolucao(7680, 4320)).toBe('4320p')
    expect(formatarResolucao(2560, 1440)).toBe('1440p')
  })

  it('deriva a altura da largura quando falta a altura', () => {
    expect(formatarResolucao(1920)).toBe('1080p')
    expect(formatarResolucao(3840)).toBe('2160p')
  })

  it('devolve null sem dimensões úteis', () => {
    expect(formatarResolucao()).toBeNull()
    expect(formatarResolucao(0, 0)).toBeNull()
  })

  it('cai no fallback `<h>p` para alturas incomuns', () => {
    expect(formatarResolucao(undefined, 300)).toBe('300p')
  })
})

describe('formatarCodec', () => {
  it('normaliza codecs de vídeo e áudio conhecidos', () => {
    expect(formatarCodec('hevc')).toBe('HEVC')
    expect(formatarCodec('h264')).toBe('H.264')
    expect(formatarCodec('AV1')).toBe('AV1')
    expect(formatarCodec('aac')).toBe('AAC')
    expect(formatarCodec('truehd')).toBe('TrueHD')
  })

  it('faz maiúsculas em desconhecidos e null em vazio', () => {
    expect(formatarCodec('xyz')).toBe('XYZ')
    expect(formatarCodec()).toBeNull()
  })
})

describe('formatarHdr', () => {
  it('rotula variantes de HDR', () => {
    expect(formatarHdr('hdr10')).toBe('HDR10')
    expect(formatarHdr('dolby_vision')).toBe('Dolby Vision')
    expect(formatarHdr('hlg')).toBe('HLG')
  })

  it('ignora SDR e vazio', () => {
    expect(formatarHdr('sdr')).toBeNull()
    expect(formatarHdr()).toBeNull()
  })
})

describe('formatarCanais', () => {
  it('traduz contagem de canais para layout falado', () => {
    expect(formatarCanais(2)).toBe('2.0')
    expect(formatarCanais(6)).toBe('5.1')
    expect(formatarCanais(8)).toBe('7.1')
    expect(formatarCanais(4)).toBe('4 canais')
    expect(formatarCanais(0)).toBeNull()
  })
})

describe('formatarDuracao', () => {
  it('formata horas, minutos e segundos', () => {
    expect(formatarDuracao(8160)).toBe('2h 16min')
    expect(formatarDuracao(3600)).toBe('1h')
    expect(formatarDuracao(2700)).toBe('45min')
    expect(formatarDuracao(30)).toBe('30s')
    expect(formatarDuracao(0)).toBeNull()
  })
})

describe('formatarIdioma', () => {
  it('traduz códigos ISO-639 conhecidos', () => {
    expect(formatarIdioma('por')).toBe('Português')
    expect(formatarIdioma('en')).toBe('Inglês')
    expect(formatarIdioma('und')).toBe('Indefinido')
    expect(formatarIdioma('zzz')).toBe('ZZZ')
    expect(formatarIdioma()).toBeNull()
  })
})

describe('formatarTamanho', () => {
  it('formata bytes em unidades legíveis pt-BR', () => {
    expect(formatarTamanho(700 * 1024 * 1024)).toBe('700 MB')
    expect(formatarTamanho(1610612736)).toBe('1,5 GB')
    expect(formatarTamanho(0)).toBeNull()
    expect(formatarTamanho(null)).toBeNull()
  })
})

describe('agregações por arquivo', () => {
  const mi: MediaInfo = {
    general: { duration_seconds: 8160, size_bytes: 1610612736 },
    video: [{ codec: 'hevc', width: 1920, height: 1080, hdr: 'hdr10' }],
    audio: [
      { codec: 'eac3', channels: 6, language: 'por' },
      { codec: 'aac', channels: 2, language: 'eng' },
    ],
    subtitle: [{ codec: 'subrip', language: 'por' }],
  }

  it('monta os chips da faixa principal', () => {
    expect(chipsDoArquivo(mi)).toEqual(['1080p', 'HEVC', 'HDR10', '5.1'])
  })

  it('devolve chips vazios sem mediainfo', () => {
    expect(chipsDoArquivo(null)).toEqual([])
  })

  it('descreve faixas de áudio e legenda', () => {
    expect(descreverAudio(mi.audio![0])).toBe('E-AC3 · 5.1 · Português')
    expect(descreverLegenda(mi.subtitle![0])).toBe('Português (SUBRIP)')
  })

  it('monta a linha geral com duração e tamanho', () => {
    expect(linhaGeral(mi)).toEqual(['2h 16min', '1,5 GB'])
    expect(linhaGeral(null)).toEqual([])
  })
})
