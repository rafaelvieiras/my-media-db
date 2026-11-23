import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { ErrorState } from '@/components/query-states'
import { I18nProvider } from '@/lib/i18n'

function renderError(props: React.ComponentProps<typeof ErrorState>) {
  return render(
    <I18nProvider>
      <ErrorState {...props} />
    </I18nProvider>,
  )
}

describe('ErrorState', () => {
  it('oferece um botão de tentar de novo quando recebe o refetch da query', async () => {
    const onRetry = vi.fn()
    renderError({ title: 'Could not load the queue', onRetry })

    const botao = screen.getByRole('button', { name: /try again/i })
    await userEvent.click(botao)

    expect(onRetry).toHaveBeenCalledOnce()
  })

  it('sem onRetry não inventa botão (a tela continua só informativa)', () => {
    renderError({ title: 'Could not load the queue' })

    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('não vaza a mensagem técnica do browser em inglês', () => {
    renderError({
      title: 'Could not load the queue',
      message: 'Failed to fetch',
      onRetry: () => {},
    })

    expect(screen.queryByText(/failed to fetch/i)).not.toBeInTheDocument()
    expect(screen.getByText('Could not load the queue')).toBeVisible()
  })

  it('mantém a mensagem de domínio, que diz o que fazer', () => {
    renderError({
      title: 'Could not save',
      message: 'The path is not reachable from the container.',
    })

    expect(
      screen.getByText('The path is not reachable from the container.'),
    ).toBeVisible()
  })
})
