# ADR-0030 — Tipos "de topo": biblioteca, busca e navegação genéricas por schema

- **Status:** Aceita
- **Data:** 2026-08-19
- **Agenda:** ver [12-roadmap](../12-roadmap.md) · **Spec:** [AGENTS.md](../../AGENTS.md) §3

## Contexto

Home (`RecentlyAddedSection`) e navbar (`BibliotecaNavItem`) já iteram sobre
`GET /entities/types` sem hardcode de tipo (briefings 0109/0110) — mas o
backend por trás disso tem **duas** exclusões de sub-nó hardcoded e
divergentes: `ListarTiposComContagem` (`internal/db/queries/entity.sql`,
usada por `/entities/types`) exclui só `season`/`episode`; `buscarCardsFTSSQL`
(`internal/server/search.go`, usada por `/search`) faz a mesma exclusão,
separadamente. Nenhuma das duas cobre `album`/`faixa` (sub-nós da árvore de
artista, ADR-0027) — resultado: se houver faixa/álbum catalogado, cada um
ganha carrossel próprio na home, entrada própria no dropdown da navbar e
aparece solto na busca, o que não faz sentido (faixa só existe pendurada num
álbum, álbum só existe pendurado num artista). O frontend, por sua vez, tem um
hardcode real de `movie|tv` em `MediaTypeFilter` (compartilhado por
`PosterGrid` e `SearchPanel`) que trava filtro de tipo a dois valores fixos. A
home mistura, na mesma tela, carrosséis "recentes" com uma listagem paginada
completa (`PosterGrid`) presa a esse mesmo filtro binário.

Isso contraria o princípio §3 do AGENTS.md ("universal e extensível sem tocar
o núcleo... schema novo... sem migration do core") na prática: cada schema
novo (música já demonstrou) exige tocar em múltiplos pontos hardcoded pra se
comportar direito na navegação.

## Decisão

1. **Registro único, em Go, de "tipo de topo"** (browsable diretamente: home,
   navbar, `/library/:entitySlug`, busca por padrão) vs. **"sub-nó"** (só
   acessível via drill-down do pai — `season`, `episode`, `album`, `faixa`).
   Os dois pontos hoje divergentes (`ListarTiposComContagem`,
   `buscarCardsFTSSQL`) passam a consultar essa **mesma** fonte — schema novo
   só precisa de uma entrada nela (ou fica de fora, se nasce sub-nó), nunca de
   editar as duas queries separadamente.
2. **`/search` fica genérico** sobre todos os tipos de topo por padrão
   (aceitando `type` livre para busca explícita/inclusão de sub-nó via
   `include`, mesmo padrão que `season`/`episode` já usam), com filtros
   estruturados equivalentes aos que `/entities` já expõe (tipo, posse,
   ordenação) em vez do enum fixo do frontend.
3. **Home mostra só carrosséis de tipos de topo** — a listagem completa/
   paginada (`PosterGrid`) sai da home.
4. **Nova rota `/library/:entitySlug`** para a biblioteca completa filtrada
   por um tipo de topo por vez (slug amigável mapeado para `entity.type`:
   `movies`→`movie`, `series`→`tv`, `artists`→`artista`). Cada carrossel da
   home ganha um link "Ver Todos" para essa rota.

## Consequências

- Fecha, de vez, "schema novo entra automaticamente na busca e na navegação"
  — próxima vertical (videoclipe, por exemplo) não exige tocar em `/search`
  nem na home, só declarar seu tipo como topo ou sub-nó no registro único.
- Reduz duas fontes de verdade hardcoded (e divergentes) para uma.
- Troca o contrato de URL pública da listagem filtrada: de `/?type=X`
  (search param da rota `/`) para `/library/:entitySlug` — decisão de
  implementação decide se a rota antiga vira redirect ou é removida.
- `MediaTypeFilter` (frontend) deixa de ser um enum fixo de dois valores e
  passa a derivar as opções do mesmo `GET /entities/types` que a navbar e a
  home já consultam.
- Decisões relacionadas: [AGENTS.md](../../AGENTS.md) §3 (universal sem tocar
  núcleo), [ADR-0029](0029-import-de-musica-local-first-por-faixa-possuida.md)
  (motivo original que expôs a lacuna: álbum/faixa vazando pra topo),
  briefings 0109/0110 (carrosséis/dropdown já genéricos no frontend).
