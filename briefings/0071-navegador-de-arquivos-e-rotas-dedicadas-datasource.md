# 0071 — Navegador de arquivos (estilo Jellyfin) + rotas dedicadas para datasource

> Trocar o "digitar o caminho de cor" por um navegador de pastas simples, e mover
> criação/edição de datasource de formulário/estado inline para `/library/new` e
> `/library/:id/edit`.

| | |
|---|---|
| **ID** | 0071 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 2 — UI de biblioteca |
| **Status** | done |
| **Docs relacionadas** | [03-arquitetura](../docs/03-arquitetura.md), [04-modelo-de-dados](../docs/04-modelo-de-dados.md), [06-biblioteca-e-scan](../docs/06-biblioteca-e-scan.md) |
| **Depende de** | 0008, 0064, 0070 |

---

## 1. Objetivo

Uma auditoria de UX (navegação real pela rota `/library`, não só leitura de
código) apontou que o maior ponto de fricção da tela é que o campo "Caminho do
datasource" é um `Input` de texto livre puro: o dono precisa saber de cor o
caminho absoluto **como visto pelo processo da API** (não pelo seu desktop —
em produção a API roda dentro de um container, com seus próprios mounts).
Digitar errado só é descoberto depois de submeter, via 422.

A mesma auditoria confirmou que faz sentido mover **criação** e **edição de
caminho** de datasource — hoje um formulário sempre-visível no topo da lista e
um formulário inline dentro da linha (`useState` local) — para rotas
dedicadas, no mesmo padrão já adotado em `/entities/$id/edit` e `/new/item`
(briefing 0030). Isso dá o espaço de tela necessário para o navegador de
pastas, que não cabe bem espremido numa linha de lista.

Este briefing entrega os dois juntos: as rotas dedicadas são o que dá espaço
para o navegador; o navegador é o que torna a rota dedicada necessária (um
campo de texto sozinho não justificaria a mudança de rota).

## 2. Jornada do usuário

**Criar:**
1. Na tela `/library`, o dono clica em "Adicionar datasource" (antes: formulário
   sempre visível no topo; agora um botão/link).
2. Navega para `/library/new`: campo de caminho (texto livre, como hoje) + botão
   "Procurar pasta…" que expande, na mesma página, um navegador simples —
   breadcrumb do caminho atual, lista de subpastas (só pastas, não arquivos),
   um jeito de subir um nível, e um botão "Usar esta pasta" em cada entrada e/ou
   no cabeçalho para a pasta atual.
3. Ao navegar, o campo de texto acompanha a pasta selecionada; o dono também
   pode ignorar o navegador e digitar direto (fallback sempre disponível — para
   mounts exóticos que o navegador não ajuda a encontrar mais rápido).
4. Escolhe o tipo (Filmes/Séries) e confirma. Sucesso volta para `/library`;
   erro (path inválido/duplicado) aparece inline, na própria página.

**Editar caminho:**
1. Em cada linha da lista, o dono clica em "Editar" (antes: ícone de lápis que
   abria edição inline; agora um link).
2. Navega para `/library/$id/edit`: mesmo navegador de pastas, pré-carregado no
   caminho atual do datasource. Salvar aplica o PATCH já existente (0070,
   recasco em cascata); erro aparece inline na página.
3. Nesta mesma página, um bloco separado ("zona de perigo") concentra a ação de
   **remover o datasource** — hoje um ícone de lixeira direto na lista, sem
   nenhuma confirmação, ao lado do botão "Escanear" (risco de clique acidental
   confirmado na auditoria). Remover passa a exigir confirmação explícita em 2
   passos, no mesmo padrão já usado na purga de órfãs (`alertdialog`).
4. "Escanear" e o switch "Observar mudanças nesta pasta" **continuam como
   estão hoje**, inline em cada linha da lista — são ações transientes/binárias
   de baixo risco, não fazem sentido como navegação de rota.

## 3. Escopo

**Dentro:**
- Novo endpoint de navegação de diretórios no backend (só leitura, sem
  side-effect), usado pelas duas rotas novas.
- Duas rotas novas no frontend: `/library/new` e `/library/$id/edit`,
  substituindo o formulário inline (`DatasourceForm` no topo da lista) e a
  edição inline de caminho (`Pencil` dentro de `DatasourceRow`, briefing 0070).
- Componente de navegador de pastas compartilhado pelas duas rotas.
- Mover a remoção de datasource (hoje ícone de lixeira sem confirmação na
  lista) para dentro de `/library/$id/edit`, com confirmação explícita em 2
  passos.
- `DatasourceList`/`datasource-list.tsx` simplifica: cada linha mostra path,
  badge de estado, botões "Escanear" e "Editar" (link) e o switch de watch;
  a lista ganha um botão/link "Adicionar datasource" no topo (sem formulário
  fixo ocupando espaço).
- Atualizar `e2e/tests/library.spec.ts` para o novo fluxo (os testes
  `'adicionar e remover datasource'` e `'editar caminho de datasource
  cadastrado recasca sem novo scan'` navegam por rota em vez de interagir com
  formulário/estado inline).

**Fora (explicitamente):**
- Corrigir o badge de estado (online/offline) ficar dessincronizado do
  resultado de um scan que falha (achado da auditoria, é bug pré-existente
  independente deste briefing — merece um fix à parte).
- Exibir `last_scan` na lista (o campo já existe na API, só não é lido pelo
  front) — feature pequena à parte, não pedida aqui.
- Permitir editar `type`/`content_type` de uma fonte já cadastrada — mesma
  exclusão já registrada no 0070, continua fora.
- Preview/thumbnail de conteúdo das pastas, seleção múltipla, criar pasta nova
  pela UI — o navegador é só "escolher uma pasta existente", não um explorador
  de arquivos completo.
- Pontos de partida sugeridos/favoritos (ex.: sugerir a raiz a partir de outros
  datasources já cadastrados) — fica como follow-up; a raiz inicial do
  navegador é sempre `/`.
- Corrigir a navbar não colapsar em mobile — achado da auditoria, mas é
  problema de nível de aplicação (layout global), não desta tela.

## 4. Contrato técnico

### Dados / modelo

Nenhuma migration — o endpoint novo só lê o filesystem, não persiste nada.

### API

Novo endpoint, dentro do grupo `/library` já existente
(`internal/server/server.go`, `v1.Route("/library", ...)`):

```
GET /api/v1/library/browse?path={absoluto opcional}
```

- `path` ausente ou vazio → usa `/` como raiz.
- Resposta (200):
  ```json
  {
    "path": "/mnt/midia",
    "parent": "/mnt",
    "entries": [
      { "name": "filmes", "path": "/mnt/midia/filmes", "readable": true },
      { "name": "series", "path": "/mnt/midia/series", "readable": false }
    ],
    "truncated": false
  }
  ```
  - `parent`: `null` quando `path` já é a raiz do filesystem (`/`).
  - `entries`: só diretórios (arquivos soltos são omitidos), ordenados por
    `name` alfabeticamente (case-insensitive).
  - `readable: false` quando a PRÓPRIA entrada não pode ser aberta para
    listagem (permissão negada) — aparece na lista (o dono vê que a pasta
    existe) mas não é navegável; o front desabilita o clique nela.
  - Cap de segurança: no máximo **2000 entradas** na resposta (evita travar o
    front numa pasta patológica, ex. milhares de arquivos numa pasta plana
    montada por engano como raiz); se o diretório tiver mais, `truncated:
    true` e a lista vem cortada nas primeiras 2000 (ordem alfabética) — front
    mostra um aviso textual simples quando `truncated`.
- Erros (reaproveita `mapLibraryError`, mesmos códigos de `Criar`/0070):
  - `422 invalid_path` — `path` não existe ou não é diretório.
  - `422 path_unreadable` — o próprio `path` pedido não pode ser aberto para
    listagem (permissão negada) — aqui SIM é erro (diferente de uma entrada
    filha ilegível, que só marca `readable:false`).

`openapi.yaml`: novo path `/library/browse`, schema `BrowseResult` com os
campos acima.

### Backend (Go)

- `internal/library/browse.go` (novo arquivo): `func (s *Service) Browse(ctx
  context.Context, rawPath string) (BrowseResult, error)`:
  1. `rawPath == ""` → usa `"/"`.
  2. Normaliza com `normalizarPath` (reaproveitado).
  3. Valida com `validarPath` (reaproveitado) — mesmos erros de domínio
     (`ErrPathInexistente`, `ErrNaoDiretorio`, `ErrPathIlegivel`).
  4. `os.ReadDir(abs)` — para cada entrada, segue symlink via `os.Stat` (não
     `os.Lstat`) para decidir se é diretório (mounts via symlink são comuns);
     ignora silenciosamente entradas que não são diretório (incluindo
     symlink quebrado).
  5. Para cada subdiretório, tenta `os.Open` + `ReadDir(1)` (mesma checagem de
     `validarPath`) só para decidir `readable`; nunca aborta a listagem
     inteira por causa de uma entrada sem permissão.
  6. Ordena por `strings.ToLower(name)`; corta em 2000 com `truncated: true`
     se exceder.
  7. `parent`: `filepath.Dir(abs)` — `nil`/omitido quando `parent == abs`
     (chegou na raiz).
- `internal/server/library.go`: novo handler `browseLibraryPath(w, r)` — lê
  `r.URL.Query().Get("path")`, chama `s.library.Browse`, mapeia erro com
  `mapLibraryError` (mesmo helper já usado pelos outros handlers de library).
- Rota: `lib.Get("/browse", s.browseLibraryPath)` em
  `v1.Route("/library", ...)` (`internal/server/server.go`).

### Frontend (Vite/React)

- `web/src/router.tsx`: duas rotas novas, registradas como as demais
  (code-based, mesmo padrão de `entityEditRoute`/`itemNewRoute`):
  - `libraryNewRoute`: `path: '/library/new'`, componente
    `LibraryNewScreen` (`web/src/routes/library-new.tsx`).
  - `libraryEditRoute`: `path: '/library/$id/edit'`, componente
    `LibraryEditScreen` (`web/src/routes/library-edit.tsx`).
- Novo componente compartilhado `web/src/components/path-browser.tsx`:
  - Props: `value: string` (path atual do campo texto), `onChange(path:
    string)`.
  - Estado interno: pasta sendo navegada (inicia em `/`, ou em `value` se já
    for um path absoluto válido ao montar — reaproveita a última pasta
    navegada, não força voltar pra raiz toda hora).
  - `useQuery` para `GET /library/browse?path=...` a cada navegação.
  - Renderiza: breadcrumb clicável (cada segmento do `path` navega direto pra
    lá), lista de `entries` (nome + ícone de pasta; entradas `readable:false`
    aparecem acinzentadas/desabilitadas com título "sem permissão de
    leitura"), botão "Usar esta pasta" que chama `onChange(path atual)`.
  - O `Input` de texto (já existente) continua editável em paralelo — digitar
    e confirmar (blur/Enter) também dispara navegação do browser para aquele
    valor, se for um path válido; se não for, o browser mantém a última pasta
    válida e o texto livre já digitado NÃO é sobrescrito (fallback continua
    funcionando mesmo se o navegador não conseguir listar aquele caminho).
- `web/src/routes/library-new.tsx`: substitui o conteúdo de
  `DatasourceForm` (que é removido/absorvido aqui) — campo de caminho +
  `PathBrowser` + Select de tipo + submit (`createLibrary`); sucesso navega de
  volta para `/library`.
- `web/src/routes/library-edit.tsx`: carrega a fonte via `libraryQueryOptions`
  + `useParams({ from: '/library/$id/edit' })`; campo de caminho pré-preenchido
  com `source.path` + `PathBrowser` + submit (`patchLibrary(id, { path })`,
  já existente do 0070); seção "zona de perigo" com botão "Remover
  datasource" → `alertdialog` de confirmação (mesmo padrão do botão de purga
  em `orphans`) → `deleteLibrary(id)` → volta para `/library`.
- `web/src/components/datasource-list.tsx`: remove o `DatasourceForm` inline
  e todo o estado/formulário de edição de path (`editandoPath`, `novoPath`,
  `pathMutation`) de dentro de `DatasourceRow`; adiciona `Link to="/library/
  new"` (botão "Adicionar datasource") acima da lista e `Link to="/library/
  $id/edit" params={{ id }}` (botão "Editar") em cada linha; remove o botão de
  lixeira e o `deleteMutation` da linha (migram para `library-edit.tsx`).
- `web/src/lib/api.ts` / `api-types.ts`: gerar tipos para o novo endpoint
  (`npm run gen:types` a partir do `openapi.yaml` atualizado) + função
  `browseLibraryPath(path?: string)`.

### Arquivos afetados

- `internal/library/browse.go` (novo) + `browse_test.go`.
- `internal/server/library.go` (+ `library_test.go`).
- `openapi.yaml` (+ `web/src/lib/api-types.ts` gerado).
- `web/src/router.tsx`.
- `web/src/routes/library-new.tsx` (novo, + `.test.tsx`).
- `web/src/routes/library-edit.tsx` (novo, + `.test.tsx`).
- `web/src/components/path-browser.tsx` (novo, + `.test.tsx`).
- `web/src/components/datasource-list.tsx` (+ `.test.tsx`) — simplifica.
- `web/src/components/datasource-form.tsx` — removido (absorvido por
  `library-new.tsx`); apagar `datasource-form.test.tsx` junto.
- `e2e/tests/library.spec.ts` — atualizar os testes existentes para o novo
  fluxo por rota.

## 5. Critérios de aceite

- [x] `GET /api/v1/library/browse` sem `path` lista o conteúdo de `/`, só
      diretórios, ordenados alfabeticamente.
- [x] `GET /api/v1/library/browse?path=...` com um caminho válido lista as
      subpastas dele; `parent` aponta um nível acima; entradas sem permissão
      de leitura vêm com `readable:false` sem quebrar o restante da lista.
- [x] `path` inexistente/não-diretório → 422; `path` existente mas sem
      permissão de leitura no próprio nível pedido → 422 `path_unreadable`.
- [x] Diretório com mais de 2000 subpastas → resposta cortada com
      `truncated: true`.
- [x] `/library/new` renderiza o navegador, permite escolher uma pasta
      clicando (preenche o campo texto) OU digitar direto; cria o datasource
      e volta para `/library`.
- [x] `/library/$id/edit` pré-carrega o caminho atual no navegador/campo,
      salva via PATCH (recasco do 0070 continua funcionando) e volta para
      `/library`.
- [x] Remover datasource só é possível a partir de `/library/$id/edit`, com
      confirmação explícita em 2 passos (`alertdialog`); não existe mais
      remoção com 1 clique direto na lista.
- [x] `/library` não tem mais formulário fixo no topo nem edição inline por
      linha — só lista + botões "Adicionar datasource" e "Editar" (link) por
      item, com "Escanear" e o switch de watch como já eram.
- [x] `e2e/tests/library.spec.ts` (`adicionar e remover datasource`, `editar
      caminho de datasource cadastrado recasca sem novo scan`) passam com o
      novo fluxo por rota; `make e2e-cov` e `make gate-local` passam.

## 6. Notas de implementação / decisões

- **Por que seguir symlink (`os.Stat`) e não `os.Lstat` na listagem?** Mounts
  via symlink (ex.: um link apontando para outro disco/rede) são comuns em
  setups caseiros; tratar o symlink como "não é pasta" (comportamento do
  `Lstat`) esconderia exatamente os atalhos que mais ajudam a navegar rápido.
- **Por que não restringir o navegador a uma allowlist de diretórios?** O
  campo de texto livre já permite hoje apontar para QUALQUER caminho absoluto
  que o processo da API enxergue — o navegador só torna visível o que já era
  possível digitar. Não é uma superfície de acesso nova, é a mesma de sempre,
  só com descoberta visual em vez de "adivinhar de cor".
- **Por que mover "Remover" para a rota de edição em vez de só adicionar um
  popover de confirmação na lista?** A auditoria de UX encontrou o botão de
  lixeira colado (mesmo grupo flex, `gap-2`) ao botão "Escanear", sem nenhuma
  confirmação — clique errado é fácil. Como a rota de edição já está sendo
  criada por este briefing (para caber o navegador de pastas), isolar a
  remoção lá dentro, numa seção visualmente separada ("zona de perigo"),
  resolve o risco de proximidade sem nenhum custo extra de escopo.
- **Reset do Select "Tipo" ao criar (achado #6 da auditoria) e posição da
  mensagem de erro (achado #7) somem de graça**: como `/library/new` é uma
  rota que monta um componente novo a cada navegação, o estado local do
  formulário (incluindo o `type` selecionado) não sobrevive entre uma criação
  e outra — o problema de "esquecer de trocar de volta pra Filmes" deixa de
  existir sem trabalho extra. Mesma coisa para o erro renderizar longe do
  campo: numa página cheia (não mais uma linha de lista espremida), o erro já
  nasce logo abaixo do campo por construção.
- O endpoint `/library/browse` é só leitura (`GET`) e não precisa de conexão
  de escrita — usa o mesmo padrão de leitura pelo pool já usado por
  `listLibrary`/`getLibrary`.

## 7. Fora deste briefing / follow-ups

- Corrigir o badge de estado (online/offline) dessincronizado quando um scan
  falha por pasta ausente (a lista não invalida `['library']` no `onError`/
  `onSettled` do scan) — bug pré-existente, fix à parte.
- Mostrar `last_scan` formatado na lista (campo já existe na API).
- Pontos de partida sugeridos no navegador (ex.: raiz comum dos datasources já
  cadastrados) — nice-to-have, não pedido agora.
- Editar `type`/`content_type` de uma fonte já cadastrada.
- Navbar responsiva em mobile (achado de UX de nível de aplicação, tela
  separada).
