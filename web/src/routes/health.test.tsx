import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import { HealthScreen } from '@/routes/health'
import { renderWithClient } from '@/test/render'

afterEach(() => {
  vi.restoreAllMocks()
})

// Standard body of `GET /api/v1/health/system` used in the success tests —
// one online source (with a bar) and one offline (badge "unavailable"), covering
// both branches of the storage block.
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

// mockFetch builds a global `fetch` that answers per route: `/health` falls on the
// basic status body, `/health/system` falls on the telemetry body (both
// parameterizable for the error/pending scenarios).
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
  it('shows the loading state before the response', () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise<Response>(() => {})),
    )

    renderWithClient(<HealthScreen />)

    expect(screen.getByText(/Connecting/)).toBeInTheDocument()
    expect(screen.getByText('connecting')).toBeInTheDocument()
  })

  it('shows "API online" with the version on success', async () => {
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

  it('shows "API unavailable" on error', async () => {
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
      expect(screen.getByText('API unavailable')).toBeInTheDocument(),
    )
    expect(screen.getByText('offline')).toBeInTheDocument()
  })

  describe('System section (0055)', () => {
    it('shows loading in the three blocks while the telemetry does not answer', () => {
      mockFetch({ system: 'pending' })

      renderWithClient(<HealthScreen />)

      expect(screen.getByText('Database')).toBeInTheDocument()
      expect(screen.getByText('Usage')).toBeInTheDocument()
      expect(screen.getByText('Storage')).toBeInTheDocument()
      // One "Loading…" per block (3), plus the "Connecting…" of the status card.
      expect(screen.getAllByText('Loading…')).toHaveLength(3)
    })

    it('shows ErrorState in the three blocks when the telemetry fails, without touching the status card', async () => {
      mockFetch({ system: { ok: false, status: 503, body: {} } })

      renderWithClient(<HealthScreen />)

      // The basic status card keeps working (independent query).
      await waitFor(() =>
        expect(screen.getByText(/API online — v9\.9\.9/)).toBeInTheDocument(),
      )
      await waitFor(() =>
        expect(
          screen.getAllByText('Could not load the telemetry'),
        ).toHaveLength(3),
      )
    })

    it('renders database/usage/storage with the data of the snapshot', async () => {
      mockFetch()

      renderWithClient(<HealthScreen />)

      await waitFor(() =>
        expect(screen.getByText('128.4 MB')).toBeInTheDocument(),
      )
      expect(screen.getByText('12')).toBeInTheDocument() // schema_version

      expect(screen.getByText('812')).toBeInTheDocument() // entities.total
      expect(screen.getByText('5d 2h')).toBeInTheDocument() // formatted uptime
      expect(
        screen.getByText('690 (682 present, 8 missing)'),
      ).toBeInTheDocument()

      expect(screen.getByText('/mnt/filmes')).toBeInTheDocument()
      expect(screen.getByText('/mnt/series')).toBeInTheDocument()
      expect(screen.getByText('unavailable')).toBeInTheDocument()
    })

    it('shows the three disk buckets of briefing 0063 (managed media/cache/unidentified)', async () => {
      mockFetch()

      renderWithClient(<HealthScreen />)

      await waitFor(() =>
        expect(screen.getByText('Managed media')).toBeInTheDocument(),
      )
      expect(screen.getByText('791.62 GB')).toBeInTheDocument() // managed_media_bytes
      expect(screen.getByText('Application cache')).toBeInTheDocument()
      expect(screen.getByText('2.00 GB')).toBeInTheDocument() // app_cache_bytes
      expect(screen.getByText('Unidentified')).toBeInTheDocument()
      expect(screen.getByText('5.00 GB')).toBeInTheDocument() // unmanaged_bytes
    })
  })
})
