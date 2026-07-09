// Cliente HTTP mínimo da API do MyMediaDB.
//
// A base é configurável via `VITE_API_BASE`, mas o padrão (dev e prod) é vazio:
// isso faz as chamadas usarem caminho RELATIVO (`/api/...`). Em dev, o proxy do
// Vite encaminha para o backend Go (:8080); em prod, o binário serve tudo no
// mesmo origin. `VITE_API_BASE` só é preenchido como override (ex.: API remota).
const base = import.meta.env.VITE_API_BASE ?? ''

// Forma do corpo de `GET /api/v1/health` (contrato do briefing 0001).
export interface Health {
  status: string
  version: string
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
