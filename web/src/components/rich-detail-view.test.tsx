import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { RichDetailView } from '@/components/rich-detail-view'
import type { Asset, CreditCast, CreditCrew, ExternalVideo } from '@/lib/api'
import { richDetailBase } from '@/test/detail-harness'
import { renderWithClient } from '@/test/render'
import type { RichDetail } from '@/lib/rich-detail'

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

// ── Hero imersivo: sinopse dentro do hero (0082) ─────────────────────────────

describe('RichDetailView — sinopse dentro do hero (0082)', () => {
  it('a sinopse aparece como paragrafo, sem heading "Sinopse" separada', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          description: 'Um hacker descobre a verdade.',
        })}
      />,
    )
    expect(
      screen.getByText('Um hacker descobre a verdade.'),
    ).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Sinopse' })).toBeNull()
  })

  it('sem sinopse, nada aparece no lugar dela', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({ description: null })}
      />,
    )
    expect(screen.queryByRole('heading', { name: 'Sinopse' })).toBeNull()
  })
})

// ── Trailer (0082 §4.3) ───────────────────────────────────────────────────────

function videoFixture(over: Partial<ExternalVideo> = {}): ExternalVideo {
  return {
    name: 'Trailer Oficial',
    key: 'abc123',
    site: 'YouTube',
    type: 'Trailer',
    official: true,
    url: null,
    ...over,
  }
}

describe('RichDetailView — botão de trailer na barra de ações (0082)', () => {
  it('com trailer do YouTube: o botão aparece e abre o MESMO player embutido', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({ videos: [videoFixture()] })}
      />,
    )
    const botao = screen.getByRole('button', { name: 'Assistir trailer' })
    fireEvent.click(botao)
    expect(screen.getByTitle('Trailer Oficial')).toHaveAttribute(
      'src',
      'https://www.youtube-nocookie.com/embed/abc123?autoplay=1',
    )
    fireEvent.click(screen.getByRole('button', { name: 'Fechar trailer' }))
    expect(screen.queryByTitle('Trailer Oficial')).toBeNull()
  })

  it('sem vídeo do tipo Trailer (só Teaser), o botão não aparece', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          videos: [videoFixture({ type: 'Teaser' })],
        })}
      />,
    )
    expect(
      screen.queryByRole('button', { name: 'Assistir trailer' }),
    ).toBeNull()
  })

  it('trailer não reproduzível (não-YouTube), o botão não aparece', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          videos: [videoFixture({ site: 'Vimeo', key: '' })],
        })}
      />,
    )
    expect(
      screen.queryByRole('button', { name: 'Assistir trailer' }),
    ).toBeNull()
  })

  it('sem vídeo algum, o botão não aparece', () => {
    renderWithClient(<RichDetailView source="tmdb" detail={richDetailBase()} />)
    expect(
      screen.queryByRole('button', { name: 'Assistir trailer' }),
    ).toBeNull()
  })
})

// ── Logo, banner e disco preferidos + tema musical (0082, só entity) ─────────

function assetFixture(over: Partial<Asset> = {}): Asset {
  return {
    id: 'a1',
    kind: 'logo',
    origin: 'user',
    preferred: true,
    is_principal: true,
    local_path: null,
    url: '/api/v1/entities/e1/assets/a1/file',
    ...over,
  }
}

// stubEntityFetch roteia `/assets`, `/theme` (HEAD) e `/historico/estado` (selo
// "Assistido", 0094) para as respostas dadas — as chamadas que o hero dispara
// quando `entityId` está presente. `estado` default vazio (sem selo).
function stubEntityFetch(
  assets: Asset[],
  temTema = false,
  estado: Record<string, unknown> = {},
) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      const u = String(url)
      if (u.includes('/theme')) {
        return temTema
          ? ({ ok: true, status: 200, json: async () => ({}) } as Response)
          : ({ ok: false, status: 404, json: async () => ({}) } as Response)
      }
      if (u.includes('/historico/estado')) {
        return {
          ok: true,
          status: 200,
          json: async () => ({ estado }),
        } as Response
      }
      if (u.includes('/assets')) {
        return { ok: true, status: 200, json: async () => assets } as Response
      }
      throw new Error(`rota nao mockada: ${u}`)
    }),
  )
}

// Atalho comum às três descrições abaixo (logo/banner/disco, selo "Assistido",
// tema musical): todas renderizam com o mesmo entityId="e1"/source="tmdb" —
// só o que `stubEntityFetch` devolve muda.
function renderComEntity(detailOver: Partial<RichDetail> = {}) {
  return renderWithClient(
    <RichDetailView
      detail={richDetailBase(detailOver)}
      source="tmdb"
      entityId="e1"
    />,
  )
}

// Idem para o caso "sem entityId": nenhuma das três seções busca `fetch`.
function stubFetchNaoDeveSerChamado() {
  const fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('RichDetailView — logo/banner/disco preferidos (0082, entity)', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('com asset kind=logo preferido, a logo substitui o título em texto', async () => {
    stubEntityFetch([assetFixture({ kind: 'logo' })])
    const { container } = renderComEntity()
    await waitFor(() =>
      expect(container.querySelector('img[alt="Duna"]')).toBeTruthy(),
    )
    expect(container.querySelector('img[alt="Duna"]')).toHaveAttribute(
      'src',
      '/api/v1/entities/e1/assets/a1/file',
    )
    expect(screen.queryByRole('heading', { level: 1, name: 'Duna' })).toBeNull()
  })

  it('sem asset kind=logo, o título em texto continua aparecendo (sem regressão)', async () => {
    stubEntityFetch([])
    renderComEntity()
    expect(
      await screen.findByRole('heading', { level: 1, name: 'Duna' }),
    ).toBeInTheDocument()
  })

  it.each([
    ['banner', 'b1'],
    ['disc', 'd1'],
  ] as const)(
    'com asset kind=%s preferido, ele aparece (asset id=%s)',
    async (kind, id) => {
      const url = `/api/v1/entities/e1/assets/${id}/file`
      stubEntityFetch([assetFixture({ id, kind, url })])
      const { container } = renderComEntity()
      await waitFor(() =>
        expect(container.querySelector(`img[src="${url}"]`)).toBeTruthy(),
      )
    },
  )

  it('sem entityId (tela do externo), não busca assets nem mostra logo/banner/disco', () => {
    const fetchMock = stubFetchNaoDeveSerChamado()
    renderWithClient(<RichDetailView detail={richDetailBase()} source="tmdb" />)
    expect(fetchMock).not.toHaveBeenCalled()
  })
})

// ── Selo "Assistido" (briefing 0094) ─────────────────────────────────────────

describe('RichDetailView — selo "Assistido" (0094)', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it.each([
    ['completed', true],
    ['started', false],
  ] as const)(
    'evento assistir=%s: selo "Assistido" visível=%s',
    async (status, deveAparecer) => {
      stubEntityFetch([], false, {
        assistir: {
          status,
          occurred_at: '2026-01-05T20:00:00Z',
          source: 'jellyfin-webhook',
        },
      })
      renderComEntity()
      if (deveAparecer) {
        expect(await screen.findByText(/Assistido em/)).toBeInTheDocument()
      } else {
        await waitFor(() =>
          expect(screen.queryByText(/Assistido em/)).toBeNull(),
        )
      }
    },
  )

  it('sem evento assistir, o selo não aparece', async () => {
    stubEntityFetch([], false, {})
    renderComEntity()
    await waitFor(() => expect(screen.queryByText(/Assistido em/)).toBeNull())
  })

  it('sem entityId (tela do externo), o selo não aparece', () => {
    stubFetchNaoDeveSerChamado()
    renderWithClient(<RichDetailView detail={richDetailBase()} source="tmdb" />)
    expect(screen.queryByText(/Assistido em/)).toBeNull()
  })
})

describe('RichDetailView — player do tema musical local (0082 §4.4)', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('com theme.* na pasta-raiz, mostra o botão "Tocar tema" (sem autoplay)', async () => {
    stubEntityFetch([], true)
    renderComEntity()
    const botao = await screen.findByRole('button', {
      name: 'Tocar tema musical',
    })
    expect(botao).toBeInTheDocument()
    // Sem autoplay: nenhum estado de "tocando" (rótulo "Pausar") antes da ação.
    expect(
      screen.queryByRole('button', { name: 'Pausar tema musical' }),
    ).toBeNull()
  })

  it('sem theme.*, o player não aparece', async () => {
    stubEntityFetch([], false)
    renderComEntity()
    await waitFor(() =>
      expect(screen.queryByRole('button', { name: /tema musical/ })).toBeNull(),
    )
  })
})

// ── Fileira de notas por fonte (0086) ─────────────────────────────────────────

describe('RichDetailView — fileira de notas por fonte (0086)', () => {
  it('aparece mesmo com 1 fonte só — não fica escondida esperando 2+ (decisão do dono)', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({ ratings: [{ source: 'tmdb', value: 7.4 }] })}
      />,
    )
    // O badge da fonte conhecida (TMDB) usa a LOGO svg (`aria-hidden`), não
    // texto — a nota em si (7.4) é texto visível ao lado.
    expect(screen.getByText('7.4')).toBeInTheDocument()
  })

  it('sem nota local nem ratings, a fileira inteira não aparece', () => {
    renderWithClient(<RichDetailView source="tmdb" detail={richDetailBase()} />)
    expect(screen.queryByText(/^\d\.\d$/)).toBeNull()
  })

  it('nota local + fonte SEM logo no registro cai no rótulo maiúsculo (fallback)', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          localRating: 9,
          ratings: [{ source: 'meufornecedor', value: 6.1 }],
        })}
      />,
    )
    expect(screen.getByLabelText('Sua nota: 9.0')).toBeInTheDocument()
    expect(screen.getByText('MEUFORNECEDOR')).toBeInTheDocument()
    expect(screen.getByText('6.1')).toBeInTheDocument()
  })

  it('duas fontes: um badge por entrada, lado a lado', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          ratings: [
            { source: 'tmdb', value: 7.4 },
            { source: 'fake', value: 8.0 },
          ],
        })}
      />,
    )
    expect(screen.getByText('7.4')).toBeInTheDocument()
    expect(screen.getByText('8.0')).toBeInTheDocument()
    expect(screen.getByText('FAKE')).toBeInTheDocument()
  })
})

// ── Fileira de links externos (0086) ──────────────────────────────────────────

describe('RichDetailView — fileira de links externos, logo após o hero (0086)', () => {
  it('aparece logo após o hero, ANTES da seção de Palavras-chave', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          externalLinks: { tmdb: 'https://www.themoviedb.org/movie/603' },
          keywords: ['simulação'],
        })}
      />,
    )
    const link = screen.getByRole('link', { name: 'TMDB' })
    const heading = screen.getByRole('heading', { name: 'Palavras-chave' })
    // DOCUMENT_POSITION_FOLLOWING no resultado de `link.compareDocumentPosition`
    // significa que `heading` vem DEPOIS de `link` no documento.
    expect(
      link.compareDocumentPosition(heading) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
    expect(link).toHaveAttribute('href', 'https://www.themoviedb.org/movie/603')
  })

  it('sem links externos, a fileira não aparece', () => {
    renderWithClient(<RichDetailView source="tmdb" detail={richDetailBase()} />)
    expect(screen.queryByRole('link', { name: 'TMDB' })).toBeNull()
  })

  it('fonte sem logo no registro cai no ícone genérico + nome em maiúsculas', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({
          externalLinks: { letterboxd: 'https://letterboxd.com/film/x' },
        })}
      />,
    )
    expect(screen.getByRole('link', { name: 'LETTERBOXD' })).toBeInTheDocument()
  })
})

// ── Botões "Baixar trailer"/"Baixar tema" (briefings 0102/0103, ADR-0025) ────

// Resposta Response-like mínima reaproveitada pelos stubs de fetch abaixo —
// só os campos que o cliente HTTP do app lê (`ok`/`status`/`json`).
function respostaJson(body: unknown, status = 200): Response {
  return { ok: status < 400, status, json: async () => body } as Response
}

// Roteia as chamadas COMUNS a qualquer teste com `entityId`: status do
// yt-dlp (0101), estado do histórico de consumo (selo assistido) e lista de
// assets (hero) — antes de cair nelas, delega pro `extra` de cada stub
// específico (trailer OU tema); `extra` devolve `undefined` quando a rota
// não é dele, pra seguir o roteamento comum.
function stubFetchComYtdlp(
  ytdlpDisponivel: boolean,
  extra: (u: string) => Response | undefined,
) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      const u = String(url)
      if (u.includes('/settings/ytdlp')) {
        return respostaJson({
          disponivel: ytdlpDisponivel,
          origem: ytdlpDisponivel ? 'baixado' : 'nenhum',
          custom_binary_path: null,
          detalhe: null,
        })
      }
      const resp = extra(u)
      if (resp) return resp
      if (u.includes('/historico/estado')) {
        return respostaJson({ estado: {} })
      }
      if (u.includes('/assets')) {
        return respostaJson([])
      }
      throw new Error(`rota nao mockada: ${u}`)
    }),
  )
}

// `jaBaixado` simula um trailer JÁ materializado antes do teste começar; um
// POST bem-sucedido em .../trailer/download também vira `jaBaixado=true`
// daí em diante (o invalidate da mutation refaz o HEAD).
function stubTrailerFetch({
  ytdlpDisponivel,
  jaBaixado = false,
}: {
  ytdlpDisponivel: boolean
  jaBaixado?: boolean
}) {
  let baixado = jaBaixado
  stubFetchComYtdlp(ytdlpDisponivel, (u) => {
    if (u.includes('/trailer/download')) {
      baixado = true
      return respostaJson({ local_path: '/lib/Duna (2024)-trailer.mp4' })
    }
    if (u.includes('/trailer')) {
      return baixado ? respostaJson({}) : respostaJson({}, 404)
    }
    if (u.includes('/theme')) {
      return respostaJson({}, 404)
    }
    return undefined
  })
}

describe('RichDetailView — botão "Baixar trailer" (0102, ADR-0025)', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('sem entityId (tela do externo), o botão não aparece mesmo com trailer', () => {
    renderWithClient(
      <RichDetailView
        source="tmdb"
        detail={richDetailBase({ videos: [videoFixture()] })}
      />,
    )
    expect(screen.queryByRole('button', { name: /Baixar trailer/ })).toBeNull()
  })

  it('sem trailer reproduzível, o botão não aparece (mesmo com entityId)', async () => {
    stubTrailerFetch({ ytdlpDisponivel: true })
    renderComEntity()
    await screen.findByRole('heading', { level: 1, name: 'Duna' })
    expect(screen.queryByRole('button', { name: /Baixar trailer/ })).toBeNull()
  })

  it('yt-dlp indisponível: o botão não aparece', async () => {
    stubTrailerFetch({ ytdlpDisponivel: false })
    renderComEntity({ videos: [videoFixture()] })
    await screen.findByRole('button', { name: 'Assistir trailer' })
    expect(screen.queryByRole('button', { name: /Baixar trailer/ })).toBeNull()
  })

  it('yt-dlp disponível e sem trailer local: baixar mostra "trailer baixado" ao final', async () => {
    stubTrailerFetch({ ytdlpDisponivel: true })
    renderComEntity({ videos: [videoFixture()] })
    const botao = await screen.findByRole('button', { name: 'Baixar trailer' })
    fireEvent.click(botao)
    expect(
      await screen.findByRole('link', { name: /Trailer baixado/ }),
    ).toHaveAttribute('href', '/api/v1/entities/e1/trailer')
  })

  it('trailer já baixado antes: mostra o link direto, sem oferecer baixar de novo', async () => {
    stubTrailerFetch({ ytdlpDisponivel: true, jaBaixado: true })
    renderComEntity({ videos: [videoFixture()] })
    expect(
      await screen.findByRole('link', { name: /Trailer baixado/ }),
    ).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Baixar trailer' })).toBeNull()
  })
})
