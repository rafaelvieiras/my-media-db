import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { JobsScreen } from '@/routes/jobs'
import type { JobDefinition, JobExecution } from '@/lib/api'
import { okJson } from '@/test/entity-fixtures'
import {
  criarFetch,
  erroJson,
  esperarChaves,
  renderComEspioes,
} from '@/test/harness'

// Testes da tela `/jobs` (0052): lista os jobs conhecidos, toggle
// habilitado/desabilitado, agendamento cron editável (com 422 inline), disparo
// manual ("Executar agora") e histórico expansível por card.

function jobFixture(over: Partial<JobDefinition> = {}): JobDefinition {
  return {
    id: 'backup_vacuum',
    nome: 'Backup do banco (VACUUM INTO)',
    descricao: 'Faz um backup consistente via VACUUM INTO e rotaciona.',
    enabled: false,
    schedule_cron: '0 3 * * *',
    next_run_at: null,
    last_run_at: null,
    last_status: null,
    last_message: null,
    running: false,
    ...over,
  }
}

interface Espioes {
  onPatch: ReturnType<typeof vi.fn<(body: unknown) => void>>
  onRun: ReturnType<typeof vi.fn<() => void>>
}

interface Opcoes {
  job?: JobDefinition
  patchErro?: { status: number; code: string; message: string }
  executions?: JobExecution[]
}

// Handler de rotas dos jobs: GET /jobs, PATCH /jobs/{id}, POST /jobs/{id}/run,
// GET /jobs/{id}/executions. Guarda o job num closure mutável para o PATCH/run
// refletirem no próximo GET /jobs (o mesmo espírito do refetch pós-invalidação).
function montarFetch(opts: Opcoes, espioes: Espioes) {
  let job = opts.job ?? jobFixture()
  return criarFetch(({ path, method, init }) => {
    if (path === '/api/v1/jobs' && method === 'GET') {
      return okJson({ items: [job] })
    }
    if (path === `/api/v1/jobs/${job.id}` && method === 'PATCH') {
      const body = JSON.parse(String(init?.body)) as Partial<JobDefinition>
      espioes.onPatch(body)
      if (opts.patchErro) {
        return erroJson(
          opts.patchErro.status,
          opts.patchErro.code,
          opts.patchErro.message,
        )
      }
      job = { ...job, ...body }
      return okJson(job)
    }
    if (path === `/api/v1/jobs/${job.id}/run` && method === 'POST') {
      espioes.onRun()
      job = { ...job, running: true }
      return okJson({ execution_id: 'exec-1' })
    }
    if (path === `/api/v1/jobs/${job.id}/executions` && method === 'GET') {
      return okJson({ items: opts.executions ?? [] })
    }
    throw new Error(`rota nao mockada: ${method} ${path}`)
  })
}

function renderJobs(opts: Opcoes = {}) {
  const espioes: Espioes = {
    onPatch: vi.fn<(body: unknown) => void>(),
    onRun: vi.fn<() => void>(),
  }
  vi.stubGlobal('fetch', montarFetch(opts, espioes))
  return { espioes, ...renderComEspioes(<JobsScreen />) }
}

function esperarLista() {
  return waitFor(() =>
    expect(
      screen.getByText('Backup do banco (VACUUM INTO)'),
    ).toBeInTheDocument(),
  )
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('JobsScreen — administração de jobs (0052)', () => {
  it('lista os jobs conhecidos', async () => {
    renderJobs()
    await esperarLista()
    expect(
      screen.getByText(/Faz um backup consistente via VACUUM INTO/),
    ).toBeInTheDocument()
    expect(screen.getByText('Desabilitado')).toBeInTheDocument()
  })

  it('mostra o estado vazio quando não há jobs', async () => {
    vi.stubGlobal(
      'fetch',
      criarFetch(({ path, method }) => {
        if (path === '/api/v1/jobs' && method === 'GET') {
          return okJson({ items: [] })
        }
        throw new Error(`rota nao mockada: ${method} ${path}`)
      }),
    )
    renderComEspioes(<JobsScreen />)
    await waitFor(() =>
      expect(screen.getByText('Nenhum job registrado')).toBeInTheDocument(),
    )
  })

  it('toggle habilitado dispara PATCH e atualiza a UI', async () => {
    const user = userEvent.setup()
    const { espioes, invalidateSpy } = renderJobs()
    await esperarLista()

    await user.click(
      screen.getByLabelText(
        'Agendamento automático de Backup do banco (VACUUM INTO)',
      ),
    )

    await waitFor(() =>
      expect(espioes.onPatch).toHaveBeenCalledWith({ enabled: true }),
    )
    await esperarChaves(invalidateSpy, ['["jobs"]'])
    await waitFor(() =>
      expect(screen.getByText('Habilitado')).toBeInTheDocument(),
    )
  })

  it('salvar o agendamento dispara PATCH com schedule_cron', async () => {
    const user = userEvent.setup()
    const { espioes } = renderJobs()
    await esperarLista()

    const campo = screen.getByLabelText(
      'Agendamento cron de Backup do banco (VACUUM INTO)',
    )
    await user.clear(campo)
    await user.type(campo, '0 4 * * *')
    await user.click(screen.getByRole('button', { name: 'Salvar agendamento' }))

    await waitFor(() =>
      expect(espioes.onPatch).toHaveBeenCalledWith({
        schedule_cron: '0 4 * * *',
      }),
    )
  })

  it('erro 422 no agendamento aparece inline', async () => {
    const user = userEvent.setup()
    renderJobs({
      patchErro: {
        status: 422,
        code: 'invalid_cron',
        message: 'Expressão cron inválida.',
      },
    })
    await esperarLista()

    const campo = screen.getByLabelText(
      'Agendamento cron de Backup do banco (VACUUM INTO)',
    )
    await user.clear(campo)
    await user.type(campo, 'lixo')
    await user.click(screen.getByRole('button', { name: 'Salvar agendamento' }))

    await waitFor(() =>
      expect(screen.getByText('Expressão cron inválida.')).toBeInTheDocument(),
    )
  })

  it('"Executar agora" dispara POST e mostra loading→rodando', async () => {
    const user = userEvent.setup()
    const { espioes, invalidateSpy } = renderJobs()
    await esperarLista()

    await user.click(screen.getByRole('button', { name: /Executar agora/ }))

    await waitFor(() => expect(espioes.onRun).toHaveBeenCalled())
    await esperarChaves(invalidateSpy, [
      '["jobs"]',
      '["job","backup_vacuum","executions"]',
    ])
    await waitFor(() =>
      expect(screen.getByText('Rodando…')).toBeInTheDocument(),
    )
  })

  it('histórico lista as execuções do job', async () => {
    const user = userEvent.setup()
    renderJobs({
      executions: [
        {
          id: 'e1',
          job_id: 'backup_vacuum',
          trigger: 'manual',
          status: 'success',
          started_at: '2026-07-13T03:00:00Z',
          finished_at: '2026-07-13T03:00:05Z',
          message: 'backup gravado em mymediadb-20260713.sqlite (7 mantidos)',
        },
      ],
    })
    await esperarLista()

    await user.click(screen.getByRole('button', { name: 'Ver histórico' }))

    await waitFor(() =>
      expect(screen.getByText(/backup gravado em/)).toBeInTheDocument(),
    )
    const historico = screen.getByText(/backup gravado em/).closest('li')
    expect(historico).not.toBeNull()
    expect(
      within(historico as HTMLElement).getByText('Manual'),
    ).toBeInTheDocument()
    expect(
      within(historico as HTMLElement).getByText('Sucesso'),
    ).toBeInTheDocument()
  })
})
