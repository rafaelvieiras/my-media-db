# 0094 — UI do histórico de consumo: estado assistido + timeline no detalhe

> Terceiro briefing do épico de histórico (ADR-0022). Dá rosto ao que 0092
> (schema/API) e 0093 (webhook Jellyfin) já alimentam: um indicador de estado
> (assistido/em andamento) no card e no hero do item, e uma timeline de
> histórico no detalhe, com ação manual de registrar/editar/apagar evento.

| | |
|---|---|
| **ID** | 0094 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 5+ — Verticais e plugins |
| **Status** | draft |
| **Docs relacionadas** | [ADR-0022](../docs/adr/0022-historico-de-consumo-eventos-genericos-e-trackerprovider.md) |
| **Depende de** | 0092, 0093 |

---

## 1. Objetivo

Até aqui o histórico de consumo existe só como dado (API) — este briefing o
torna visível e editável pelo dono na UI que já existe (hero/detalhe do item,
`RichDetailView`), fechando o ciclo: assistir no Jellyfin → webhook →
histórico → refletido na tela do MyMediaDB sem nenhuma ação manual do dono.

## 2. Jornada do usuário

1. O dono assiste um filme via Jellyfin (0093 grava o histórico
   automaticamente). Ao abrir o item no MyMediaDB, o hero mostra um selo
   "Assistido" (com data) — sem ele ter feito nada manualmente.
2. No detalhe do item, uma seção "Histórico" (nova, ou dentro do dropdown
   "Gerenciar" já existente — decisão de UX na implementação) lista os
   eventos em ordem cronológica: quando começou, pausou, terminou.
3. Para um item sem Jellyfin (ex. assistiu num player qualquer, ou quer
   registrar histórico retroativo de antes de ter o webhook configurado), o
   dono usa uma ação manual "Marcar como assistido" (grava um evento
   `status=completed` com `occurred_at` = agora, ou uma data escolhida).
4. Um evento errado pode ser apagado direto da timeline (ação por item, com
   confirmação).
5. Estados vazios: item nunca assistido não mostra selo nem seção de
   histórico vazia poluindo a tela (mesmo padrão de "Similares" quando a
   lista vem vazia — a seção simplesmente não aparece).

## 3. Escopo

**Dentro:**
- Selo/indicador de estado no card e no hero do item (filme/série), lido de
  `GET /entities/{id}/historico/estado` (kind `assistir`).
- Seção de histórico no detalhe do item: lista dos eventos (`GET
  .../historico`), com data/status/origem (`source` — permite o dono
  diferenciar "veio do Jellyfin" de "registrei na mão").
- Ação manual "Marcar como assistido" / "Marcar como não assistido"
  (equivalente a criar/apagar o evento mais recente relevante).
- Apagar um evento individual da timeline.
- Estados de loading/erro/vazio consistentes com o resto do detalhe
  (`RichDetailView`).

**Fora (explicitamente):**
- Editar um evento existente (progresso/data) — só criar/apagar nesta
  entrega; edição fica como follow-up se a prática mostrar necessidade.
- Qualquer UI de configuração do webhook Jellyfin (token) — fica com o 0093
  ou um briefing de UI de configurações dedicado.
- Séries: granularidade de "quais episódios foram assistidos" na UI (a API
  já suporta por entity, mas a agregação visual "X/Y episódios assistidos"
  no card da temporada é UX própria, fora desta entrega).
- Gráficos/estatísticas de consumo (tempo total assistido, etc.) — puro
  follow-up, sem pedido do dono ainda.

## 4. Contrato técnico

A detalhar quando 0092/0093 aterrissarem (convenção do projeto para
briefings `draft` dependentes — ver Fase 3 do índice). Pontos já previstos:
- Componente novo `HistoricoSection`/`EstadoAssistidoBadge` em
  `web/src/components/rich-detail-view.tsx` (ou arquivo companheiro, mesmo
  padrão de `SimilaresRail`).
- Chamadas React Query para os 3 endpoints GET do 0092 + mutações para
  POST/DELETE.
- Tipos gerados via `gen:types` (contrato OpenAPI já existe desde o 0092).

## 5. Critérios de aceite

- [ ] Item com evento `completed` mais recente no histórico mostra selo
      "Assistido" no card e no hero.
- [ ] Item sem nenhum evento não mostra selo nem seção de histórico vazia.
- [ ] Seção de histórico lista eventos em ordem cronológica reversa, com
      status/data/origem visíveis.
- [ ] Ação manual "Marcar como assistido" cria o evento e a UI reflete sem
      reload manual (invalidação de query).
- [ ] Apagar um evento da timeline remove e atualiza o selo/estado
      derivado imediatamente.
- [ ] `tsc -b` zero-tolerância, `npm run lint`, `npm run test:coverage`
      verdes.
- [ ] `make gate-local` passa.

## 6. Notas de implementação / decisões

- A detalhar na revisão deste briefing quando 0092/0093 estiverem `done` —
  segue a convenção já usada nas Fases 2/3 (briefings `draft` detalhados
  quando o predecessor aterrissa), para não fixar decisão de UX antes de o
  dado real (shape do `estado`, latência, volume de eventos por item) estar
  disponível para testar contra.

## 7. Fora deste briefing / follow-ups

- Estatísticas/gráficos de consumo.
- Granularidade por episódio na UI de série.
- UI de configuração do webhook (token, passo a passo de setup no Jellyfin).
- Vertical de música/livro/jogo consumindo a mesma timeline (reaproveita
  componente genérico quando essas verticais existirem).
