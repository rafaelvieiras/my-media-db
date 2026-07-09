import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import { MediaInfoTable } from '@/components/media-info-table'
import type { MediaFile } from '@/lib/api'
import { renderWithClient } from '@/test/render'

function filesResponse(items: MediaFile[]) {
  return { ok: true, status: 200, json: async () => ({ items }) } as Response
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('MediaInfoTable', () => {
  it('mostra carregamento e depois os arquivos com mediainfo legível', async () => {
    const file: MediaFile = {
      id: 'f1',
      path: '/mnt/filmes/Matrix (1999)/Matrix.mkv',
      size: 1610612736,
      edition: "Director's Cut",
      part_no: 1,
      state: 'present',
      mediainfo: {
        general: { duration_seconds: 8160, size_bytes: 1610612736 },
        video: [{ codec: 'hevc', width: 1920, height: 1080, hdr: 'hdr10' }],
        audio: [{ codec: 'eac3', channels: 6, language: 'por' }],
        subtitle: [{ codec: 'subrip', language: 'eng' }],
      },
    }
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(filesResponse([file])))

    renderWithClient(<MediaInfoTable entityId="e1" />)
    expect(screen.getByText('Carregando arquivos…')).toBeInTheDocument()

    await waitFor(() =>
      expect(screen.getByText('Matrix.mkv')).toBeInTheDocument(),
    )
    expect(screen.getByText('1080p · HEVC · HDR10 · 5.1')).toBeInTheDocument()
    expect(screen.getByText("Director's Cut")).toBeInTheDocument()
    expect(screen.getByText('Parte 1')).toBeInTheDocument()
    expect(screen.getByText('presente')).toBeInTheDocument()
    expect(screen.getByText(/E-AC3 · 5.1 · Português/)).toBeInTheDocument()
  })

  it('marca arquivos ausentes e sem informações técnicas', async () => {
    const file: MediaFile = {
      id: 'f2',
      path: '/x/y.mkv',
      size: null,
      edition: null,
      part_no: null,
      state: 'missing',
      mediainfo: null,
    }
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(filesResponse([file])))

    renderWithClient(<MediaInfoTable entityId="e1" />)
    await waitFor(() => expect(screen.getByText('ausente')).toBeInTheDocument())
    expect(screen.getByText('Sem informações técnicas')).toBeInTheDocument()
  })

  it('mostra vazio quando não há arquivos', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(filesResponse([])))
    renderWithClient(<MediaInfoTable entityId="e1" />)
    await waitFor(() =>
      expect(
        screen.getByText('Nenhum arquivo associado a este item.'),
      ).toBeInTheDocument(),
    )
  })

  it('mostra estado de erro', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 404,
        json: async () => ({
          error: { code: 'not_found', message: 'não achou' },
        }),
      } as Response),
    )
    renderWithClient(<MediaInfoTable entityId="e1" />)
    await waitFor(() =>
      expect(
        screen.getByText('Não foi possível carregar os arquivos'),
      ).toBeInTheDocument(),
    )
  })
})
