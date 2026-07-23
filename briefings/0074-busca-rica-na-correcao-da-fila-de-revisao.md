# 0074 — Busca rica (pôster/sinopse) na correção da fila de Revisão

> A caixa "Corrigir" da fila de Revisão mostra os mesmos resultados externos
> da busca principal, mas em texto puro — sem pôster, sinopse ou o selo "Na
> biblioteca". Este briefing traz o mesmo tratamento visual para lá.

| | |
|---|---|
| **ID** | 0074 |
| **Tipo** | fix |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | done |
| **Docs relacionadas** | [07-enriquecimento-providers](../docs/07-enriquecimento-providers.md) |
| **Depende de** | 0014, 0018, 0026 |

---

## 1. Objetivo

A auditoria de UX comparou lado a lado dois pontos do app que fazem
essencialmente a mesma busca (`searchQueryOptions`, o mesmo hook por trás de
`/search`): a página `/search` (busca principal do menu) e o painel
"Corrigir" inline da fila de Revisão (`CorrigirPainel`/`ResultadosCorrecao`,
em `web/src/routes/review.tsx`).

O código confirma a causa raiz: os dois **já chamam o mesmo endpoint**
(`searchQueryOptions`), mas `ResultadosCorrecao` (dentro de `review.tsx`)
renderiza cada resultado como uma linha de texto puro — só título e ano,
nenhuma miniatura de pôster, nenhuma sinopse, nenhum selo "Na biblioteca". Já
`/search` usa `ExternalResultCard` (`web/src/components/external-result-card.tsx`),
com pôster (via proxy de imagem), sinopse truncada, e o selo "Na biblioteca"
quando o resultado já é uma entity interna (interno-first, 0026).

É exatamente no momento de **corrigir um item ambíguo** — a fila de Revisão
existe para casos de baixa confiança/múltiplos candidatos — que o dono mais
precisa de pistas visuais para diferenciar títulos parecidos, e é aí que a
interface esconde justamente o pôster.

## 2. Jornada do usuário

1. Um item cai na fila de Revisão com baixa confiança (ex.: "nenhum candidato
   encontrado" ou confiança abaixo do limiar).
2. O dono clica "Corrigir" e digita o título correto na caixa inline.
3. **Hoje**: a lista de resultados é só texto (título + ano), sem miniatura.
4. **Depois deste briefing**: cada resultado aparece como o mesmo card rico
   usado em `/search` — pôster, título, ano, sinopse truncada — com um botão
   "Usar este" no lugar de "Importar" (a ação aqui é corrigir a identidade do
   item já existente, não importar um novo).
5. Se o resultado escolhido já corresponde a uma entity interna (já
   catalogada), o card mostra o mesmo selo "Na biblioteca" que `/search` já
   mostra — dando ao dono o mesmo aviso de possível duplicata que ele já tem
   em outro lugar do app.

## 3. Escopo

**Dentro:**
- Reaproveitar `ExternalResultCard` (ou uma variação dele) dentro de
  `ResultadosCorrecao`, trazendo pôster + sinopse + selo "Na biblioteca" para
  a busca de correção.
- Trocar a ação do card nesse contexto: em vez de `ImportarButton`, um botão
  "Usar este" que chama `correctMatch` (mutação já existente em
  `CorrigirPainel`).

**Fora (explicitamente):**
- Mudar o endpoint/ranking de busca em si (`searchQueryOptions` já é
  compartilhado; não há endpoint duplicado para unificar, só apresentação).
- Trazer os toggles de sub-nós (Temporadas/Episódios, 0033) para a correção —
  a correção troca a IDENTIDADE do item já existente, não busca sub-nós.
- Mudar o fluxo de fusão de duplicata (0051) — esse já dispara a partir de
  `correctMatch` como está, sem alteração aqui.

## 4. Contrato técnico

### Frontend (Vite/React)

- `web/src/components/external-result-card.tsx`: adicionar um modo
  alternativo à ação do rodapé do card. Duas formas equivalentes, a
  implementação escolhe a mais barata dado o código atual:
  - (a) uma prop `action` (`{ kind: 'importar' } | { kind: 'corrigir', onEscolher:
    (r: ExternalResult) => void, pendente?: boolean }`), com o componente
    decidindo internamente qual botão renderizar no lugar de
    `ImportarButton`/badge "Na biblioteca"; ou
  - (b) extrair o miolo visual (pôster+título+ano+sinopse, hoje inline dentro
    de `ExternalResultCard`) para um subcomponente `ExternalResultContent`
    reutilizado por dois wrappers finos (`ExternalResultCard` para importar,
    um novo `CorrectMatchResultCard` para corrigir) — evita inchar as props
    de um componente só.
  - Em qualquer uma das duas formas: quando `internalId` já existe (a busca
    aponta para um item já catalogado), a correção ainda deve permitir
    escolher (é justamente o caso de fundir duplicata, 0051) — não esconder
    "Usar este" atrás do selo "Na biblioteca" como acontece hoje com
    "Importar".
- `web/src/routes/review.tsx` (`ResultadosCorrecao`): troca a `<ul>` de linhas
  de texto pelo card escolhido acima, mantendo os mesmos estados
  (pending/error/vazio) já implementados.

### Arquivos afetados

- `web/src/components/external-result-card.tsx` (+ `.test.tsx`).
- `web/src/routes/review.tsx` (+ `.test.tsx`) — só a função
  `ResultadosCorrecao`/`CorrigirPainel`, resto da tela intocado.

## 5. Critérios de aceite

- [ ] A busca de correção na fila de Revisão mostra pôster (proxyado) e
      sinopse truncada de cada resultado externo, no mesmo padrão visual de
      `/search`.
- [ ] Um resultado que já é uma entity interna mostra o selo "Na biblioteca" e
      ainda permite "Usar este" (não desaparece a ação, diferente do fluxo de
      importar).
- [ ] Escolher "Usar este" continua chamando `correctMatch` e fechando/atualizando
      a fila como já acontece hoje.
- [ ] Nenhuma duplicação de markup medida pelo jscpd entre `ExternalResultCard`
      e o novo caminho de correção (reuso, não cópia).
- [ ] `make e2e-cov` e `make gate-local` passam.

## 6. Notas de implementação / decisões

- **Por que não é um bug de endpoint/ranking?** A auditoria original comparou
  buscas com termos diferentes ("Matrix" na correção vs. "The Matrix" em
  `/search"), o que confunde relevância de busca com apresentação. Lendo o
  código dos dois pontos, ambos já chamam `searchQueryOptions` — o gap real e
  verificável é puramente visual (ausência de pôster/sinopse/selo), e é nisso
  que este briefing foca.
