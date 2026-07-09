// Cliente HTTP da API do MyMediaDB.
//
// A base é configurável via `VITE_API_BASE`, mas o padrão (dev e prod) é vazio:
// isso faz as chamadas usarem caminho RELATIVO (`/api/...`). Em dev, o proxy do
// Vite encaminha para o backend Go (:8080); em prod, o binário serve tudo no
// mesmo origin. `VITE_API_BASE` só é preenchido como override (ex.: API remota).
import type { components } from '@/lib/api-types'

const base = import.meta.env.VITE_API_BASE ?? ''

// Tipos gerados do OpenAPI (0010) — nunca redigitados à mão.
export type Source = components['schemas']['Source']
export type SourceList = components['schemas']['SourceList']
export type CreateSource = components['schemas']['CreateSource']
export type ScanStatus = components['schemas']['ScanStatus']
export type StartScan = components['schemas']['StartScan']
export type EntityCard = components['schemas']['EntityCard']
export type EntityList = components['schemas']['EntityList']
export type ResolvedEntity = components['schemas']['ResolvedEntity']
export type MediaFile = components['schemas']['MediaFile']
export type FileList = components['schemas']['FileList']

// Forma do corpo de `GET /api/v1/health` (contrato do briefing 0001).
export interface Health {
  status: string
  version: string
}

// Erro tipado do envelope padrão `{ error: { code, message } }` (0008). Carrega
// o `code` estável (para ramificar) e a `message` em pt-BR (para exibir inline).
export class ApiError extends Error {
  readonly code: string
  readonly status: number

  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

// Lê o envelope de erro de uma resposta não-2xx e devolve um ApiError. Se o corpo
// não for o envelope esperado, cai num erro genérico com o status HTTP.
async function erroDaResposta(res: Response): Promise<ApiError> {
  try {
    const corpo = (await res.json()) as components['schemas']['Error']
    if (corpo?.error?.message) {
      return new ApiError(res.status, corpo.error.code, corpo.error.message)
    }
  } catch {
    // corpo ausente ou não-JSON — cai no genérico abaixo.
  }
  return new ApiError(res.status, 'http_error', `HTTP ${res.status}`)
}

async function lerJson<T>(res: Response): Promise<T> {
  if (!res.ok) {
    throw await erroDaResposta(res)
  }
  return (await res.json()) as T
}

// getHealth consulta o endpoint de saúde. Lança em status HTTP não-2xx ou em
// falha de rede, de modo que o TanStack Query trate isso como estado de erro.
export async function getHealth(): Promise<Health> {
  const res = await fetch(`${base}/api/v1/health`)
  if (!res.ok) {
    throw new Error(`health respondeu HTTP ${res.status}`)
  }
  return (await res.json()) as Health
}

// ── Datasources (biblioteca) ─────────────────────────────────────────────────

export async function listLibrary(): Promise<SourceList> {
  return lerJson<SourceList>(await fetch(`${base}/api/v1/library`))
}

export async function createLibrary(body: CreateSource): Promise<Source> {
  const res = await fetch(`${base}/api/v1/library`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  })
  return lerJson<Source>(res)
}

export async function deleteLibrary(id: string): Promise<void> {
  const res = await fetch(`${base}/api/v1/library/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
  if (!res.ok) {
    throw await erroDaResposta(res)
  }
}

export async function startScan(id: string): Promise<StartScan> {
  const res = await fetch(
    `${base}/api/v1/library/${encodeURIComponent(id)}/scan`,
    { method: 'POST' },
  )
  return lerJson<StartScan>(res)
}

export async function getScanStatus(id: string): Promise<ScanStatus> {
  const res = await fetch(
    `${base}/api/v1/library/${encodeURIComponent(id)}/scan`,
  )
  return lerJson<ScanStatus>(res)
}

// ── Entities (catálogo) ──────────────────────────────────────────────────────

export interface EntityListParams {
  type?: string
  owned?: boolean
  limit?: number
  offset?: number
  sort?: 'title' | '-title'
}

export async function listEntities(
  params: EntityListParams = {},
): Promise<EntityList> {
  const qs = new URLSearchParams()
  if (params.type) {
    qs.set('type', params.type)
  }
  if (params.owned !== undefined) {
    qs.set('owned', String(params.owned))
  }
  if (params.limit !== undefined) {
    qs.set('limit', String(params.limit))
  }
  if (params.offset !== undefined) {
    qs.set('offset', String(params.offset))
  }
  if (params.sort) {
    qs.set('sort', params.sort)
  }
  const suffix = qs.toString() ? `?${qs.toString()}` : ''
  return lerJson<EntityList>(await fetch(`${base}/api/v1/entities${suffix}`))
}

export async function getEntity(id: string): Promise<ResolvedEntity> {
  return lerJson<ResolvedEntity>(
    await fetch(`${base}/api/v1/entities/${encodeURIComponent(id)}`),
  )
}

export async function getEntityFiles(id: string): Promise<FileList> {
  return lerJson<FileList>(
    await fetch(`${base}/api/v1/entities/${encodeURIComponent(id)}/files`),
  )
}

// URL absoluta (respeitando a base) do binário do pôster local, ou `null` quando
// o card não tem arte — a UI cai no placeholder. `poster_url` já vem como rota
// pronta do card (ex.: `/api/v1/entities/{id}/assets/poster`).
export function posterSrc(posterUrl: string | null | undefined): string | null {
  if (!posterUrl) {
    return null
  }
  return `${base}${posterUrl}`
}
