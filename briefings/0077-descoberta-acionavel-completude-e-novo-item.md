# 0077 — Descoberta acionável: banner de completude com ação por item + cross-link "Novo item" ↔ Busca

> O banner "Catalogação incompleta" lista o que falta mas não diz o que fazer
> a respeito; e quem clica "Novo item" esperando importar cai num formulário
> em branco sem saber que a Busca resolveria melhor.

| | |
|---|---|
| **ID** | 0077 |
| **Tipo** | fix |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | done |
| **Docs relacionadas** | [04-modelo-de-dados](../docs/04-modelo-de-dados.md) |
| **Depende de** | 0030, 0057, 0058 |

---

## 1. Objetivo

Dois achados de descoberta/CTA, pequenos individualmente, bundlados aqui por
serem do mesmo tipo de problema (informação existe, ação não é óbvia):

1. **Banner "Catalogação incompleta"** (`SecaoCompletudeIncompleta`, em
   `web/src/routes/entity-detail.tsx`): lista as chaves que faltam
   (`rotuloItemFaltando` — sinopse, duração, classificação, elenco, arquivo de
   mídia, artes por kind) mas é só texto. O botão genérico "Baixar
   informações faltando" (re-enriquecimento) resolve a maioria dos casos
   (sinopse/elenco/artes vêm do provider), mas NÃO resolve `media_file`
   (arquivo de mídia) — que só se resolve vinculando um datasource/rodando um
   scan, uma tela diferente. O dono vê "arquivo de mídia" faltando sem
   nenhuma pista de que a solução fica em Bibliotecas.
2. **Dois caminhos para adicionar um filme**: `/new/item` (link "Novo item"
   na Biblioteca) abre um formulário em branco — só Título/Sinopse/Título
   original/Data, sem busca nenhuma. `/search` é o fluxo completo, ligado ao
   provider, com pôster e "Importar". Nenhuma das duas telas menciona a
   outra — um dono que clica "Novo item" esperando algo perto do TMDB
   (buscar e importar) cai num formulário manual sem pista de que a busca
   fica em outra aba do menu.

## 2. Jornada do usuário

**Banner de completude:**
1. O dono abre um item com o banner "Catalogação incompleta: arquivo de
   mídia".
2. **Hoje**: só vê o texto "arquivo de mídia" numa lista; a única ação
   visível na tela é "Baixar informações faltando", que não resolve esse
   item específico.
3. **Depois deste briefing**: o item "arquivo de mídia" na lista é um
   link/ação curta ("ver Bibliotecas") que leva direto para onde o problema
   se resolve. Itens que o botão genérico já resolve (sinopse, elenco, artes)
   continuam como estão hoje.

**Novo item vs. Busca:**
1. O dono clica "Novo item" na tela de Biblioteca, esperando poder buscar um
   título existente.
2. **Hoje**: cai direto num formulário em branco, sem nenhuma pista de que
   `/search` é o caminho para importar.
3. **Depois deste briefing**: o topo de `/new/item` traz uma linha curta
   ("Procurando um título existente? Use a Busca para importar direto do
   catálogo.", linkando para `/search`) antes do formulário manual — que
   continua disponível como opção para quem realmente quer cadastrar algo
   que não existe em nenhuma fonte externa.

## 3. Escopo

**Dentro:**
- `SecaoCompletudeIncompleta`/`rotuloItemFaltando`: cada chave de `missing`
  vira uma ação específica quando fizer sentido (pelo menos `media_file` →
  link para `/library`); as chaves já cobertas pelo botão genérico continuam
  como estão (não precisam de ação individual redundante).
- `/new/item` (`ItemNewScreen`/`NewEntityPage`): linha de cross-link para
  `/search` no topo da página, antes do formulário.

**Fora (explicitamente):**
- Unificar `/new/item` e `/search` numa única tela (a auditoria só pediu o
  cross-link, não fundir os dois fluxos).
- Mudar `/new/collection` (`collection-new.tsx`) — não citado pela auditoria;
  se fizer sentido o mesmo cross-link lá, é decisão de implementação, não
  critério de aceite deste briefing.
- Qualquer mudança na lógica de completude em si (0057/0058 — quais campos
  contam, como o job de saúde avalia) — este briefing só muda a
  APRESENTAÇÃO do que já é calculado.

## 4. Contrato técnico

### Frontend (Vite/React)

- `web/src/routes/entity-detail.tsx` (`SecaoCompletudeIncompleta`): em vez de
  `<li>{rotuloItemFaltando(chave)}</li>` puro para TODA chave, mapear
  `media_file` (e outras chaves sem solução via re-enriquecimento, se
  identificadas durante a implementação) para um `<li>` com um `<Link to=
  "/library">` (ou rota equivalente de gestão de datasources) junto ao rótulo
  — ex.: "arquivo de mídia → ver Bibliotecas". As chaves de campo/arte
  (`description`, `runtime`, `certification`, `keywords`, `cast`,
  `artwork:<kind>`) continuam só como texto (o botão genérico já resolve).
- `web/src/routes/item-new.tsx` (`ItemNewScreen`) ou `web/src/components/new-
  entity-page.tsx` (componente compartilhado com `collection-new.tsx`):
  adicionar um bloco de texto curto com `<Link to="/search">` no topo,
  ANTES do formulário — decidir se entra em `NewEntityPage` (compartilhado,
  se fizer sentido também para coleção) ou só em `ItemNewScreen` (a auditoria
  citou especificamente "Novo item", não coleção).

### Arquivos afetados

- `web/src/routes/entity-detail.tsx` (+ `.test.tsx`).
- `web/src/routes/item-new.tsx` (+ `.test.tsx`) e/ou `web/src/components/new-
  entity-page.tsx`.

## 5. Critérios de aceite

- [ ] O banner "Catalogação incompleta" com `media_file` na lista mostra um
      link para a tela de Bibliotecas junto ao rótulo, não só texto.
- [ ] As demais chaves da lista (sinopse, elenco, artes, etc.) continuam
      exibidas como hoje, sem regressão.
- [ ] `/new/item` mostra a linha de cross-link para `/search` no topo, antes
      do formulário, e o link navega corretamente.
- [ ] O formulário manual de `/new/item` continua funcionando exatamente como
      hoje (nenhuma mudança de comportamento, só a adição do link).
- [ ] `make e2e-cov` e `make gate-local` passam.

## 6. Notas de implementação / decisões

- **Por que só `media_file` ganha ação garantida?** É o único item da lista
  hoje que o botão genérico "Baixar informações faltando" comprovadamente NÃO
  resolve (ele re-enriquece campos/artes via provider; vincular um arquivo é
  uma operação de biblioteca, tela diferente). Se a implementação encontrar
  outras chaves na mesma situação, estender o mesmo tratamento é bem-vindo,
  mas não é um critério de aceite fixo — o problema relatado é especificamente
  esse.
