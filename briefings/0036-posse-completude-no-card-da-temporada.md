# 0036 — Posse e completude no card da temporada

> A temporada (e a série) precisam mostrar, no próprio card, que têm conteúdo na
> biblioteca e se estão completas — sem o usuário ter que abrir cada uma às cegas.

| | |
|---|---|
| **ID** | 0036 |
| **Tipo** | fix/feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override (vertical séries) |
| **Status** | done |
| **Docs relacionadas** | [0034](0034-scanner-de-series-arquivo-episodio.md), [0032](0032-usabilidade-arvore-series-home-busca-breadcrumb.md) |
| **Depende de** | 0034 |

---

## 1. Objetivo

Hoje só o **episódio** tem marcação de posse (owned/arquivos). A **temporada não
tem marcação nenhuma**: ao abrir uma série, todos os cards de temporada parecem
iguais, e o usuário precisa **entrar em cada uma às cegas** para descobrir qual tem
episódio disponível — e só lá dentro vê a marcação por episódio. Além disso, nada
diz se a temporada está **completa** (tem todos os episódios catalogados) ou não.

O detalhe da temporada/série já expõe `ownership {owned_count, total_count}` (0034),
mas isso **não chega ao card** na lista de temporadas. Este briefing leva a posse
agregada + o estado de completude para o **card da temporada**.

## 2. Jornada do usuário

1. O usuário abre uma série e vê a lista de temporadas. **Cada card de temporada
   mostra**: "possui X/Y episódios" e um estado visual — **completa** (tem todos os
   catalogados), **parcial** (tem alguns) ou **vazia** (não tem nenhum na biblioteca).
2. De relance ele identifica as temporadas com conteúdo (as vazias ficam apagadas/
   discretas), sem abrir nenhuma.
3. Ao entrar numa temporada, os episódios possuídos já vêm marcados (0034) — nada
   muda aí.

## 3. Escopo

**Dentro:**
- O **card** de `season` (na lista de membros da série) carrega
  `ownership {owned_count, total_count}` (reusa `posseDaTemporada` do 0034).
- Frontend: o card de temporada renderiza o selo "possui X/Y episódios" + estado
  de completude (completa / parcial / vazia), com destaque visual para quem tem
  conteúdo e aparência discreta para as vazias.
- Por paridade e custo baixo, o card de `tv` (série) também pode carregar
  `ownership` quando aparecer em contexto de membros — opcional, não obrigatório.

**Fora (explicitamente):**
- Card de série na **home** com completude — o card de série já aparece como
  possuído (0034); completude na home fica como follow-up (não é a dor relatada).
- Mudar a marcação do **episódio** (já existe, 0034) ou o selo do **detalhe** da
  série/temporada (já existe, 0034).
- Barra de progresso elaborada — um selo textual + cor basta.

## 4. Contrato técnico

### Dados / API
- O **card DTO** (`entityCardDTO`) ganha um campo opcional `ownership`
  (`OwnershipInfo {owned_count, total_count}`, o mesmo schema do 0034), **nullable**
  — presente só para `season` (e opcionalmente `tv`); `null` para movie/episode/
  collection. Atualizar `openapi.yaml` (schema do card / `EntityCard`) + `gen:types`.
- **Não** mudar a query SQL do card (que serve todos os tipos): a posse agregada é
  **anotada no server** (pós-listagem), como `montarPosseAgregada`/`anotarInternos`
  já fazem — só para os cards `season` na resposta de `GET /entities/{id}/members`.

### Backend (Go)
- `internal/server/entities.go` — em `getEntityMembers` (ou onde monta os cards de
  membros), para cada card `season`, calcular `posseDaTemporada(card.id)` (helper já
  existente do 0034) e preencher `ownership`. Robusto a falha de leitura (degrada
  para `null`, não derruba a lista). Evitar N+1 explosivo: a série tem poucas
  temporadas (dezenas), aceitável; se ficar caro, considerar um batch — mas não
  otimizar prematuramente.

### Frontend (Vite/React)
- Componente do card de temporada (na lista de membros — ver `entity-members.tsx`
  `CardNumerado`/`MembrosSomenteLeitura` e `entity-detail.tsx`): renderizar o selo
  a partir de `ownership`. Estados:
  - `total>0 && owned==total` → **completa** (selo positivo, ex.: verde + "completa").
  - `0<owned<total` → **parcial** ("possui X/Y").
  - `owned==0` (ou `ownership` ausente com total conhecido) → **vazia** (discreto/
    apagado; deixa o olho ir para as que têm conteúdo).
  - `total==0` → sem selo.
- Estilo shadcn/ui, acessível (o estado não pode ser só cor — incluir texto).

### Arquivos afetados (previsão)
- `internal/server/entities.go`, `openapi.yaml`; `web/src/components/entity-members.tsx`
  e/ou `entity-detail.tsx`, `web/src/lib/api-types.ts` (gen). Testes correspondentes.

## 5. Critérios de aceite

- [ ] `GET /entities/{serieID}/members` traz `ownership` em cada card `season`.
- [ ] Na UI, cada card de temporada mostra "possui X/Y episódios" e o estado
      (completa/parcial/vazia); temporadas vazias ficam visualmente discretas.
- [ ] Movie/episode/collection nos cards seguem sem `ownership` (null).
- [ ] Catraca completa verde; smoke: numa série com posse parcial, as temporadas com
      conteúdo se distinguem das vazias sem abrir.

## 6. Notas de implementação / decisões

- Reusa `posseDaTemporada` (0034) — não duplicar a lógica de agregação.
- Anotação no server (não no resolver puro) — mesma fronteira do 0034/0032.
- "Completa" = possui todos os episódios **catalogados** da temporada (total = nº
  catalogado do provider, owned = com arquivo). Não é "todos os episódios que já
  foram ao ar" — é relativo ao catálogo, como o usuário já entende.

## 7. Fora deste briefing / follow-ups

- Completude no card de **série na home** (visão geral da coleção).
- Marcação de "faltando episódios X, Y, Z" (lista dos ausentes) no detalhe da
  temporada.
