import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { LanguageSelector } from '@/components/language-selector'
import { LanguageProvider } from '@/lib/language-context'
import type { LanguageSettings } from '@/lib/api'
import { renderWithClient } from '@/test/render'

function okJson<T>(body: T) {
  return { ok: true, status: 200, json: async () => body } as Response
}

// Mocks GET /settings/languages with the given config (source of the `allowed` ones).
function stubSettings(settings: LanguageSettings) {
  const fetchMock = vi.fn(async () => okJson(settings))
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderSelector() {
  return renderWithClient(
    <LanguageProvider>
      <LanguageSelector />
    </LanguageProvider>,
  )
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('LanguageSelector', () => {
  it('lista os permitidos e troca o idioma (persiste em localStorage)', async () => {
    const fetchMock = stubSettings({
      primary: 'pt-BR',
      fallback: 'en',
      allowed: ['pt-BR', 'en'],
    })
    renderSelector()

    const select =
      await screen.findByLabelText<HTMLSelectElement>('Idioma de leitura')
    // One option per allowed language, with a readable label.
    const opcoes = within(select).getAllByRole('option')
    expect(opcoes.map((o) => o.textContent)).toEqual([
      'Português (BR)',
      'Inglês',
    ])
    // Seeded on the primary one.
    expect(select).toHaveValue('pt-BR')

    await userEvent.selectOptions(select, 'en')

    expect(select).toHaveValue('en')
    expect(localStorage.getItem('mmdb.lang')).toBe('en')
    expect(fetchMock).toHaveBeenCalled()
  })

  it('retoma o idioma salvo (persistencia entre reloads) quando ainda permitido', async () => {
    localStorage.setItem('mmdb.lang', 'en')
    stubSettings({ primary: 'pt-BR', fallback: 'en', allowed: ['pt-BR', 'en'] })
    renderSelector()

    const select =
      await screen.findByLabelText<HTMLSelectElement>('Idioma de leitura')
    // The seed would have been pt-BR (primary), but the saved 'en' ∈ allowed wins.
    expect(select).toHaveValue('en')
  })

  it('cai no principal quando o idioma salvo nao e mais permitido', async () => {
    localStorage.setItem('mmdb.lang', 'fr')
    stubSettings({ primary: 'pt-BR', fallback: 'en', allowed: ['pt-BR', 'en'] })
    renderSelector()

    const select =
      await screen.findByLabelText<HTMLSelectElement>('Idioma de leitura')
    // 'fr' left (or never was in) the allowed list → it falls back to the primary pt-BR.
    expect(select).toHaveValue('pt-BR')
  })

  it('esconde-se quando ha um unico idioma permitido', async () => {
    const fetchMock = stubSettings({
      primary: 'pt-BR',
      fallback: 'pt-BR',
      allowed: ['pt-BR'],
    })
    renderSelector()

    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    expect(screen.queryByLabelText('Idioma de leitura')).not.toBeInTheDocument()
  })
})
