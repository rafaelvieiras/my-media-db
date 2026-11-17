import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { LanguageSelector } from '@/components/language-selector'
import { I18nProvider } from '@/lib/i18n'
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
    <I18nProvider>
      <LanguageProvider>
        <LanguageSelector />
      </LanguageProvider>
    </I18nProvider>,
  )
}

// Opens the control and returns its menu, so a test can query inside it.
async function abrirMenu() {
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Language' }))
  const menu = await screen.findByRole('menu')
  return { user, menu }
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('LanguageSelector', () => {
  it('offers the three interface languages and switches the UI', async () => {
    stubSettings({ primary: 'en', fallback: 'en', allowed: ['en'] })
    renderSelector()

    const { user, menu } = await abrirMenu()

    // English is the default of a browser with no stored preference.
    expect(
      within(menu).getByRole('menuitemradio', { name: 'English' }),
    ).toHaveAttribute('aria-checked', 'true')

    await user.click(
      within(menu).getByRole('menuitemradio', { name: 'Português (Brasil)' }),
    )

    expect(localStorage.getItem('mmdb.ui-lang')).toBe('pt-BR')
    // The trigger itself follows the choice.
    expect(
      await screen.findByRole('button', { name: 'Idioma' }),
    ).toBeInTheDocument()
  })

  it('lists the allowed metadata languages and switches the reading language', async () => {
    stubSettings({
      primary: 'pt-BR',
      fallback: 'en',
      allowed: ['pt-BR', 'en'],
    })
    renderSelector()

    const { user, menu } = await abrirMenu()
    // "English" is both an interface language and a metadata language (the names
    // are endonyms), so the metadata item is the LAST of the two.
    const itens = await within(menu).findAllByRole('menuitemradio', {
      name: 'English',
    })
    expect(itens).toHaveLength(2)

    await user.click(itens[1])

    expect(localStorage.getItem('mmdb.lang')).toBe('en')
    // The interface language is a different setting and stays untouched.
    expect(localStorage.getItem('mmdb.ui-lang')).toBeNull()
  })

  it('hides the metadata section when a single language is allowed', async () => {
    const fetchMock = stubSettings({
      primary: 'pt-BR',
      fallback: 'pt-BR',
      allowed: ['pt-BR'],
    })
    renderSelector()

    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    const { menu } = await abrirMenu()

    // The interface section is always there; the metadata one has nothing to offer.
    expect(
      within(menu).getByRole('menuitemradio', { name: 'English' }),
    ).toBeInTheDocument()
    expect(
      within(menu).queryByText('Metadata language'),
    ).not.toBeInTheDocument()
  })
})
