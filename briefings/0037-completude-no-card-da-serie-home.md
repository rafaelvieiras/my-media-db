# 0037 — Completude no card da série (home)

> O card de série (home/listagens) mostra "X/Y temporadas" — quantas temporadas
> têm conteúdo na biblioteca — para o usuário ver de relance o quão completa está
> a série sem abrir.

| | |
|---|---|
| **ID** | 0037 |
| **Tipo** | fix/feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override (vertical séries) |
| **Status** | done |
| **Docs relacionadas** | [0036](0036-posse-completude-no-card-da-temporada.md), [0034](0034-scanner-de-series-arquivo-episodio.md) |
| **Depende de** | 0036 |

---

## 1. Objetivo

O 0036 marcou completude no card da **temporada**. Falta o mesmo no card da
**série** (home e demais listagens de cards): hoje a série só aparece como
"possuída" (tem algum arquivo), sem dizer o quão completa está.

## 2. Jornada do usuário

1. Na home, cada card de **série** mostra "X/Y temporadas" — X = temporadas com
   algum episódio na biblioteca, Y = total de temporadas catalogadas.
2. Séries sem nenhum conteúdo ficam discretas (apagadas); com conteúdo em todas as
   temporadas ficam destacadas (positivo).
3. De relance o usuário vê quais séries tem e quão completas estão, sem abrir.

## 3. Escopo

**Dentro:**
- **Definição consistente de posse do card** = "filhos diretos com conteúdo / total
  de filhos diretos" (raso, 1 nível):
  - card `season` → episódios com arquivo / total de episódios (**já é o 0036**).
  - card `tv` → **temporadas com conteúdo / total de temporadas** (novo).
- Anotar `ownership` (o campo já existente do 0036) nos cards `tv` da **listagem
  da home** (`GET /api/v1/entities`) e onde mais cards `tv` aparecem (busca).
- Frontend: o selo do card usa a **unidade por tipo** — `tv` → "temporadas",
  `season` → "episódios". Reusa `estadoCompletude`/`SeloCompletude` do 0036.

**Fora (explicitamente):**
- Mudar o `ownership` do **detalhe** da série (`ResolvedEntity`), que segue em
  **episódios profundos** (2/875) — é outro contexto, com espaço para o número fino.
- Contagem "profunda" de episódios no card da série (glance quer temporadas).
- Barra de progresso / percentuais elaborados.

## 4. Contrato técnico

### Backend (Go)
- `internal/server/entities.go`: hoje `anotarPosseCard` (0036) para `tv` usa
  `montarPosseAgregada` (episódios profundos). **Ajustar**: o card `tv` passa a
  receber posse **rasa por temporada** — `total_count` = nº de filhos `tv_season`;
  `owned_count` = nº desses cujos `posseDaTemporada(seasonID).owned > 0`. (Card
  `season` permanece episódios, como no 0036.) Assim o card é sempre raso e
  consistente; o detalhe permanece profundo.
- Aplicar a anotação na **listagem** (`listEntities`/`listarCards` da home) e na
  busca, não só em `/members`. Robusto a falha (degrada para `null`).
- **Custo**: por card `tv`, ~1 + nº de temporadas leituras. Biblioteca self-hosted
  (dezenas de séries) → aceitável. Se crescer, otimizar com uma query agregada
  (follow-up) — não otimizar prematuramente.

### Frontend (Vite/React)
- O selo de completude passa a rotular a **unidade conforme o tipo**: `tv` →
  "temporadas", `season` → "episódios" (ex.: "possui 3/38 temporadas"). Reusar o
  componente/estado do 0036 (`entity-badges.ts` `estadoCompletude`, `SeloCompletude`).
- Renderizar o selo no card de **série** na home/grid (onde os cards `tv` aparecem —
  ver o grid da home e o componente de card compartilhado). Estados: completa
  (X==Y), parcial (0<X<Y), vazia (X==0, discreto). Acessível (texto, não só cor).

### Arquivos afetados (previsão)
- `internal/server/entities.go` (ajuste em `anotarPosseCard` + hook na listagem/busca);
  `web/` (componente de card da home + `entity-badges.ts` para a unidade); testes.
  `openapi.yaml` não muda (campo `ownership` já existe no card).

## 5. Critérios de aceite

- [ ] `GET /api/v1/entities` traz `ownership` (temporadas) em cada card `tv`.
- [ ] Card de série na home mostra "X/Y temporadas" + estado (completa/parcial/vazia);
      séries sem conteúdo ficam discretas.
- [ ] Card `season` segue mostrando "X/Y episódios" (0036 intacto).
- [ ] Movie/episode/collection seguem sem `ownership`.
- [ ] Catraca completa verde; smoke: série com 1 temporada com conteúdo aparece
      "1/38 temporadas" na home.

## 6. Notas de implementação / decisões

- **Posse do card = rasa (1 nível), por tipo**: `tv`→temporadas, `season`→episódios.
  O detalhe fica **profundo** (episódios). Decisão para o card ser glanceável e
  consistente ("quanto do nível de baixo eu tenho").
- Reusa o campo `ownership` (0036) — sem mudança de schema/OpenAPI.

## 7. Fora deste briefing / follow-ups

- Query agregada única para posse de série (perf, se a biblioteca crescer muito).
- Lista de temporadas/episódios ausentes.
