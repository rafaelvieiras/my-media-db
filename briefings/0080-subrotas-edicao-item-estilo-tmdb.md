# 0080 — Subrotas na edição de item, com sidebar no padrão do TMDB

> A edição de um item hoje é UMA página gigante com os 3 blocos empilhados
> (informações básicas, IDs externos, elenco/equipe) — o dono quer o mesmo
> padrão do editor do TMDB: uma barra lateral com seções, cada uma numa
> subrota própria, mostrando só o bloco daquela seção.

| | |
|---|---|
| **ID** | 0080 |
| **Tipo** | feature (refactor de rota) |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | done |
| **Docs relacionadas** | [03-arquitetura](../docs/03-arquitetura.md) |
| **Depende de** | 0030, 0073 |

---

## 1. Objetivo

O dono anexou uma captura do editor do TMDB: barra lateral à esquerda com uma
lista de seções (Informações básicas, Nomes alternativos, Classificações
indicativas, Equipe técnica, Grupos de episódios, IDs de terceiros, Gêneros,
Palavras-chave, Informações de produção, Elenco recorrente, Temporadas,
Slogans, Vídeos), cabeçalho com pôster+título+seletor de idioma, e a área
principal mostrando só o formulário da seção selecionada.

O my-media-db **não tem** todas essas seções como conceitos editáveis — a
fonte única de campos editoriais (`internal/resolver/resolver.go`,
`registroOrigem`) só permite editar `title`, `original_title`, `description`,
`release_date`, `tags`, `local_rating` (todos via `SchemaForm`), além de
`external_ids` (`ExternalIdsEditor`) e `credits` — cast/crew/companies —
(`CreditsEditor`). Campos como `tagline`, `keywords`, `airing_status` são
`soLeitura: true` no registro (só a fonte externa escreve, não o dono);
"Nomes alternativos", "Classificações indicativas", "Grupos de episódios",
"Elenco recorrente", "Temporadas", "Slogans", "Vídeos" simplesmente não
existem como dado editável no schema atual — replicar essas seções vazias
seria fachada sem função por trás.

Este briefing adapta o PADRÃO VISUAL (sidebar + subrotas) às TRÊS seções que o
produto realmente tem hoje: **Informações básicas** (`SchemaForm`), **IDs de
terceiros** (`ExternalIdsEditor`), **Elenco e equipe** (`CreditsEditor`) — no
lugar da página única e longa (`web/src/routes/entity-edit.tsx`,
`EntityEditForm`, que hoje empilha os 3 componentes um embaixo do outro).

## 2. Jornada do usuário

1. O dono clica "Editar" num item.
2. **Hoje**: cai em `/entities/$id/edit`, uma página só, com os 3 blocos
   (informações, IDs, créditos) empilhados — scroll longo, sobretudo com
   elenco grande.
3. **Depois deste briefing**: cai em `/entities/$id/edit` (que passa a ser a
   subrota "Informações básicas" por padrão), com uma barra lateral à
   esquerda mostrando as 3 seções disponíveis (Informações básicas / IDs de
   terceiros / Elenco e equipe), um cabeçalho com pôster+título do item e o
   seletor de idioma (já existe como componente, `LanguageSelector`), e a
   área principal mostrando SÓ o formulário da seção ativa.
4. Clicar em "IDs de terceiros" navega para `/entities/$id/edit/ids` (URL
   própria, deep-linkável), mostrando só o `ExternalIdsEditor`. Clicar em
   "Elenco e equipe" navega para `/entities/$id/edit/creditos`, mostrando só
   o `CreditsEditor`. A sidebar e o cabeçalho continuam visíveis nas três
   subrotas (chrome compartilhado).
5. Editar uma seção sem salvar e clicar em OUTRA seção da sidebar (ou em
   "Voltar ao item") dispara a MESMA confirmação de "sair sem salvar" do
   [0073](0073-guard-sair-sem-salvar-edicao-item.md) — agora por seção
   (já que cada subrota só tem UM bloco, o guard fica mais simples: não
   precisa mais combinar 3 booleanos, só o da seção atual).

## 3. Escopo

**Dentro:**
- 3 rotas novas (substituindo a única `/entities/$id/edit` de hoje):
  `/entities/$id/edit` (Informações básicas, default), `/entities/$id/edit/ids`
  (IDs de terceiros), `/entities/$id/edit/creditos` (Elenco e equipe).
- Um "chrome" compartilhado pelas 3 (sidebar de navegação entre seções +
  cabeçalho com pôster/título/idioma + "Voltar ao item"), extraído para um
  componente reusado pelas 3 telas — NÃO precisa ser uma rota de layout
  aninhada do TanStack Router (o projeto usa rotas FLAT, `getParentRoute: () =>
  rootRoute`, ver `web/src/router.tsx` — seguir o MESMO padrão das rotas
  existentes, ex. como `library-edit`/`library-new` compartilham
  `LibraryFormHeader`).
- Guard de "sair sem salvar" (0073) adaptado: cada subrota só tem 1 bloco,
  então o guard passa a checar o "sujo" DAQUELE bloco só; navegar para OUTRA
  seção da sidebar com alterações pendentes dispara a mesma confirmação que
  hoje só dispara ao clicar "Voltar ao item"/fechar aba.
- A sidebar indica visualmente qual seção está ativa (rota atual).

**Fora (explicitamente):**
- Qualquer seção do TMDB sem dado editável correspondente no schema atual
  (Nomes alternativos, Classificações indicativas, Grupos de episódios,
  Elenco recorrente, Temporadas, Slogans, Vídeos, Gêneros, Palavras-chave,
  Equipe técnica como seção PRÓPRIA — equipe já mora dentro de "Elenco e
  equipe"/`CreditsEditor`, não vira seção separada). Se o dono quiser tornar
  algum desses campos editável no futuro, é um briefing de SCHEMA/BACKEND
  separado (mudar `registroOrigem`), não deste briefing de UI.
- Um "score de completude" numérico na sidebar (mesma ressalva do
  [0079](0079-hero-compacto-estilo-tmdb.md) — precisaria de métrica nova no
  backend).
- Mudar o CONTRATO de salvamento de qualquer bloco (`patchOverride`,
  `updateExternalIds`, `PATCH` de créditos) — só a ORGANIZAÇÃO de rota muda,
  os endpoints e payloads continuam os mesmos.
- Mudar o conteúdo/campos de `SchemaForm`, `ExternalIdsEditor`,
  `CreditsEditor` em si (comportamento interno de cada um inalterado — só
  onde cada um é montado na árvore de rotas muda).

## 4. Contrato técnico

### Frontend (Vite/React)

- `web/src/router.tsx`: trocar a única `entityEditRoute` (`path:
  '/entities/$id/edit'`) por 3 rotas flat com o MESMO padrão das existentes:
  - `entityEditBasicoRoute` — `path: '/entities/$id/edit'` (mantém a URL
    atual como default/"Informações básicas", preservando deep-links já
    salvos/documentados).
  - `entityEditIdsRoute` — `path: '/entities/$id/edit/ids'`.
  - `entityEditCreditosRoute` — `path: '/entities/$id/edit/creditos'`.
  - Todas com `getParentRoute: () => rootRoute`, registradas em `routeTree`.
- Novo componente de chrome compartilhado (sugestão de nome:
  `EntityEditShell`, em `web/src/components/entity-edit-shell.tsx`):
  - Recebe `id`, a `entity` resolvida (para pôster/título do cabeçalho),
    a seção ativa (para destacar o item correspondente na sidebar), e
    `children` (o conteúdo da seção atual).
  - Cabeçalho: miniatura do pôster (`PosterImg`, já existe e é exportado de
    `rich-detail-view.tsx`) + título do item + `LanguageSelector` (já existe,
    `web/src/components/language-selector.tsx`) + link "Voltar ao item" (MESMO
    comportamento de guard do 0073, adaptado — ver abaixo).
  - Sidebar: 3 links (`<Link>` do TanStack Router) para as 3 subrotas,
    destacando a ativa (`useMatchRoute` ou comparação de `pathname`).
  - Cada link da sidebar (e o "Voltar ao item") intercepta o clique da MESMA
    forma que o 0073 já implementou para "Voltar ao item" — se a seção ATUAL
    estiver suja, confirma (`window.confirm`) antes de navegar.
- `web/src/routes/entity-edit.tsx` (renomear/reorganizar conforme fizer
  sentido — pode virar 3 arquivos, ex. `entity-edit-basico.tsx`,
  `entity-edit-ids.tsx`, `entity-edit-creditos.tsx`, ou 1 arquivo com 3
  componentes exportados; decisão de implementação):
  - Cada tela carrega os MESMOS dados que hoje (`entityQueryOptions`,
    `overrideBlobQueryOptions`, `entityFieldsQueryOptions` só onde for
    necessário — a subrota de IDs/Créditos não precisa de `fieldsQuery`, só
    a de Informações básicas precisa) e renderiza SÓ o bloco correspondente
    dentro do `EntityEditShell`.
  - O guard de "sair sem salvar" (0073) fica LOCAL a cada subrota: só o
    `sujo` do bloco daquela tela, reportado via `onDirtyChange` (prop que já
    existe nos 3 componentes desde o 0073) — não precisa mais combinar 3
    booleanos num estado pai, já que cada subrota só monta 1 bloco.
  - Ao salvar com sucesso em QUALQUER subrota, o comportamento de navegação
    pode continuar voltando para `/entities/$id` (como hoje) OU permanecer
    na mesma subrota (decisão de implementação — o critério de aceite é
    "salvar funciona e o dado persiste", não uma navegação pós-salvar
    específica).

### Arquivos afetados

- `web/src/router.tsx`.
- `web/src/routes/entity-edit.tsx` (reorganizado, possivelmente dividido).
- `web/src/components/entity-edit-shell.tsx` (novo, + `.test.tsx`).
- Links para `/entities/$id/edit` existentes no app (ex. `entity-action-
  buttons.tsx`, que hoje linka para essa rota) — conferir que continuam
  válidos (a rota default não muda de path).

## 5. Critérios de aceite

- [ ] `/entities/$id/edit` mostra a sidebar com as 3 seções e o formulário de
      "Informações básicas" (`SchemaForm`) — comportamento de salvar/limpar
      idêntico ao de hoje.
- [ ] `/entities/$id/edit/ids` mostra só o `ExternalIdsEditor`, com a sidebar
      e cabeçalho, comportamento de salvar/re-enriquecer idêntico ao de hoje.
- [ ] `/entities/$id/edit/creditos` mostra só o `CreditsEditor`, com a
      sidebar e cabeçalho, comportamento de salvar créditos (incluindo a
      reordenação em lote do 0076) idêntico ao de hoje.
- [ ] Editar uma seção sem salvar e clicar em outra seção da sidebar dispara
      a confirmação de "sair sem salvar"; cancelando, permanece na seção
      atual com a alteração intacta.
- [ ] Editar uma seção sem salvar e clicar "Voltar ao item" (ou fechar
      aba/recarregar) continua dispando a mesma confirmação/aviso nativo do
      0073.
- [ ] O link "Editar" existente no app (`entity-action-buttons.tsx` e
      qualquer outro ponto que navegue para `/entities/$id/edit`) continua
      funcionando sem alteração (a URL default não muda).
- [ ] `make e2e-cov` e `make gate-local` passam.

## 6. Notas de implementação / decisões

- **Por que só 3 seções, não as ~13 do TMDB?** O TMDB edita um catálogo
  colaborativo com muito mais metadado editorial do que o my-media-db
  modela hoje. Replicar seções sem dado editável por trás (ex. "Slogans",
  "Vídeos", "Temporadas" como formulário) seria UI decorativa sem função —
  contra o princípio do projeto de não construir fachada sem lógica real
  (ver `AGENTS.md`). As 3 seções aqui são exatamente os 3 blocos que já
  existem hoje, só reorganizados em rotas — nenhum campo novo fica editável
  por este briefing.
- **Por que rotas FLAT, não uma rota de layout aninhada?** O projeto já usa
  esse padrão consistentemente (`web/src/router.tsx`, todas as rotas com
  `getParentRoute: () => rootRoute`) — seguir o mesmo estilo evita introduzir
  um segundo padrão de roteamento só para esta tela.
- Recomenda-se implementar DEPOIS que qualquer trabalho pendente em
  `entity-edit.tsx`/`credits-editor.tsx` (ex. 0076, já concluído) estiver
  mesclado, para reduzir risco de conflito — na prática, no momento em que
  este briefing foi escrito, 0072-0078 já estão todos `done`, então não há
  mais briefing concorrente tocando esses arquivos.
