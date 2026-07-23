import { describe, expect, it, vi } from 'vitest'
import { fireEvent, screen } from '@testing-library/react'
import { RichDetailView } from '@/components/rich-detail-view'
import type { CreditCast, CreditCrew } from '@/lib/api'
import { richDetailBase } from '@/test/detail-harness'
import { renderWithClient } from '@/test/render'

// Fábricas de créditos mockados (0029): o backend live ainda não retorna créditos,
// então as seções são exercitadas com dados montados aqui.
function ator(i: number, over: Partial<CreditCast> = {}): CreditCast {
  return {
    source_id: String(i),
    name: `Ator ${i}`,
    character: `Personagem ${i}`,
    order: i,
    ...over,
  }
}

function membro(
  name: string,
  department: string,
  job: string,
  over: Partial<CreditCrew> = {},
): CreditCrew {
  return { source_id: name, name, department, job, ...over }
}

// Link/useNavigate mockados (a view usa ambos nas seções de coleção/similares).
vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock,
  useNavigate: () => vi.fn(),
}))

describe('RichDetailView — selos de disponibilidade de artes (0049)', () => {
  it('mostra um selo por kind disponível, com rótulo + contagem, na ordem canônica', () => {
    renderWithClient(
      <RichDetailView
        detail={richDetailBase()}
        source="tmdb"
        availableArtwork={{ fanart: 40, poster: 23, logo: 17 }}
      />,
    )
    expect(screen.getByText('Pôster 23')).toBeInTheDocument()
    expect(screen.getByText('Fanart 40')).toBeInTheDocument()
    expect(screen.getByText('Logo 17')).toBeInTheDocument()
  })

  it('sem availableArtwork a faixa não aparece', () => {
    renderWithClient(<RichDetailView detail={richDetailBase()} source="tmdb" />)
    expect(screen.queryByText(/Pôster \d/)).toBeNull()
  })
})

describe('RichDetailView — créditos (elenco/equipe/produtoras, 0029)', () => {
  it('renderiza as três seções quando não-vazias', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          cast: [ator(0)],
          crew: [membro('Denis Villeneuve', 'Direção', 'Diretor')],
          companies: [{ source_id: 'w', name: 'Warner Bros.' }],
        })}
      />,
    )
    expect(screen.getByText('Elenco')).toBeInTheDocument()
    expect(screen.getByText('Equipe')).toBeInTheDocument()
    expect(screen.getByText('Produtoras')).toBeInTheDocument()
    expect(screen.getByText('Ator 0')).toBeInTheDocument()
    expect(screen.getByText('Denis Villeneuve')).toBeInTheDocument()
    expect(screen.getByText('Warner Bros.')).toBeInTheDocument()
  })

  it('seções somem quando os créditos estão vazios', () => {
    renderWithClient(<RichDetailView detail={richDetailBase()} source="tmdb" />)
    expect(screen.queryByText('Elenco')).toBeNull()
    expect(screen.queryByText('Equipe')).toBeNull()
    expect(screen.queryByText('Produtoras')).toBeNull()
  })

  it('elenco mostra top 24 e "Ver todos (N)" expande a lista inteira', () => {
    const cast = Array.from({ length: 30 }, (_, i) => ator(i))
    renderWithClient(
      <RichDetailView source="tmdb" detail={richDetailBase({ cast })} />,
    )
    // O 25º (order 24) e o 26º ficam ocultos no teto inicial de 24.
    expect(screen.getByText('Ator 0')).toBeInTheDocument()
    expect(screen.getByText('Ator 23')).toBeInTheDocument()
    expect(screen.queryByText('Ator 24')).toBeNull()
    expect(screen.queryByText('Ator 25')).toBeNull()
    // Expande: mostra todos e troca o rótulo para "Ver menos".
    fireEvent.click(screen.getByRole('button', { name: 'Ver todos (30)' }))
    expect(screen.getByText('Ator 24')).toBeInTheDocument()
    expect(screen.getByText('Ator 29')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Ver menos' }))
    expect(screen.queryByText('Ator 24')).toBeNull()
  })

  it('elenco respeita a ordem de bilhagem (order asc)', () => {
    // Fora de ordem na entrada; só o de menor `order` fica no teto de 1... mas aqui
    // provamos que o de `order` alto some quando há mais que o teto.
    const cast = Array.from({ length: 26 }, (_, i) => ator(25 - i)) // ordem invertida
    renderWithClient(
      <RichDetailView source="tmdb" detail={richDetailBase({ cast })} />,
    )
    // Ordenado por `order`, o Ator 0..23 aparece; 24 e 25 caem fora do teto de 24.
    expect(screen.getByText('Ator 0')).toBeInTheDocument()
    expect(screen.queryByText('Ator 24')).toBeNull()
    expect(screen.queryByText('Ator 25')).toBeNull()
  })

  it('série: o card de elenco mostra "· N eps"', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          type: 'series',
          cast: [ator(0, { character: 'Homer (voz)', episode_count: 12 })],
        })}
      />,
    )
    expect(screen.getByText(/Homer \(voz\) · 12 eps/)).toBeInTheDocument()
  })

  it('equipe agrupa por departamento (Direção/Roteiro primeiro, demais alfabético)', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          crew: [
            membro('Roger Deakins', 'Fotografia', 'Fotógrafo'),
            membro('Denis Villeneuve', 'Direção', 'Diretor'),
            membro('Jon Spaihts', 'Roteiro', 'Roteirista'),
            membro('Hans Zimmer', 'Som', 'Música'),
          ],
        })}
      />,
    )
    const grupos = screen
      .getAllByRole('heading', { level: 3 })
      .map((h) => h.textContent)
    expect(grupos).toEqual(['Direção', 'Roteiro', 'Fotografia', 'Som'])
    // Nome + cargo do membro renderizados juntos.
    expect(screen.getByText('Roger Deakins')).toBeInTheDocument()
    expect(screen.getByText(/— Fotógrafo/)).toBeInTheDocument()
  })

  it('elenco: card com person_id vira link para /person/{id} e ganha o selo "Na biblioteca"', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          cast: [ator(0, { name: 'Bryan Cranston', person_id: 'p1' })],
        })}
      />,
    )
    const link = screen.getByRole('link', {
      name: 'Ver Bryan Cranston na biblioteca',
    })
    expect(link).toHaveAttribute('href', '/person/p1')
    expect(screen.getByText('Na biblioteca')).toBeInTheDocument()
  })

  it('elenco: card sem person_id não é link interno nem tem o selo', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({ cast: [ator(0, { name: 'Ator Externo' })] })}
      />,
    )
    expect(
      screen.queryByRole('link', { name: /Ver Ator Externo na biblioteca/ }),
    ).toBeNull()
    expect(screen.queryByText('Na biblioteca')).toBeNull()
  })

  it('equipe: membro com person_id vira link para /person/{id}; sem person_id segue linkando ao TMDB', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          crew: [
            membro('Denis Villeneuve', 'Direção', 'Diretor', {
              person_id: 'p2',
            }),
            membro('Roger Deakins', 'Fotografia', 'Fotógrafo'),
          ],
        })}
      />,
    )
    const link = screen.getByRole('link', {
      name: 'Ver Denis Villeneuve na biblioteca',
    })
    expect(link).toHaveAttribute('href', '/person/p2')
    // Sem person_id, o nome segue linkando à fonte (TMDB) — comportamento inalterado.
    const externo = screen.getByRole('link', { name: 'Roger Deakins' })
    expect(externo).toHaveAttribute(
      'href',
      'https://www.themoviedb.org/person/Roger Deakins',
    )
    expect(screen.getByText(/— Fotógrafo/)).toBeInTheDocument()
  })

  it('equipe: Direção/Roteiro (priorizados) ganham foto, no padrão do PessoaCard do Elenco', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          crew: [
            membro('Denis Villeneuve', 'Direção', 'Diretor', {
              profile_url: '/api/v1/images?src=denis',
            }),
            membro('Roger Deakins', 'Fotografia', 'Fotógrafo', {
              profile_url: '/api/v1/images?src=roger',
            }),
          ],
        })}
      />,
    )
    // Direção (priorizado): foto real via <img alt="Foto de X">, como o Elenco.
    expect(
      screen.getByRole('img', { name: 'Foto de Denis Villeneuve' }),
    ).toHaveAttribute('src', '/api/v1/images?src=denis')
    // Fotografia (não priorizado): segue em texto compacto, sem foto — mesmo com
    // profile_url disponível.
    expect(
      screen.queryByRole('img', { name: 'Foto de Roger Deakins' }),
    ).toBeNull()
    expect(screen.getByText(/— Fotógrafo/)).toBeInTheDocument()
  })

  it('equipe: departamento priorizado sem profile_url degrada para o placeholder (como o Elenco)', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          crew: [membro('Denis Villeneuve', 'Direção', 'Diretor')],
        })}
      />,
    )
    expect(
      screen.queryByRole('img', { name: 'Foto de Denis Villeneuve' }),
    ).toBeNull()
    expect(screen.getAllByTestId('poster-placeholder').length).toBeGreaterThan(
      0,
    )
  })

  it('produtora sem logo cai no nome; com logo renderiza a imagem já proxyada', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          companies: [
            { source_id: '1', name: 'Legendary' },
            {
              source_id: '2',
              name: 'Warner Bros.',
              logo_url: '/api/v1/images?src=logo',
            },
          ],
        })}
      />,
    )
    // Sem logo → nome em texto.
    expect(screen.getByText('Legendary')).toBeInTheDocument()
    // Com logo → <img> com a URL proxyada direta (sem reproxyar) e alt = nome.
    const logo = screen.getByRole('img', { name: 'Warner Bros.' })
    expect(logo).toHaveAttribute('src', '/api/v1/images?src=logo')
  })
})
