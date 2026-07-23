# 0076 — Créditos: reordenar em lote (elenco/equipe grandes) + foto para papéis-chave de equipe

> Mover alguém da 50ª posição do elenco para o topo hoje exige 49 cliques; e a
> seção "Equipe" mostra só texto, quebrando o padrão visual do "Elenco" logo
> acima.

| | |
|---|---|
| **ID** | 0076 |
| **Tipo** | feature/fix |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | done |
| **Docs relacionadas** | [04-modelo-de-dados](../docs/04-modelo-de-dados.md) |
| **Depende de** | 0029, 0073 |

---

## 1. Objetivo

Dois achados da auditoria de UX, ambos em torno de elenco/equipe, um na
edição e outro na exibição:

1. **Edição (`CreditsEditor`, `web/src/components/credits-editor.tsx`)**: a
   reordenação de elenco/equipe/produtoras usa um componente genérico
   (`SecaoCreditos`) com só duas setas (subir/descer um passo) por linha. Num
   elenco de ~50 pessoas (comum em qualquer produção de porte, como o próprio
   "A Origem" usado na auditoria), mover alguém da última posição para o topo
   exige 49 cliques — na prática, ninguém reordena elenco grande, a
   funcionalidade existe mas não escala para os dados reais do próprio app.
2. **Exibição (`rich-detail-view.tsx`)**: `ElencoGrid` mostra cartões com
   foto (via `PessoaCard`), no padrão do TMDB. Logo abaixo, `EquipeGrupos`
   vira uma lista de texto corrido agrupada por departamento, **sem nenhuma
   foto** — mesmo quando `CreditCrew.profile_url` já vem preenchido pela API
   (mesmo campo que `CreditCast` usa para a foto do elenco). A quebra de
   padrão visual faz a Equipe parecer uma lista "de sistema" ao lado de uma
   seção rica.

## 2. Jornada do usuário

**Reordenar (edição):**
1. Na tela `/entities/$id/edit`, o dono quer subir uma pessoa do fim da lista
   de elenco para perto do topo.
2. **Hoje**: clica na seta "subir" repetidamente, um passo por clique.
3. **Depois deste briefing**: arrasta a linha para a nova posição (drag-and-
   drop), OU informa um número de posição-alvo ao lado da linha ("mover para
   posição N") — as setas de um passo continuam existindo como alternativa
   acessível/teclado.

**Exibição (equipe com foto):**
1. O dono abre a tela de detalhe de um filme com elenco e equipe grandes.
2. **Hoje**: "Elenco" é uma grade de cartões com foto; "Equipe", logo abaixo,
   é texto corrido sem nenhuma foto.
3. **Depois deste briefing**: os papéis de equipe mais reconhecíveis
   (Direção/Roteiro/Fotografia) ganham foto, no mesmo tratamento visual do
   Elenco; os demais ~40 nomes de equipe continuam em lista textual compacta
   (não pesar a página inteira com fotos de cada um).

## 3. Escopo

**Dentro:**
- `SecaoCreditos` (`credits-editor.tsx`): mecanismo de reordenação em lote —
  drag-and-drop OU campo de posição numérica — além das setas existentes.
- `EquipeGrupos` (`rich-detail-view.tsx`): foto para os membros dos
  departamentos priorizados (mesmos já reconhecidos por
  `PRIORIDADE_DEPARTAMENTO`: Direção, Roteiro), reusando o mesmo tratamento
  visual do `PessoaCard`/`ElencoGrid` (ou uma variação compacta dele).

**Fora (explicitamente):**
- Foto para TODOS os ~40 nomes de equipe — pesaria a página; só os papéis
  priorizados ganham foto, o resto continua em texto.
- Mudar `agruparEquipe`/`PRIORIDADE_DEPARTAMENTO` (a lógica de agrupamento e
  ordem dos departamentos já está correta e não é o achado).
- Qualquer mudança no `CreditsEditor` fora da mecânica de reordenação (campos
  editáveis, remover, adicionar — inalterados).

## 4. Contrato técnico

### Frontend (Vite/React)

- `web/src/components/credits-editor.tsx`:
  - `SecaoCreditos`/`BotoesMover` (linhas ~580-620) ganham um mecanismo
    adicional de reordenação em lote. Duas opções, escolher a mais barata
    dado o estado atual do componente (lista simples em `useState`, sem
    nenhuma lib de drag-and-drop no projeto ainda):
    - (a) campo numérico "mover para posição N" ao lado de cada linha,
      reusando `moverItem`/o mesmo `reorder` já calculado ao salvar — mais
      barato, sem dependência nova;
    - (b) drag-and-drop (ex.: `@dnd-kit/core`, a lib mais comum no
      ecossistema React/shadcn) — mais fiel ao pedido da auditoria, mas
      introduz dependência nova (`web/package.json` — lembrar do restart do
      container de dev depois de instalar, `docker compose -f
      compose.dev.yaml restart web`).
  - As setas de um passo continuam funcionando como estão (alternativa
    acessível/teclado) — não removidas.
- `web/src/components/rich-detail-view.tsx`:
  - `EquipeGrupos`: para membros cujo `department` normalizado está em
    `PRIORIDADE_DEPARTAMENTO` (Direção/Roteiro — considerar também incluir
    Fotografia/Cinematography na lista de prioridade, citada na auditoria),
    renderizar com foto — reusando `PessoaCard` (mesmo componente do
    `ElencoGrid`) ou uma variação compacta dele, mantendo a navegação
    interno-first (`MembroNome`/`personId`) já existente.
  - Demais departamentos continuam na lista textual compacta atual
    (`MembroNome` + cargo), sem foto.

### Arquivos afetados

- `web/src/components/credits-editor.tsx` (+ `.test.tsx`).
- `web/src/components/rich-detail-view.tsx` (+ `.test.tsx`).
- `web/package.json` (+ `package-lock.json`, gerado com npm ≥ 11.16) — só se
  a opção (b), drag-and-drop, for a escolhida.

## 5. Critérios de aceite

- [ ] Num elenco/equipe com muitas pessoas, é possível mover um item para
      qualquer posição sem precisar de um clique por posição intermediária
      (drag-and-drop ou campo de posição-alvo).
- [ ] As setas de subir/descer um passo continuam funcionando como hoje.
- [ ] `PATCH` de créditos (`reorder`) continua recebendo a ordem final
      completa, como já acontece hoje — só o MECANISMO de reordenar na UI
      muda, o contrato de salvamento não.
- [ ] Na tela de detalhe, membros de equipe dos departamentos priorizados
      (Direção/Roteiro, e Fotografia se incluída) aparecem com foto quando
      `profile_url` estiver disponível; sem foto disponível, degrada
      graciosamente (mesmo comportamento que `PessoaCard` já tem para elenco
      sem foto).
- [ ] Os demais departamentos de equipe continuam em lista textual compacta,
      sem foto.
- [ ] `make e2e-cov` e `make gate-local` passam.

## 6. Notas de implementação / decisões

- **Por que depende de 0073?** Ambos tocam `credits-editor.tsx` — 0073 lê o
  estado "sujo" do componente (prop `onDirtyChange`), este briefing muda a
  mecânica de reordenação interna. Aterrissar 0073 primeiro evita um merge
  conflituoso entre os dois; a dependência é de ORDEM DE MERGE, não uma
  dependência funcional real (um não precisa da lógica do outro para
  funcionar).
- **Por que não foto para todo mundo na Equipe?** A própria auditoria pondera
  isso — ~40 fotos por item pesaria a página sem ganho proporcional (a
  maioria dos nomes de equipe não é reconhecida pelo dono do jeito que
  diretor/roteirista/fotografia costumam ser). Curadoria por departamento é
  mais barata e cobre o caso que realmente importa (paridade com o TMDB nos
  papéis que o dono reconhece).
