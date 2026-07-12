# 0038 — Badge de posse no topo-direito da capa (sem apagar ausentes)

> Trocar o card apagado (opacity) dos itens sem conteúdo por um badge no
> topo-direito que só aparece quando o item TEM na biblioteca.

| | |
|---|---|
| **ID** | 0038 |
| **Tipo** | fix |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override (vertical séries) |
| **Status** | done |
| **Docs relacionadas** | [0034](0034-scanner-de-series-arquivo-episodio.md), [0036](0036-posse-completude-no-card-da-temporada.md), [0037](0037-completude-no-card-da-serie-home.md) |
| **Depende de** | 0037 |

---

## 1. Objetivo

Hoje o card de série/temporada **vazio** (sem conteúdo) recebe `opacity-60` — a capa
fica "esbranquiçada"/apagada. O dono prefere o inverso: **capa sempre normal**, e um
**badge no topo-direito** indicando quando o item **está na biblioteca** (tem
conteúdo). Ausente → **sem badge**, capa normal. Isso comunica melhor "o que eu
tenho" sem escurecer nada.

## 2. Jornada do usuário

1. No grid (home/busca/membros), a capa de todo item aparece **normal** (sem
   apagamento).
2. Itens **na biblioteca** ganham um badge discreto no **canto superior direito**:
   filme/episódio → "na biblioteca"; série/temporada → o selo de completude
   (**completa** ✓ ou **X/Y** parcial).
3. Itens **ausentes/vazios** não têm badge — a capa fica limpa.

## 3. Escopo

**Dentro:**
- Remover o `opacity-60` (estado `vazia`) do `PosterCard`.
- Mover o indicador de **posse** para um overlay no **topo-direito**:
  - `movie`/`episode`/`collection`: badge "na biblioteca" quando `owned` (hoje é o
    `PosseBadge`, que fica no topo-esquerdo — passa para o topo-direito).
  - `season`/`tv` (tipos com posse agregada): o **selo de completude** (0036/0037) —
    **completa** (verde ✓) ou **parcial** ("X/Y", com a unidade por tipo) — no
    topo-direito. **Vazio** (`owned_count==0`) → **sem badge**.
- Eliminar a redundância atual (série com conteúdo hoje mostra "na biblioteca" +
  selo de completude): para `season`/`tv` vale só o selo; para os demais, "na
  biblioteca".
- O badge de **revisão/"não identificado"** (`EstadoMatchBadge`) permanece no
  **topo-esquerdo** (é "precisa de atenção", não posse).

**Fora (explicitamente):**
- Mudar a lógica de posse/ownership no backend (nada muda; é só apresentação).
- Números de completude no detalhe (seguem como estão).

## 4. Contrato técnico

### Frontend (Vite/React)
- `web/src/components/poster-card.tsx`:
  - Tirar `estado === 'vazia' && 'opacity-60'` do container.
  - Criar overlay `absolute top-1.5 right-1.5` com o indicador de posse:
    - tipos com completude → `SeloCompletude` (que já renderiza completa/parcial e
      retorna `null` quando vazio/sem ownership); posicionar no canto.
    - demais → `PosseBadge` (owned) no canto.
  - Manter `EstadoMatchBadge` no overlay `top-1.5 left-1.5`.
  - O selo no canto deve caber (compacto). Pode encurtar o texto visível (ex.: "X/Y")
    mantendo a unidade no `title`/`aria-label` (acessível — não só cor).
- Ajustar/estender `poster-card.test.tsx`: ausente/vazio → sem badge e **sem**
  `opacity-60`; série completa → selo verde no topo-direito; série parcial → "X/Y";
  filme owned → "na biblioteca" no topo-direito; filme não-owned → sem badge.

### Arquivos afetados (previsão)
- `web/src/components/poster-card.tsx`, `web/src/components/poster-card.test.tsx`.
  (Sem backend, sem openapi, sem gen:types.)

## 5. Critérios de aceite

- [ ] Nenhum card fica apagado (`opacity-60` removido).
- [ ] Item na biblioteca → badge no topo-direito (filme/episódio "na biblioteca";
      série/temporada completa ✓ ou parcial "X/Y").
- [ ] Item ausente/vazio → sem badge, capa normal.
- [ ] Badge de revisão segue no topo-esquerdo.
- [ ] `tsc -b` 0, `npm test` verde; catraca completa verde.

## 6. Notas de implementação / decisões

- Posse (topo-direito) vs. atenção/revisão (topo-esquerdo): separação clara de
  significado no card.
- Série/temporada usa o selo de completude como indicador de posse (mais rico que um
  booleano); filme/episódio usa "na biblioteca". Vazio nunca mostra badge.

## 7. Fora deste briefing / follow-ups

- Ícone/estilo do badge de "na biblioteca" poderia virar um ✓ compacto por
  consistência com o "completa" — deixado a critério da implementação.
