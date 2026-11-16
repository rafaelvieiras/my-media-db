import { APIRequestContext, expect } from '@playwright/test'

// Journey isolation: they share ONE database (workers=1). Instead of coordinating
// "owner" ids of state, each test clears the library in beforeEach — that way every
// journey starts clean and does not depend on ordering. It uses only the public API
// (list + DELETE by id; the DELETE purges with CASCADE), with no test endpoint.
export async function resetBiblioteca(request: APIRequestContext): Promise<void> {
  const res = await request.get('/api/v1/entities?limit=1000')
  expect(res.ok()).toBeTruthy()
  const body = await res.json()
  const itens: Array<{ id: string }> = body.items ?? body ?? []
  for (const it of itens) {
    if (it?.id) await request.delete(`/api/v1/entities/${it.id}`)
  }
}
