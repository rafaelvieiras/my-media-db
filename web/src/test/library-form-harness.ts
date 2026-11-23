import { screen, waitFor, within } from '@testing-library/react'
import type userEvent from '@testing-library/user-event'
import { expect } from 'vitest'

// SHARED scaffolding of the tests of the two datasource form routes
// (`/dashboard/library/new` and `/dashboard/library/$id/edit`): the submit of the form
// (it waits for the success navigation and hands back the body the API received) and
// the "leave without saving" guard, which is the same `AlertDialog` on both routes.
// Centralized in src/test (outside coverage and the duplication ratchet) so the two
// test files do not repeat the same `waitFor` + request inspection nor the guard's
// opening — each file only informs the verb/route its own screen sends.

type Usuario = ReturnType<typeof userEvent.setup>
type FetchMock = ReturnType<typeof vi.fn>
type NavegacaoMock = ReturnType<typeof vi.fn>

// enviarFormulario clicks the submit button, waits for the success navigation and
// returns the body (already parsed) of the request the submit sent to `rota` —
// the submit tests of both routes only vary in what they fill in before and in what
// they check after this common point.
export async function enviarFormulario({
  user,
  fetchMock,
  navigate,
  botao,
  rota,
  metodo,
  destino = '/dashboard/library',
}: {
  user: Usuario
  fetchMock: FetchMock
  navigate: NavegacaoMock
  botao: string | RegExp
  rota: string
  metodo: string
  destino?: string
}): Promise<Record<string, unknown>> {
  await user.click(screen.getByRole('button', { name: botao }))

  await waitFor(() => expect(navigate).toHaveBeenCalledWith({ to: destino }))
  const chamada = fetchMock.mock.calls.find(
    ([url, init]) =>
      String(url) === rota &&
      (init as RequestInit | undefined)?.method === metodo,
  )
  expect(chamada).toBeDefined()
  return JSON.parse(String(chamada?.[1]?.body))
}

// abrirGuardaDeSaida makes the form dirty, clicks "Back to libraries" and returns the
// open confirmation. `preencher` covers the edit screen, which has to clear the value
// pre-loaded from the API before typing the new one.
export async function abrirGuardaDeSaida({
  user,
  preencher,
}: {
  user: Usuario
  preencher?: (campo: HTMLElement) => Promise<void>
}): Promise<HTMLElement> {
  const campo = await screen.findByLabelText('Datasource path')
  if (preencher) {
    await preencher(campo)
  } else {
    await user.type(campo, '/mnt/filmes')
  }
  await user.click(screen.getByRole('link', { name: /Back to libraries/ }))
  return screen.findByRole('alertdialog', { name: 'Discard changes?' })
}

// descartarESair confirms the open guard ("Discard and leave"): the navigation that
// was blocked while the dialog was up happens after the confirmation. The click is
// scoped to the dialog — the rest of the page is `aria-hidden` while it is open.
export async function descartarESair({
  user,
  navigate,
  dialogo,
  destino = '/dashboard/library',
}: {
  user: Usuario
  navigate: NavegacaoMock
  dialogo: HTMLElement
  destino?: string
}): Promise<void> {
  expect(navigate).not.toHaveBeenCalled()

  await user.click(
    within(dialogo).getByRole('button', { name: 'Discard and leave' }),
  )
  expect(navigate).toHaveBeenCalledWith({ to: destino })
}
