# 0109 — Home: "Adicionados recentemente" em carrosséis por schema

> Substitui a home (hoje um grid único misto) por uma seção "Adicionados
> recentemente" com um carrossel horizontal por schema (filme, série, álbum,
> artista...), 10 itens por slide, responsivo — cada carrossel busca seus
> próprios dados.

| | |
|---|---|
| **ID** | 0109 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 5+ — Verticais e plugins (música) |
| **Status** | done |
| **Docs relacionadas** | [03-arquitetura](../docs/03-arquitetura.md) |
| **Depende de** | 0107 (para haver schema de música com dado real pra exibir) |

---

## 1. Objetivo

A home hoje (`web/src/routes/index.tsx`, `PosterGrid`) é um grid único
paginado, ordenado só por título, com um filtro de tipo binário
(`movie`/`tv`/`all`) que nem cobre os tipos de música (`artista`/`album`)
já existentes desde 0105-0107. Não há noção de "adicionado recentemente" —
a única ordenação suportada no backend é por título
(`ListarEntityCards`/`ListarEntityCardsDesc`, `internal/server/entities.go`).

Este briefing dá à home uma seção de destaque por schema — o dono abre a
home e vê, separadamente, o que entrou recentemente em cada categoria
(filmes, séries, álbuns, artistas...), em vez de uma lista misturada
ordenada por título.

## 2. Jornada do usuário

1. O dono abre a home (`/`).
2. Para cada schema com pelo menos 1 item (`movie`, `tv`, `album`,
   `artista` — e qualquer tipo futuro, sem hardcode de lista fixa quando
   possível), aparece uma seção "Adicionados recentemente" com um
   carrossel horizontal mostrando até 10 itens, ordenados do mais recente
   pro mais antigo (`created_at` desc).
3. O carrossel é navegável (setas/arraste) e responsivo — em telas
   estreitas mostra menos cards por vez, mantendo a rolagem horizontal.
4. Clicar num card navega pro detalhe da entity, igual ao grid já faz hoje.
5. O grid completo/paginado (uso atual da home) continua existindo abaixo
   ou em rota própria — este briefing **adiciona** a seção de destaque, não
   remove a navegação/filtro existente (ver decisão de escopo abaixo).

## 3. Escopo

**Dentro:**
- Backend: novo valor de `sort` em `GET /api/v1/entities` —
  `sort=-created_at` (mais recente primeiro), reaproveitando o padrão já
  existente de `ListarEntityCards`/`ListarEntityCardsDesc`
  (`internal/db/queries/entity.sql:170-190`, que já ordena por
  `created_at` para outro propósito — auto-match determinístico) — criar a
  variante exposta via API com `LIMIT`.
- Descoberta de schemas com dado: endpoint ou query que devolve a lista de
  `type` distintos com pelo menos 1 entity `owned` (evita a home
  hardcodar `['movie','tv','album','artista']` no frontend e quebrar
  quando um tipo novo aparecer). Pode ser um novo endpoint leve
  (`GET /api/v1/entities/types` → `[{type, count}]`) ou reaproveitar
  `GET /health/system` se o shape já servir (avaliar na implementação).
- Frontend: componente `RecentlyAddedRail` (nome de implementação livre) —
  um carrossel por schema, usando `Carousel` do shadcn (`npx shadcn add
  carousel`, traz `embla-carousel-react`; hoje não instalado). 10 itens por
  slide, responsivo (breakpoints do Tailwind já usados no projeto).
- Cada carrossel dispara sua própria `useQuery` (`entitiesQueryOptions`
  com `type` + `sort=-created_at` + `limit=10`), independente dos demais —
  requisito explícito do dono ("cada slide deve puxar separadamente").
- Rótulo de cada seção usa o mesmo texto amigável de tipo já usado em
  `MediaTypeFilter`/badges existentes (não expor `type` cru tipo `tv` sem
  tradução).

**Fora (explicitamente):**
- Reescrever o grid paginado existente — este briefing só adiciona a seção
  de destaque acima dele (ou em posição a decidir na implementação, mas
  sem remover o grid).
- Ordenação/curadoria manual de destaques (ex.: "fixar item") — é sempre
  automático por `created_at`.
- Skeleton/loading state elaborado além do padrão já usado no projeto
  (reaproveitar o que já existe para o grid).

## 4. Contrato técnico

### Dados / modelo
- Nenhuma migration — `entity.created_at` já existe
  (`internal/db/migrations/0002_schema_nucleo.sql:37`).

### API
- `GET /api/v1/entities?type=X&sort=-created_at&owned=true&limit=10&lang=..`
  — reaproveita o endpoint existente, só adiciona o valor de `sort`.
- Novo (ou reaproveitado) endpoint para listar tipos com dado — decidir
  forma exata na implementação; documentar a escolha nas notas.

### Backend (Go)
- `internal/db/queries/entity.sql`: nova query `ListarEntityCardsRecentes`
  (ou nome equivalente), mesmo padrão de `ListarEntityCards*`, ordenando
  `created_at DESC, id` com `LIMIT`.
- `internal/server/entities.go`: `parseSortFiltro` (ou equivalente) ganha
  o valor `-created_at`/`created_at`.
- Endpoint de tipos-com-dado (novo handler pequeno, se optar por essa
  forma).

### Frontend (Vite/React)
- `web/src/routes/index.tsx`: adicionar a seção de carrosséis acima/junto
  do `PosterGrid` existente.
- `web/src/components/ui/carousel.tsx` (gerado via shadcn CLI).
- `web/src/components/recently-added-rail.tsx` (novo) — um carrossel por
  schema, busca independente.
- `web/src/lib/queries.ts`/`api.ts`: novo helper de query com `sort`
  parametrizável (se `entitiesQueryOptions` já aceitar `sort` livre,
  reaproveitar; senão, estender).

### Arquivos afetados
- `internal/db/queries/entity.sql`, sqlc regenerado
- `internal/server/entities.go`
- `web/src/routes/index.tsx`
- `web/src/components/recently-added-rail.tsx` (novo)
- `web/src/components/ui/carousel.tsx` (novo, gerado)
- `web/src/lib/queries.ts`, `web/src/lib/api.ts`
- `web/package.json` (dependência `embla-carousel-react`)

## 5. Critérios de aceite

- [ ] `go test ./internal/server/... -count=1` cobre `sort=-created_at`
      devolvendo os itens mais recentes primeiro.
- [ ] Frontend: teste de componente/E2E cobrindo pelo menos 1 carrossel
      renderizando itens reais e navegação (setas ou swipe).
- [ ] Responsivo: validar manualmente (viewport mobile/desktop) que o
      carrossel não quebra layout nem gera scroll horizontal na página
      inteira (só dentro do próprio carrossel).
- [ ] Schema sem nenhum item não renderiza seção vazia (sem "Adicionados
      recentemente" fantasma pra tipo zerado).
- [ ] `tsc -b` zero-tolerância, `npm run lint`, `npm run test:coverage`.
- [ ] `go build ./...`, `go vet ./...`, `go test ./... -count=1` verdes.
- [ ] `make gate-local` limpo antes do commit final.

## 6. Notas de implementação / decisões

- Depende de 0108 apenas na medida em que roda melhor com progresso visível
  de scan (não é dependência técnica dura) — mas depende de 0107 de fato,
  porque sem dado de música catalogado o carrossel de álbum/artista fica
  sempre vazio na validação.
- Decidir na implementação: posição da seção de destaque na home (acima do
  grid é o padrão mais comum desse tipo de UI — tipo Netflix/Jellyfin — mas
  registrar a escolha).

## 7. Fora deste briefing / follow-ups

- Curadoria/fixar item em destaque — avaliar demanda depois.
- Carrossel de "continuar assistindo"/outros critérios além de "recente" —
  fora de escopo, não pedido.
- Backend (`sort=-created_at`, `GET /entities/types`) implementado nesta
  rodada, reaproveitado por 0110. `openapi.yaml` estava desatualizado desde
  0105-0107 (faltava `album`/`artista`/`faixa` no enum de `EntityCard.type`)
  — corrigido junto.
- Validação em produção adiada para a rodada de deploy conjunta com
  0108/0110/0111.
