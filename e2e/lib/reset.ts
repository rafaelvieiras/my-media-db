import { APIRequestContext, expect } from '@playwright/test'

// Isolamento das jornadas: elas compartilham UM banco (workers=1). Em vez de
// coordenar ids "donos" de estado, cada teste zera a biblioteca no beforeEach —
// assim cada jornada comeca limpa e independe da ordem. Usa so a API publica
// (lista + DELETE por id; o DELETE purga com CASCADE), sem endpoint de teste.
export async function resetBiblioteca(request: APIRequestContext): Promise<void> {
  const res = await request.get('/api/v1/entities?limit=1000')
  expect(res.ok()).toBeTruthy()
  const body = await res.json()
  const itens: Array<{ id: string }> = body.items ?? body ?? []
  for (const it of itens) {
    if (it?.id) await request.delete(`/api/v1/entities/${it.id}`)
  }
}
