# 0078 — Polimentos: badge de status traduzido, paridade de uid no dev e retry do navegador de pastas

> Três achados pequenos e independentes da auditoria de UX, bundlados por
> serem baixo esforço/baixo risco cada um: tradução de um badge, paridade de
> permissão de container entre dev e produção, e uma chamada repetida no
> console.

| | |
|---|---|
| **ID** | 0078 |
| **Tipo** | fix/chore |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | done |
| **Docs relacionadas** | [09-nfo-e-export](../docs/09-nfo-e-export.md), [deploy](../docs/deploy.md) |
| **Depende de** | 0004, 0071 |

---

## 1. Objetivo

Três achados de baixa prioridade, sem relação funcional entre si, mas cada um
pequeno o suficiente para não justificar um briefing próprio:

1. **Badge de status não traduzido**: em `web/src/routes/entity-detail.tsx`
   (linha ~312), `<Badge variant="outline">{data.status}</Badge>` mostra o
   valor cru do enum (`cataloged`) quando o item não é rascunho — o resto da
   tela é bem traduzido (inclusive termos técnicos), esse badge é a exceção.
   O backend só usa dois valores de status (`draft`/`cataloged` — ver
   `internal/scanner/match.go`, `internal/server/completeness.go`); `draft`
   já tem tratamento especial (`ehDraft` → "não identificado"), só
   `cataloged` precisa de rótulo.
2. **Paridade de usuário no container de dev**: `Dockerfile.dev` não define
   `USER`, então o serviço `api` do `compose.dev.yaml` roda como `root` — NFO
   e artes exportadas para a pasta da biblioteca nascem com dono `root`,
   ilegíveis pelo próprio usuário no host. `compose.prod.yaml` já resolve
   isso (`user: "1000:1000"`, linha 47) — o gap é só no ambiente de
   desenvolvimento, e pode mascarar bugs reais de exportação atrás de um
   problema de permissão que não existe em produção (como aconteceu durante
   a própria auditoria desta rodada).
3. **Retry automático mascarado de "chamada tripla"**: editar um datasource
   offline (`/library/$id/edit`) dispara 3 chamadas idênticas a `GET
   /api/v1/library/browse?path=...`, todas 422. A causa mais provável não é
   um efeito duplicado de renderização, e sim o comportamento PADRÃO do
   TanStack Query: `useQuery` tenta de novo automaticamente (`retry: 3` por
   padrão) uma consulta que falhou — inclusive para um erro 422 (path
   inválido), que é permanente e nunca vai ter sucesso na 2ª/3ª tentativa.
   `libraryBrowseQueryOptions` (`web/src/lib/queries.ts`) não desliga retry.

## 2. Escopo

**Dentro:**
- Mapear `status: "cataloged"` para "catalogado" no badge de
  `entity-detail.tsx` (mesmo padrão de tradução de enum já usado em outros
  lugares do app, ex. `rotuloItemFaltando`/`rotuloTipo`).
- `compose.dev.yaml`/`Dockerfile.dev`: rodar o serviço `api` como uid/gid não-
  root, espelhando `compose.prod.yaml`.
- `libraryBrowseQueryOptions` (`web/src/lib/queries.ts`): desligar retry (ou
  limitar a erros que fazem sentido re-tentar) para essa query especificamente,
  já que um 422 de path inválido é permanente.

**Fora (explicitamente):**
- Qualquer outro enum não traduzido que a implementação encontrar fora do
  escopo desta auditoria (achado específico foi só o badge de status).
- Mudar a política de retry GLOBAL do QueryClient (`web/src/lib/query-
  client.ts` ou equivalente) — o ajuste é local a essa query, não uma
  mudança de comportamento do app inteiro.
- Investigar/tratar o 422 como estado visível na UI (ex.: "não foi possível
  pré-visualizar esta pasta") — a auditoria sugeriu isso como secundário; o
  bug de fato (retry inútil gerando ruído de console) é o que este briefing
  resolve; se sobrar tempo, tratar o estado visível é bem-vindo mas não é
  critério de aceite.

## 3. Contrato técnico

### Frontend (Vite/React)

- `web/src/routes/entity-detail.tsx`: mapa `{ cataloged: 'catalogado' }` (ou
  reaproveitar/estender um helper existente de tradução de enum, se houver
  um lugar canônico para isso) no lugar de `{data.status}` cru.
- `web/src/lib/queries.ts` (`libraryBrowseQueryOptions`): adicionar `retry:
  false` (mais simples, já que 422 é sempre permanente para este endpoint —
  path inválido não se resolve tentando de novo sem mudar o `path`) à
  `queryOptions`.

### Infra (Docker)

- `Dockerfile.dev`: adicionar `USER` (ou equivalente) para uid/gid 1000:1000,
  mesmo valor de `compose.prod.yaml`. Cuidado: o `go install
  github.com/air-verse/air@latest` e o `apt-get install ffmpeg` já rodam como
  root ANTES do `USER` (build steps), só o `CMD`/runtime precisa do usuário
  não-root — checar que `$GOPATH/bin` (onde `air` fica) continua no `PATH` e
  acessível pelo uid 1000 (pode exigir `ENV GOPATH=...`/ajuste de
  permissão do diretório de instalação, dependendo de como a imagem base
  `golang:1.26` organiza `$GOPATH`).
- `compose.dev.yaml`: adicionar `user: "1000:1000"` ao serviço `api` (mesmo
  padrão do `compose.prod.yaml`), e confirmar que os volumes nomeados/bind
  mounts usados pelo `air` (cache de módulos/build) continuam graváveis por
  esse uid — se algum volume nomeado já tiver conteúdo criado como root de
  uma sessão anterior, pode ser necessário recriá-lo
  (`docker compose -f compose.dev.yaml down -v` — **ação destrutiva de
  volumes locais, confirmar com o dono antes de rodar**).

### Arquivos afetados

- `web/src/routes/entity-detail.tsx` (+ `.test.tsx`).
- `web/src/lib/queries.ts` (+ teste, se houver suíte própria para
  `libraryBrowseQueryOptions`).
- `Dockerfile.dev`.
- `compose.dev.yaml`.

## 4. Critérios de aceite

- [ ] Um item com `status: "cataloged"` mostra o badge "catalogado" (pt-BR),
      não o valor cru do enum.
- [ ] Abrir `/library/$id/edit` para um datasource com path offline dispara
      **uma** chamada a `GET /library/browse` para aquele path, não três
      (verificável no console/rede).
- [ ] O serviço `api` de `compose.dev.yaml` roda como uid/gid 1000:1000 (não
      root); NFO/artes exportadas para uma pasta de biblioteca montada do
      host nascem com dono correspondente ao usuário do host (não `root`),
      legíveis sem `sudo`.
- [ ] O loop de desenvolvimento (`docker compose -f compose.dev.yaml up
      --build`, hot reload via `air`) continua funcionando normalmente após
      a mudança de usuário — sem regressão de permissão de escrita em
      cache/build.
- [ ] `make gate-local` passa (a mudança de Docker não é coberta pela catraca
      de código, mas validar manualmente que o container sobe e o app
      funciona).

## 5. Notas de implementação / decisões

- **Por que crer que é retry, não um efeito duplicado?** TanStack Query usa
  `retry: 3` por padrão para qualquer `useQuery` que não desliga
  explicitamente — 3 tentativas + a primeira = os "3 erros 422 repetidos"
  batem exatamente com esse default, sem precisar de nenhum efeito rodando
  em duplicata. Vale confirmar isso na prática antes de assumir uma causa
  mais complexa (ex.: React StrictMode em dev, que double-invoca efeitos mas
  normalmente é deduplicado pelo cache de query — não geraria 3 requests
  network distintos por si só).
- Este briefing NÃO tem "Fora deste briefing / follow-ups" — os três itens
  são achados de baixa prioridade sem desdobramento esperado.
