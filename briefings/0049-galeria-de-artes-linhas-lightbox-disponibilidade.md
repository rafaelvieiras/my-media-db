# 0049 — Galeria de artes: linhas por tipo + lightbox/zoom + selos de disponibilidade

> Redesenhar a exibição de artes (agnóstica de fonte): uma linha por tipo com o
> aspect certo, um lightbox quase-cheio com zoom e navegação, e selos no card do
> topo mostrando quais artes existem para o item.

| | |
|---|---|
| **ID** | 0049 |
| **Tipo** | feature (frontend) |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | ready |
| **Docs relacionadas** | [08-artwork](../docs/08-artwork-e-assets.md) |
| **Depende de** | 0048 |

---

## 1. Objetivo

Hoje a galeria mostra só pôster e fanart, **no mesmo aspect (2:3)** — a fanart
sai deformada/pequena — e **não há zoom** para ver a arte em tela aberta. Com a
fanart.tv (0048) chegam muitos tipos (logo, clearart, disc, banner, thumb,
characterart, artes de temporada). Este briefing entrega a exibição boa que o
dono pediu: **separada por tipo, com zoom, e com um resumo do que está disponível
no card do topo**. É **agnóstica de fonte** — vale para o TMDB que já temos e
para a fanart.tv.

## 2. Jornada do usuário

1. O usuário abre o detalhe de um filme/série. No **card do topo** vê selos:
   `Pôster 14 · Fanart 39 · Logo 17 · Clearart 24 · Disc — · Banner 5 · Thumb 16 ·
   Character 66` (só os tipos que existem, com contagem).
2. Rola até a galeria: **uma linha por tipo**, cada card no aspect certo do tipo,
   com scroll horizontal. A arte atualmente servida aparece marcada como preferida.
3. Clica numa arte → abre um **lightbox quase-cheio** com a imagem em alta,
   navegação prev/próxima dentro do tipo, selo de idioma e likes, e as ações
   **"definir como preferida"** e **"baixar pro disco"**.
4. Filtra por **idioma** dentro do tipo (ex.: só `pt`/`en`/textless).
5. Numa **temporada**, a galeria mostra as artes de temporada (poster/thumb/banner)
   daquela temporada.

## 3. Escopo

**Dentro:**
- Galeria reescrita: **uma linha (carrossel) por kind**, rótulo do tipo, aspect
  correto por kind (poster 2:3; fanart/thumb 16:9; logo/clearart/character
  transparente; disc quadrado; banner faixa). Kinds por tipo de entity.
- **Lightbox/zoom**: overlay quase-cheio, imagem em alta, prev/próxima no tipo,
  `lang`+`likes`, ações preferida/baixar/(deletar se for do dono). Teclado
  (setas/Esc) e fechar no backdrop.
- **Selos de disponibilidade** no `RichDetailView` (card do topo), lendo o
  `available: { [kind]: count }` do endpoint `GET .../artwork` (0048).
- Consumir o endpoint agrupado do 0048 (imagens já vêm proxied).
- Estados vazio/loading/erro por linha; skeleton nos thumbnails.

**Fora (explicitamente):**
- Backend do inventário/endpoint → **0048**.
- Edição de metadados da arte; reordenação manual.

## 4. Contrato técnico (Frontend — Vite/React)

- **Componentes**: reescrever `web/src/components/art-gallery.tsx` para linhas por
  kind; novo `art-lightbox.tsx` (overlay + zoom + navegação); estender
  `rich-detail-view.tsx` com a faixa de selos de disponibilidade.
- **Dados**: novo hook/consulta para `GET /api/v1/entities/{id}/artwork`
  (TanStack Query); tipos regenerados via `gen:types` (arquivos gerados **já
  formatados** — hook do prettier roda da raiz).
- **Aspect por kind** e **rótulos** centralizados (estender o que o 0044 criou:
  `ROTULO_KIND`, `kindsParaTipo`, `arteSrc`).
- **Idioma**: filtro por `lang` no lightbox; default segue principal→en→textless.
- **Acessibilidade**: foco preso no lightbox, `alt`, navegação por teclado.

### Arquivos afetados (previsão)
- `web/src/components/art-gallery.tsx` (reescrita), `web/src/components/art-lightbox.tsx` (novo)
- `web/src/components/rich-detail-view.tsx` (selos)
- `web/src/lib/api.ts` / `rich-detail.ts` (consulta + mapeamento), `api-types.ts` (regen)
- Testes vitest: linhas por kind, selos de disponibilidade, abrir/navegar/fechar
  lightbox, filtro de idioma.

## 5. Critérios de aceite

- [ ] Cada tipo de arte aparece em sua própria linha, no aspect correto (fanart
      não sai mais deformada em 2:3).
- [ ] Clicar numa arte abre o lightbox quase-cheio; setas navegam; Esc/back fecha.
- [ ] O card do topo mostra os selos de disponibilidade com contagem, só dos tipos
      que existem.
- [ ] "Definir como preferida" troca a arte servida; "baixar" materializa o asset
      local (via 0048).
- [ ] Filtro de idioma no lightbox funciona; default respeita principal→en→textless.
- [ ] Temporada mostra suas artes de temporada.
- [ ] `tsc -b` 0 erros; vitest verdes; cobertura web não regride; jscpd ok.

## 6. Notas / decisões

- Galeria **agnóstica de fonte**: TMDB e fanart.tv entram nas mesmas linhas; a
  origem vira só um selo. Resolve o "tamanho ruim / sem zoom" para todo tipo de
  arte, não só as novas.
- Reusa os helpers de aspect/rótulo/`arteSrc` do 0044. Ver [[fluxo-de-artes]].

## 7. Follow-ups

- Zoom com pan (arrastar) e níveis de zoom no lightbox.
- Reordenação/curadoria de artes; marcar múltiplas para baixar.
