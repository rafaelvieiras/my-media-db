import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {
  I18nProvider,
  interpolate,
  matchUiLanguage,
  resolveUiLanguage,
  translate,
  useT,
  useUiLanguage,
} from '@/lib/i18n'
import { collections } from '@/lib/i18n/catalog/collections'
import { common } from '@/lib/i18n/catalog/common'
import { entity } from '@/lib/i18n/catalog/entity'
import { jobs } from '@/lib/i18n/catalog/jobs'
import { library } from '@/lib/i18n/catalog/library'
import { media } from '@/lib/i18n/catalog/media'
import { nav } from '@/lib/i18n/catalog/nav'
import { review } from '@/lib/i18n/catalog/review'
import { settings } from '@/lib/i18n/catalog/settings'

// Every domain file holds the three locales side by side, and `defineDomain`
// checks the shape at compile time. This walks the same tree at runtime because a
// cast (`as`) somewhere would silence the type check and ship a blank label —
// the test is the second lock, not a redundant one.
const DOMAINS = [
  ['common', common],
  ['nav', nav],
  ['library', library],
  ['entity', entity],
  ['review', review],
  ['settings', settings],
  ['jobs', jobs],
  ['collections', collections],
  ['media', media],
] as const

// Flat dotted paths of the string leaves.
function caminhos(no: unknown, prefixo = ''): string[] {
  if (typeof no === 'string') {
    return [prefixo]
  }
  if (typeof no !== 'object' || no === null) {
    return []
  }
  return Object.entries(no as Record<string, unknown>).flatMap(([k, v]) =>
    caminhos(v, prefixo ? `${prefixo}.${k}` : k),
  )
}

describe('catalog parity', () => {
  it.each(DOMAINS)(
    '%s has the same keys in en, pt-BR and es',
    (_nome, dominio) => {
      const en = caminhos(dominio.en).sort()
      expect(en.length).toBeGreaterThan(0)
      expect(caminhos(dominio['pt-BR']).sort()).toEqual(en)
      expect(caminhos(dominio.es).sort()).toEqual(en)
    },
  )

  it.each(DOMAINS)(
    '%s translates every key (no locale echoes the key back)',
    (nome, dominio) => {
      for (const chave of caminhos(dominio.en)) {
        const completa = `${nome}.${chave}`
        for (const lang of ['en', 'pt-BR', 'es'] as const) {
          // `translate` returns the key itself when it cannot resolve it.
          expect(translate(lang, completa)).not.toBe(completa)
        }
      }
    },
  )
})

describe('matchUiLanguage', () => {
  it('maps any regional tag to the supported locale', () => {
    expect(matchUiLanguage('pt-PT')).toBe('pt-BR')
    expect(matchUiLanguage('pt')).toBe('pt-BR')
    expect(matchUiLanguage('es-419')).toBe('es')
    expect(matchUiLanguage('en-GB')).toBe('en')
  })

  it('has no match for an unsupported or empty tag', () => {
    expect(matchUiLanguage('fr-FR')).toBeNull()
    expect(matchUiLanguage('')).toBeNull()
    expect(matchUiLanguage(null)).toBeNull()
  })
})

describe('resolveUiLanguage', () => {
  it('prefers the stored choice', () => {
    expect(resolveUiLanguage('es', 'pt-BR')).toBe('es')
  })

  it('falls back to the browser language when nothing is stored', () => {
    expect(resolveUiLanguage(null, 'pt-BR')).toBe('pt-BR')
  })

  it('falls back to English when the stored choice is not supported', () => {
    expect(resolveUiLanguage('fr', 'de-DE')).toBe('en')
    expect(resolveUiLanguage(null, null)).toBe('en')
  })
})

describe('interpolate', () => {
  it('replaces the placeholders it knows', () => {
    expect(interpolate('{n} items of {name}', { n: 3, name: 'Ana' })).toBe(
      '3 items of Ana',
    )
  })

  it('leaves an unknown placeholder visible instead of printing undefined', () => {
    expect(interpolate('{n} items', {})).toBe('{n} items')
  })
})

describe('translate', () => {
  it('reads the requested locale', () => {
    expect(translate('en', 'common.action.save')).toBe('Save')
    expect(translate('pt-BR', 'common.action.save')).toBe('Salvar')
    expect(translate('es', 'common.action.save')).toBe('Guardar')
  })

  it('falls back to English for a key the locale does not have', () => {
    // `common.state.notFound` exists everywhere, but the fallback path is the one
    // that matters: a catalog filled later must not render an empty label.
    expect(translate('es', 'common.state.notFound')).toBe('No encontrado')
    expect(translate('es', 'common.state.thisKeyDoesNotExist')).toBe(
      'common.state.thisKeyDoesNotExist',
    )
  })
})

// Small probe so the provider is exercised through the public hooks.
function Sonda() {
  const t = useT()
  const { lang, setLang } = useUiLanguage()
  return (
    <div>
      <span data-testid="lang">{lang}</span>
      <span data-testid="rotulo">{t('common.action.save')}</span>
      <button onClick={() => setLang('pt-BR')}>trocar</button>
    </div>
  )
}

describe('I18nProvider', () => {
  it('starts in English, switches at runtime and tracks the document lang', async () => {
    localStorage.clear()
    const user = userEvent.setup()
    render(
      <I18nProvider>
        <Sonda />
      </I18nProvider>,
    )

    expect(screen.getByTestId('lang')).toHaveTextContent('en')
    expect(screen.getByTestId('rotulo')).toHaveTextContent('Save')
    expect(document.documentElement.lang).toBe('en')

    await user.click(screen.getByRole('button', { name: 'trocar' }))

    expect(screen.getByTestId('lang')).toHaveTextContent('pt-BR')
    expect(screen.getByTestId('rotulo')).toHaveTextContent('Salvar')
    expect(document.documentElement.lang).toBe('pt-BR')
    expect(localStorage.getItem('mmdb.ui-lang')).toBe('pt-BR')
  })

  it('honours a language stored in a previous visit', () => {
    localStorage.setItem('mmdb.ui-lang', 'es')
    render(
      <I18nProvider>
        <Sonda />
      </I18nProvider>,
    )

    expect(screen.getByTestId('lang')).toHaveTextContent('es')
    expect(screen.getByTestId('rotulo')).toHaveTextContent('Guardar')
  })

  it('keeps working when localStorage is unavailable', async () => {
    const setItem = vi
      .spyOn(Storage.prototype, 'setItem')
      .mockImplementation(() => {
        throw new Error('quota')
      })
    const user = userEvent.setup()
    render(
      <I18nProvider>
        <Sonda />
      </I18nProvider>,
    )

    // The choice still applies for this session; only the persistence fails.
    await user.click(screen.getByRole('button', { name: 'trocar' }))
    expect(screen.getByTestId('lang')).toHaveTextContent('pt-BR')

    setItem.mockRestore()
    localStorage.clear()
  })
})
