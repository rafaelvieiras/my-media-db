# 0011 — Frontend: grid de posters, lista e detalhe com mediainfo

> A UI que fecha o loop da Fase 2: gerenciar datasources, disparar scan, ver o
> catálogo detectado num grid de posters e abrir o detalhe com os fatos técnicos.
> É a validação visual do rumo.

| | |
|---|---|
| **ID** | 0011 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 2 — UI de biblioteca |
| **Docs relacionadas** | [03-arquitetura](../docs/03-arquitetura.md), [05-api](../docs/05-api.md), [ADR-0008](../docs/adr/0008-identidade-e-matching-kodi-jellyfin.md) |
| **Status** | done |
| **Depende de** | [0008](0008-datasources-cadastro-api-library.md), [0009](0009-scanner-fswalk-ffprobe-matching.md), [0010](0010-api-leitura-resolver-minimo.md) |

---

## 1. Objetivo

Dar **cara** à Fase 2. Até aqui tudo é API; este briefing entrega as telas que o
dono usa para **apontar pastas, escanear e ver a mídia detectada com seus fatos
técnicos** — o objetivo declarado da fase ("validar o rumo"). Consome os contratos
dos [0008](0008-datasources-cadastro-api-library.md)/[0010](0010-api-leitura-resolver-minimo.md)
via os **tipos TS gerados** do OpenAPI, e reusa o scaffold do [0002](0002-frontend-scaffold-integracao-api.md)
(TanStack Router/Query, shadcn/ui).

## 2. Jornada do usuário

1. Abre **Bibliotecas**: lista de datasources; "Adicionar" pede caminho + tipo; erro
   de path inválido aparece inline; o novo datasource entra na lista.
2. Clica **Escanear**; um indicador de **progresso** mostra o job correndo (arquivos
   vistos), estado online/offline, e conclui sozinho.
3. Vai para a **Biblioteca** (grid): posters dos itens detectados; sem arte → placeholder;
   **badges** de posse ("na biblioteca") e de **revisão** (match de baixa confiança).
   Estado vazio orienta ("nenhum item — adicione um datasource e escaneie").
4. Filtra por tipo/posse; pagina.
5. Abre um item: **tela de detalhe** com título, tipo, status, e a lista de **arquivos**
   com seus **fatos técnicos** (resolução, codecs, HDR, faixas de áudio/legenda,
   duração, edição/parte). Itens não-identificados aparecem como `draft`.

## 3. Escopo

**Dentro:**
- **Rotas** novas (TanStack Router, code-based): `/library` (datasources + scan),
  `/` ou `/library/items` (grid), `/entities/$id` (detalhe). Ajustar a home do
  [0002](0002-frontend-scaffold-integracao-api.md).
- **Tela de datasources:** listar, adicionar (form com validação e erro da API),
  remover, disparar scan e **acompanhar progresso** (polling do status do [0009](0009-scanner-fswalk-ffprobe-matching.md)).
- **Grid de posters:** cards com poster (via endpoint do [0010](0010-api-leitura-resolver-minimo.md)),
  título, badges (posse, revisão), fallback de placeholder; filtros (tipo/posse),
  paginação; estados de loading/vazio/erro.
- **Tela de detalhe:** cabeçalho (título/tipo/status) + **arquivos com mediainfo**
  formatado de forma legível (ex.: `1080p · HEVC · HDR10 · 5.1`), edição/parte, estado.
- **Tipos gerados** do OpenAPI ([0010](0010-api-leitura-resolver-minimo.md)) em vez de
  tipos à mão; TanStack Query para fetch/cache.
- **Testes vitest** (a catraca de cobertura do [0005](0005-quality-gate-catraca-unificada.md)
  já vigora): componentes de card/badges, estados do grid (loading/vazio/erro),
  formatação de mediainfo, fluxo de adicionar datasource (mock de API).

**Fora (explicitamente):**
- **Edição de override**, upload de arte custom, busca — Fase 3.
- Ações sobre a **fila de revisão/órfãs** (só exibir o badge aqui; resolver é Fase 3).
- i18n, temas, responsividade fina além do razoável do MVP.
- Realtime/websocket para progresso — **polling** basta (§6).

## 4. Contrato técnico

### Frontend (Vite/React)
- Componentes: `DatasourceList`/`DatasourceForm`, `PosterGrid`/`PosterCard`,
  `EntityDetail`/`MediaInfoTable`, badges de posse/revisão, estados vazios.
- Estado servidor via **TanStack Query** (chaves por rota; invalidar após criar
  datasource / concluir scan). Progresso de scan por **polling** do status enquanto
  `running`.
- **Sem tipos à mão:** importar os tipos gerados do [0010](0010-api-leitura-resolver-minimo.md);
  base de API relativa (`VITE_API_BASE ?? ''`, [0002](0002-frontend-scaffold-integracao-api.md)).
- Formatação de mediainfo em util testável (entrada = `mediainfo_json`, saída = rótulos).

### API consumida
- `/api/v1/library` (CRUD + scan/status) e `/api/v1/entities` (lista/detalhe/files/poster)
  dos [0008](0008-datasources-cadastro-api-library.md)/[0009](0009-scanner-fswalk-ffprobe-matching.md)/[0010](0010-api-leitura-resolver-minimo.md).

### Arquivos afetados
- Criar: componentes/rotas em `web/src/routes/` e `web/src/components/`, util de
  mediainfo em `web/src/lib/`, testes `*.test.tsx`.
- Alterar: registro de rotas/router do [0002](0002-frontend-scaffold-integracao-api.md);
  cliente de API (`web/src/lib/api.ts`) com as novas chamadas.

## 5. Critérios de aceite

- [ ] Adicionar um datasource pela UI cria via API; erro de path inválido aparece inline; a lista atualiza.
- [ ] Disparar scan mostra progresso e conclui; o grid passa a exibir os itens detectados.
- [ ] O grid mostra posters (arte local) com **fallback de placeholder**, badge de **posse** e de **revisão** (baixa confiança); filtro por tipo/posse e paginação funcionam.
- [ ] Estados de **loading/vazio/erro** existem e são claros (grid e detalhe).
- [ ] O detalhe lista os arquivos com **mediainfo legível** (resolução/codec/HDR/faixas/duração) e edição/parte.
- [ ] Usa **tipos gerados** (não à mão); `tsc`/build limpos; **testes vitest** cobrem cards, estados e a formatação de mediainfo; catraca (cobertura/lint) verde.

## 6. Notas de implementação / decisões

- **Polling, não websocket** (decisão de sessão): o progresso de scan é de baixa
  frequência; a UI só existe quando aberta ([03](../docs/03-arquitetura.md)). Um
  `refetchInterval` do TanStack Query enquanto `running` é suficiente; realtime é §7.
- **Badge de revisão** só **mostra** o alerta de baixa confiança ([ADR-0008](../docs/adr/0008-identidade-e-matching-kodi-jellyfin.md));
  confirmar/corrigir o match é fluxo da Fase 3 (precisa de override).
- **mediainfo legível** vive num util puro (fácil de testar e de manter a cobertura do
  [0005](0005-quality-gate-catraca-unificada.md) honesta), separado da renderização.
- Reusar **shadcn/ui** (Card/Badge já no [0002](0002-frontend-scaffold-integracao-api.md));
  novos componentes gerados entram nos excludes de cobertura como o combinado no 0005.

## 7. Fora deste briefing / follow-ups

- Fluxo de **revisão de match** e **fila de órfãs** (confirmar/corrigir) — Fase 3.
- **Edição de override**, arte custom, busca/filtros ricos, ordenações — Fase 3.
- **Realtime** de progresso (SSE/websocket) se o polling ficar aquém.
- Refinamento visual, responsividade fina, i18n.
