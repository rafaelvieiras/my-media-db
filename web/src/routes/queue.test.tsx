import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueueScreen } from '@/routes/queue'
import type { QueueEvent, QueueItem, QueueView } from '@/lib/api'
import { okJson } from '@/test/entity-fixtures'
import { criarFetch, renderComEspioes } from '@/test/harness'

// Tests of the `/dashboard/queue` screen (ADR-0040, issue #62): it shows the run
// in progress (step, counters, current item and elapsed time), the FIFO of
// waiting items with their position and the recently finished ones with their
// duration — all from a single `GET /queue`. Expanding an item fetches its trail
// (`GET /queue/{id}`), which is where "which items succeeded and which failed"
// lives.

// Fixtures mirror what the server sends: milliseconds are DERIVED there, so the
// component just formats them.
function itemFixture(over: Partial<QueueItem> = {}): QueueItem {
  return {
    id: 'q-1',
    kind: 'job',
    ref_id: 'saude_biblioteca',
    ref_label: 'Saúde da biblioteca',
    trigger: 'manual',
    status: 'running',
    message: null,
    position: null,
    step_label: 'enriquecimento retroativo',
    step_index: 3,
    step_total: 8,
    current_item: 'Filmes/O Auto da Compadecida (2000)',
    done: 40,
    total: 120,
    ok: 37,
    failed: 3,
    enqueued_at: '2026-11-19T15:44:00Z',
    started_at: '2026-11-19T15:44:48Z',
    finished_at: null,
    wait_ms: 48000,
    duration_ms: null,
    elapsed_ms: 4500000, // 1h15min
    ...over,
  }
}

function filaVazia(over: Partial<QueueView> = {}): QueueView {
  return { running: null, queued: [], recent: [], ...over }
}

function montarFetch(fila: QueueView, eventos: QueueEvent[] = []) {
  return criarFetch(({ path, method }) => {
    if (path === '/api/v1/queue' && method === 'GET') {
      return okJson(fila)
    }
    const detalhe = /^\/api\/v1\/queue\/([^/]+)$/.exec(path)
    if (detalhe && method === 'GET') {
      const id = detalhe[1]
      const achado =
        fila.running?.id === id
          ? fila.running
          : (fila.queued.find((i) => i.id === id) ??
            fila.recent.find((i) => i.id === id))
      if (!achado) {
        return { ok: false, status: 404, json: async () => ({}) } as Response
      }
      return okJson({ item: achado, events: eventos })
    }
    throw new Error(`rota nao mockada: ${method} ${path}`)
  })
}

function renderFila(fila: QueueView, eventos: QueueEvent[] = []) {
  vi.stubGlobal('fetch', montarFetch(fila, eventos))
  return renderComEspioes(<QueueScreen />)
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('QueueScreen — a fila observável (ADR-0040)', () => {
  it('the empty queue says so instead of showing three blank sections', async () => {
    renderFila(filaVazia())

    expect(await screen.findByText('Nothing running')).toBeInTheDocument()
    expect(
      screen.getByText(/The queue is empty\. A scheduled job or a scan/),
    ).toBeInTheDocument()
    expect(screen.queryByText('Running now')).not.toBeInTheDocument()
  })

  it('the running item shows the step, the counters, the item and how long it has been running', async () => {
    renderFila(filaVazia({ running: itemFixture() }))

    expect(await screen.findByText('Running now')).toBeInTheDocument()
    const card = screen.getByText('Saúde da biblioteca').closest('li')
    expect(card).not.toBeNull()
    const dentro = within(card as HTMLElement)

    expect(dentro.getByText('Job')).toBeInTheDocument()
    expect(dentro.getByText(/^Manual · started /)).toBeInTheDocument()
    expect(
      dentro.getByText('Step 3/8: enriquecimento retroativo'),
    ).toBeInTheDocument()
    expect(dentro.getByText('40 of 120 files')).toBeInTheDocument()
    expect(dentro.getByText('37 items ok · 3 failed')).toBeInTheDocument()
    expect(
      dentro.getByText('Now: Filmes/O Auto da Compadecida (2000)'),
    ).toBeInTheDocument()
    // 4 500 000 ms = 1h 15min: only the two largest components, same as uptime.
    expect(dentro.getByText('running 1h 15min')).toBeInTheDocument()
    expect(dentro.getByText('Running')).toBeInTheDocument()
    expect(dentro.getByRole('progressbar')).toHaveAttribute(
      'aria-valuenow',
      '40',
    )
  })

  it('announces the counters of the run and does not read them as one number', async () => {
    renderFila(filaVazia({ running: itemFixture({ ok: 0, failed: 15 }) }))

    const card = (await screen.findByText('Saúde da biblioteca')).closest('li')
    const dentro = within(card as HTMLElement)

    // `ok` disappears when nothing succeeded; the failure counter keeps its noun.
    expect(dentro.getByText('40 of 120 files')).toBeInTheDocument()
    expect(dentro.getByText('15 items failed')).toBeInTheDocument()
    expect(dentro.queryByText(/items ok/)).not.toBeInTheDocument()

    // The counters live in a polite live region: they change on their own every 4s.
    const viva = dentro.getByText('15 items failed').closest('[role="status"]')
    expect(viva).not.toBeNull()
    expect(viva).toHaveAttribute('aria-live', 'polite')
  })

  it('formats the counters with the locale separator', async () => {
    renderFila(
      filaVazia({
        recent: [
          itemFixture({
            id: 'q-9',
            status: 'success',
            done: 36599,
            total: 36599,
            ok: 15,
            failed: 0,
            elapsed_ms: null,
            duration_ms: 3600000,
          }),
        ],
      }),
    )

    await screen.findByText('Recently finished')
    expect(screen.getByText('36,599 of 36,599 files')).toBeInTheDocument()
    expect(screen.getByText('15 items ok · 0 failed')).toBeInTheDocument()
  })

  it('paints the progress bar by status and labels it for screen readers', async () => {
    renderFila(
      filaVazia({
        recent: [
          itemFixture({
            id: 'q-4',
            status: 'success',
            done: 120,
            total: 120,
            ok: 120,
            failed: 0,
            elapsed_ms: null,
            duration_ms: 140000,
          }),
        ],
      }),
    )

    await screen.findByText('Recently finished')
    const barra = screen.getByRole('progressbar')
    expect(barra).toHaveAttribute('aria-label', 'Progress of this run')
    expect(barra).toHaveAttribute('aria-valuetext', '120 of 120 files (100%)')
    expect(barra.firstElementChild?.className).toContain('bg-emerald-600')
  })

  it('a waiting item shows its position and how long it has been waiting', async () => {
    renderFila(
      filaVazia({
        queued: [
          itemFixture({
            id: 'q-2',
            kind: 'scan',
            ref_id: 'src-1',
            ref_label: '/media/videos',
            trigger: 'watch',
            status: 'queued',
            position: 1,
            step_label: null,
            step_index: 0,
            step_total: 0,
            current_item: null,
            done: 0,
            total: 0,
            ok: 0,
            failed: 0,
            started_at: null,
            wait_ms: 120000,
            elapsed_ms: null,
          }),
        ],
      }),
    )

    // "Waiting" is both the section title and the status badge of the item.
    expect(
      await screen.findByRole('heading', { name: 'Waiting' }),
    ).toBeInTheDocument()
    const card = screen.getByText('/media/videos').closest('li')
    const dentro = within(card as HTMLElement)
    expect(dentro.getByText('Scan')).toBeInTheDocument()
    expect(dentro.getByText(/^Watcher/)).toBeInTheDocument()
    expect(dentro.getByText('#1 in line')).toBeInTheDocument()
    expect(dentro.getByText('waiting 2min')).toBeInTheDocument()
  })

  it('a finished item shows its total duration and the reason it failed', async () => {
    renderFila(
      filaVazia({
        recent: [
          itemFixture({
            id: 'q-3',
            status: 'error',
            message: 'fonte indisponível',
            done: 10,
            total: 10,
            ok: 9,
            failed: 1,
            finished_at: '2026-11-19T15:50:00Z',
            duration_ms: 140000,
            elapsed_ms: null,
          }),
        ],
      }),
    )

    await screen.findByText('Recently finished')
    const card = screen.getByText('Saúde da biblioteca').closest('li')
    const dentro = within(card as HTMLElement)
    // 140 000 ms = 2min20s, but the label keeps the two largest components only.
    expect(dentro.getByText('took 2min')).toBeInTheDocument()
    expect(dentro.getByText('Failed')).toBeInTheDocument()
    expect(dentro.getByText('fonte indisponível')).toBeInTheDocument()
  })

  it('expanding an item fetches the trail and lists what each processed thing did', async () => {
    const user = userEvent.setup()
    renderFila(filaVazia({ running: itemFixture() }), [
      {
        seq: 1,
        at: '2026-11-19T15:50:00Z',
        status: 'ok',
        label: '10 Dálmatas',
        detail: 'tmdb 11674',
      },
      {
        seq: 2,
        at: '2026-11-19T15:50:02Z',
        status: 'error',
        label: 'Super',
        detail: 'nenhum candidato',
      },
    ])

    await user.click(
      await screen.findByRole('button', { name: 'Show what it did' }),
    )

    expect(await screen.findByText('10 Dálmatas')).toBeInTheDocument()
    expect(screen.getByText('Super')).toBeInTheDocument()
    expect(screen.getByText(/^Done · /)).toBeInTheDocument()
    expect(screen.getByText(/tmdb 11674/)).toBeInTheDocument()
    expect(screen.getByText(/^Failed · /)).toBeInTheDocument()
    // The trail is fetched once, on demand — not with the listing.
    expect(
      screen.getByRole('button', { name: 'Hide what it did' }),
    ).toBeInTheDocument()
  })

  it('paginates the trail instead of dumping every row into the page', async () => {
    const user = userEvent.setup()
    const eventos: QueueEvent[] = Array.from({ length: 45 }, (_, i) => ({
      seq: i + 1,
      at: '2026-11-19T15:50:00Z',
      status: 'ok' as const,
      label: `Item ${String(i + 1).padStart(2, '0')}`,
      detail: null,
    }))
    renderFila(filaVazia({ running: itemFixture() }), eventos)

    await user.click(
      await screen.findByRole('button', { name: 'Show what it did' }),
    )

    expect(await screen.findByText('Showing 20 of 45')).toBeInTheDocument()
    expect(screen.getAllByText(/^Item \d\d$/)).toHaveLength(20)

    await user.click(screen.getByRole('button', { name: 'Show more' }))
    expect(screen.getByText('Showing 40 of 45')).toBeInTheDocument()
    expect(screen.getAllByText(/^Item \d\d$/)).toHaveLength(40)

    await user.click(screen.getByRole('button', { name: 'Show more' }))
    expect(screen.getByText('Showing 45 of 45')).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Show more' }),
    ).not.toBeInTheDocument()
  })

  it('the trail of a run that reported nothing says so', async () => {
    const user = userEvent.setup()
    renderFila(
      filaVazia({ running: itemFixture({ step_total: 0, step_label: null }) }),
      [],
    )

    await user.click(
      await screen.findByRole('button', { name: 'Show what it did' }),
    )
    expect(
      await screen.findByText('This run has not reported any item yet.'),
    ).toBeInTheDocument()
  })

  it('a failure to load the queue shows the error, not an empty screen', async () => {
    vi.stubGlobal(
      'fetch',
      criarFetch(
        () => ({ ok: false, status: 503, json: async () => ({}) }) as Response,
      ),
    )
    renderComEspioes(<QueueScreen />)

    await waitFor(() =>
      expect(screen.getByText('Could not load the queue')).toBeInTheDocument(),
    )
  })

  it('a failed load offers a retry instead of a dead end', async () => {
    const user = userEvent.setup()
    let chamadas = 0
    vi.stubGlobal(
      'fetch',
      criarFetch(() => {
        chamadas += 1
        if (chamadas === 1) {
          return { ok: false, status: 503, json: async () => ({}) } as Response
        }
        return okJson(filaVazia({ running: itemFixture() }))
      }),
    )
    renderComEspioes(<QueueScreen />)

    await user.click(await screen.findByRole('button', { name: 'Try again' }))

    expect(await screen.findByText('Running now')).toBeInTheDocument()
  })

  it('says how fresh the polling data is', async () => {
    renderFila(filaVazia({ running: itemFixture() }))

    expect(
      await screen.findByText(/^updated (just now|\d+s ago)$/),
    ).toBeInTheDocument()
  })
})
