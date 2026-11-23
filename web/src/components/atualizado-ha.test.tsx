import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { AtualizadoHa } from '@/components/atualizado-ha'

// The line is the live signal of the polling screens, so its three states matter:
// nothing before the first answer, "just now" for the first second and the elapsed
// seconds after that.
describe('AtualizadoHa', () => {
  it('não inventa linha antes da primeira resposta', () => {
    render(<AtualizadoHa seconds={null} />)

    expect(screen.queryByText(/updated/)).not.toBeInTheDocument()
  })

  it('diz que acabou de atualizar no primeiro segundo', () => {
    render(<AtualizadoHa seconds={1} />)

    expect(screen.getByText('updated just now')).toBeVisible()
  })

  it('mostra os segundos decorridos depois disso', () => {
    render(<AtualizadoHa seconds={12} />)

    expect(screen.getByText('updated 12s ago')).toBeVisible()
  })
})
