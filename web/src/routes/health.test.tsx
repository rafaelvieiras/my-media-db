import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import { HealthScreen } from '@/routes/health'
import { renderWithClient } from '@/test/render'

afterEach(() => {
  vi.restoreAllMocks()
})

// Corpo padrão de `GET /api/v1/health/system` usado nos testes de sucesso —
// uma fonte online (com barra) e uma offline (badge "indisponível"), cobrindo
// os dois ramos do bloco de armazenamento.
const SYSTEM_HEALTH_OK = {
  computed_at: '2026-07-15T19:40:00.000Z',
  database: {
    size_bytes: 134676480,
    page_count: 32880,
    page_size: 4096,
    schema_version: 12,
  },
  usage: {
    uptime_seconds: 439980, // 5d 2h
    entities: {
      total: 812,
      by_type: { movie: 340, season: 96 },
    },
    media_files: { total: 690, present: 682, missing: 8 },
    library_sources: 3,
    review_queue_size: 5,
    orphans_count: 2,
    job_executions_total: 47,
    goroutines: 14,
    mem_alloc_bytes: 18874368,
  },
  storage: {
    managed_media_bytes: 850000000000,
    app_cache_bytes: 2147483648,
    unmanaged_bytes: 5368709120,
    sources: [
      {
        source_id: 'a1b2',
        path: '/mnt/filmes',
        used_bytes: 892173058048,
        filesystem: { free_bytes: 120259084288, total_bytes: 2000398934016 },
        offline: false,
      },
      {
        source_id: 'c3d4',
        path: '/mnt/series',
        used_bytes: 0,
        filesystem: null,
        offline: true,
        error:
          'estatisticas de filesystem indisponiveis: no such file or directory',
      },
    ],
  },
}

// mockFetch monta um `fetch` global que responde por rota: `/health` cai no
// corpo básico de status, `/health/system` cai no corpo de telemetria (ambos
// parametrizáveis para os cenários de erro/pendente).
function mockFetch({
  health = { ok: true, status: 200, body: { status: 'ok', version: '9.9.9' } },
  system = { ok: true, status: 200, body: SYSTEM_HEALTH_OK },
}: {
  health?: { ok: boolean; status: number; body: unknown }
  system?: { ok: boolean; status: number; body: unknown } | 'pending'
} = {}) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/health/system')) {
        if (system === 'pending') {
          return new Promise<Response>(() => {})
        }
        return Promise.resolve({
          ok: system.ok,
          status: system.status,
          json: async () => system.body,
        } as Response)
      }
      return Promise.resolve({
        ok: health.ok,
        status: health.status,
        json: async () => health.body,
      } as Response)
    }),
  )
}

describe('HealthScreen', () => {
  it('mostra o estado de carregamento antes da resposta', () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise<Response>(() => {})),
    )

    renderWithClient(<HealthScreen />)

    expect(screen.getByText(/Conectando/)).toBeInTheDocument()
    expect(screen.getByText('conectando')).toBeInTheDocument()
  })

  it('mostra "API online" com a versão em caso de sucesso', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ status: 'ok', version: '9.9.9' }),
      } as Response),
    )

    renderWithClient(<HealthScreen />)

    await waitFor(() =>
      expect(screen.getByText(/API online — v9\.9\.9/)).toBeInTheDocument(),
    )
    expect(screen.getByText('online')).toBeInTheDocument()
  })

  it('mostra "API indisponível" em caso de erro', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        json: async () => ({}),
      } as Response),
    )

    renderWithClient(<HealthScreen />)

    await waitFor(() =>
      expect(screen.getByText('API indisponível')).toBeInTheDocument(),
    )
    expect(screen.getByText('offline')).toBeInTheDocument()
  })

  describe('seção Sistema (0055)', () => {
    it('mostra loading nos três blocos enquanto a telemetria não responde', () => {
      mockFetch({ system: 'pending' })

      renderWithClient(<HealthScreen />)

      expect(screen.getByText('Banco de dados')).toBeInTheDocument()
      expect(screen.getByText('Uso')).toBeInTheDocument()
      expect(screen.getByText('Armazenamento')).toBeInTheDocument()
      // Um "Carregando…" por bloco (3), fora o "Conectando…" do card de status.
      expect(screen.getAllByText('Carregando…')).toHaveLength(3)
    })

    it('mostra ErrorState nos três blocos quando a telemetria falha, sem afetar o card de status', async () => {
      mockFetch({ system: { ok: false, status: 503, body: {} } })

      renderWithClient(<HealthScreen />)

      // O card de status básico segue funcionando (query independente).
      await waitFor(() =>
        expect(screen.getByText(/API online — v9\.9\.9/)).toBeInTheDocument(),
      )
      await waitFor(() =>
        expect(
          screen.getAllByText('Não foi possível carregar a telemetria'),
        ).toHaveLength(3),
      )
    })

    it('renderiza banco/uso/armazenamento com os dados do snapshot', async () => {
      mockFetch()

      renderWithClient(<HealthScreen />)

      await waitFor(() =>
        expect(screen.getByText('128,4 MB')).toBeInTheDocument(),
      )
      expect(screen.getByText('12')).toBeInTheDocument() // schema_version

      expect(screen.getByText('812')).toBeInTheDocument() // entities.total
      expect(screen.getByText('5d 2h')).toBeInTheDocument() // uptime formatado
      expect(
        screen.getByText('690 (682 presentes, 8 ausentes)'),
      ).toBeInTheDocument()

      expect(screen.getByText('/mnt/filmes')).toBeInTheDocument()
      expect(screen.getByText('/mnt/series')).toBeInTheDocument()
      expect(screen.getByText('indisponível')).toBeInTheDocument()
    })

    it('mostra os três buckets de disco do briefing 0063 (mídia gerenciada/cache/não identificado)', async () => {
      mockFetch()

      renderWithClient(<HealthScreen />)

      await waitFor(() =>
        expect(screen.getByText('Mídia gerenciada')).toBeInTheDocument(),
      )
      expect(screen.getByText('791,62 GB')).toBeInTheDocument() // managed_media_bytes
      expect(screen.getByText('Cache da aplicação')).toBeInTheDocument()
      expect(screen.getByText('2,00 GB')).toBeInTheDocument() // app_cache_bytes
      expect(screen.getByText('Não identificado')).toBeInTheDocument()
      expect(screen.getByText('5,00 GB')).toBeInTheDocument() // unmanaged_bytes
    })
  })
})
