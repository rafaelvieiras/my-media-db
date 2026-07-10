# 0023 — Tela rica de detalhe do item externo (UI)

> A tela que o dono vê **antes de importar**: o item externo por inteiro (pôster,
> fundo, sinopse, gêneros e palavras-chave, duração, classificação, slogan, vídeos,
> coleção, similares, nota e links) — com o botão **Importar**. Consome o detalhe
> rico e persistente do [0022](0022-detalhe-externo-rico-persistente.md).

| | |
|---|---|
| **ID** | 0023 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override (extensão pós-revisão) |
| **Docs relacionadas** | [07-enriquecimento-providers](../docs/07-enriquecimento-providers.md), [08-artwork-e-assets](../docs/08-artwork-e-assets.md), [05-api](../docs/05-api.md) |
| **Status** | ready |
| **Depende de** | [0022](0022-detalhe-externo-rico-persistente.md), [0021](0021-ui-idiomas-admin-seletor.md) |

---

## 1. Objetivo

Dar ao dono a **decisão informada** de importar: em vez de um card raso, uma **tela
de detalhe do item externo** com o conjunto rico que o [0022](0022-detalhe-externo-rico-persistente.md)
serve — o mínimo que o TMDB mostra. É a metade de UI do épico levantado na revisão da
Fase 3; **sem** elenco/equipe (MD6) e **sem** nota local (briefings próprios).

## 2. Jornada do usuário

1. Na busca ([0021](0021-ui-idiomas-admin-seletor.md)/search), o dono clica num
   resultado externo → abre a **tela de detalhe do externo** (rota própria ou painel).
2. Vê o item completo: pôster + **fundo (backdrop)**, título/título original, ano e
   data, **sinopse**, **gêneros** e **palavras-chave**, **duração**, **classificação
   indicativa**, **slogan** (com o destaque em evidência), **idioma original**,
   **nota externa**, **vídeos** (trailer/clip), se faz parte de uma **coleção**, os
   **similares**, e **links externos** (TMDB, IMDb…).
3. Tudo respeita o **idioma ativo** do topo ([0021](0021-ui-idiomas-admin-seletor.md)):
   trocar o idioma reflete os campos traduzíveis.
4. Ele pode **clicar num similar ou na coleção** → a tela abre o detalhe externo
   daquele item (navegação recursiva no externo, sem catalogar). Voltar retorna.
5. Ele clica **Importar** → cria/atualiza a entity (reusa o detalhe já cacheado) e vai
   ao detalhe da entity (o do catálogo).
6. Estados: carregando, **fonte indisponível** (502/503), item inexistente (404).

## 3. Escopo

**Dentro:**
- **Rota/painel de detalhe do externo** consumindo `GET /external/{source}/{type}/{id}`
  do [0022](0022-detalhe-externo-rico-persistente.md), no idioma ativo.
- Layout rico: hero com backdrop+pôster, blocos de sinopse, metadados (duração,
  classificação, nota, idioma original), **gêneros e palavras-chave** (campos
  distintos, [0022](0022-detalhe-externo-rico-persistente.md)), **slogan em destaque**,
  **galeria/lista de vídeos**, faixa de **coleção**, faixa de **similares**, **links
  externos**, botão **Importar**.
- **Similares e coleção NAVEGÁVEIS** (decidido na revisão): clicar num similar ou num
  item da coleção **abre o detalhe externo daquele item** — a mesma rota, que chama
  `GET /external/...` do alvo (persistindo-o na 1ª visita, [0022](0022-detalhe-externo-rico-persistente.md)).
  Navegação recursiva no espaço externo, sem catalogar nada.
- Estados vazio/carregando/erro; reflexo do seletor de idioma.

**Fora (explicitamente):**
- **Elenco/equipe (MD6)** e **nota local** — não aparecem nesta versão.
- **Player** de vídeo embutido e ordenação "locais em destaque" (não há arquivo local
  no externo) — no máximo link/thumb do vídeo externo; o resto é follow-up.

## 4. Contrato técnico

### Frontend (Vite/React)
- `web/src/lib/api.ts`/`queries.ts`: `getExternalDetail(source, type, id, lang)` +
  query options com chave por `(source,id,lang)` (o idioma entra na chave, como o fix
  da busca da revisão). Tipos do `openapi` gerado (schema do detalhe externo do 0022).
- Rota (TanStack Router code-based) **ou** painel de detalhe; link a partir do
  `ExternalResultCard` da busca. Reusa `posterSrc`/proxy de imagem para pôster e
  backdrop; `EmptyState`/`ErrorState`/`LoadingState`.
- Botão **Importar** reusa `importExternal` ([0015](0015-enriquecimento-import-proxy-imagem.md));
  no sucesso navega ao `/entities/{id}` e invalida `['entities']`.
- Respeita `useLanguage()` — trocar o idioma refaz a query (campos traduzíveis).

### Arquivos afetados
- Criar: componente/rota de detalhe do externo + testes; helpers em `api.ts`/`queries.ts`.
  Alterar: `ExternalResultCard` (link para a tela), `router.tsx` (se rota), `AppHeader`
  não muda.

## 5. Critérios de aceite

- [ ] A tela exibe o conjunto rico do [0022](0022-detalhe-externo-rico-persistente.md)
  (pôster, backdrop, sinopse, gêneros, keywords, duração, classificação, slogan+destaque,
  nota, idioma original, vídeos, coleção, similares, links) — **sem** créditos/nota local.
- [ ] Trocar o **idioma ativo** reflete os campos traduzíveis (título, sinopse, slogan,
  keywords) — testado.
- [ ] **Similares e coleção navegáveis**: clicar abre o detalhe externo do alvo (mesma
  rota, `GET /external/...`), sem criar entity; voltar retorna — testado.
- [ ] **Importar** cria/atualiza a entity e leva ao detalhe do catálogo; a 2ª ida à
  fonte não acontece (cache do 0022).
- [ ] Estados vazio/carregando/erro (502/503/404) tratados. Catraca verde (type-check
  web + cobertura).

## 6. Notas de implementação / decisões

- **Reusa o proxy/cache de imagem** ([0015](0015-enriquecimento-import-proxy-imagem.md)/
  [08](../docs/08-artwork-e-assets.md)) para pôster e backdrop do externo, sem baixar
  no cliente direto da fonte.
- **Idioma na chave da query**: mesma lição do fix da busca (a mesma tela em idiomas
  diferentes é conteúdo diferente — não pode compartilhar cache do TanStack Query).
- **Navegação recursiva barata**: clicar num similar/coleção é só um `GET /external/...`
  do alvo — que o [0022](0022-detalhe-externo-rico-persistente.md) persiste na 1ª
  visita. Navegar não cataloga; a chave de query por `(source,id,lang)` cacheia cada
  item visitado no cliente também. Cuidar de loops visuais (A→similar B→similar A):
  é navegação normal de rota (voltar funciona), sem estado a manter.
- **Vídeos**: nesta versão, lista/thumb com link para o provedor (YT/TMDB). A regra
  do dono "vídeos locais em destaque, depois externos" vale para o **detalhe da entity
  importada** (que tem arquivos) — follow-up.

## 7. Fora deste briefing / follow-ups

- Tela/seção de **elenco e equipe** (MD6) quando o modelo existir.
- **Nota/avaliação local** do dono na tela.
- **Coleção como página própria** (a coleção inteira, não só o pulo item→item) e
  similares com curadoria/paginação; **player** de vídeo embutido e a ordenação
  "locais em destaque" no detalhe da entity importada.
