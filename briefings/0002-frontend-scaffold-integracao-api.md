# 0002 — Frontend Vite + React: scaffold e integração com a API

> SPA Vite+React (+shadcn/ui, TanStack Router + Query) que builda para o embed e prova
> a flag `VITE_API_BASE` consumindo `/api/v1/health`.

| | |
|---|---|
| **ID** | 0002 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 1 — Esqueleto do container único |
| **Status** | ready |
| **Docs relacionadas** | [03-arquitetura](../docs/03-arquitetura.md), [ADR-0014](../docs/adr/0014-stack-de-implementacao.md) |
| **Depende de** | — (mas o `outDir` deve casar com [0001](0001-backend-esqueleto-servidor.md); ver Contrato) |

---

## 1. Objetivo

Levantar o **cliente** do produto: a SPA que, em desenvolvimento, fala com a API Go
por uma base configurável e, em produção, roda no mesmo *origin* embutida no binário.
O entregável prova a ponta a ponta da flag `VITE_API_BASE` mostrando o estado do
`/api/v1/health` na tela — a fumaça que valida a arquitetura de dois modos.

## 2. Jornada do usuário

1. Em dev, o dev roda `npm run dev` (Vite na `5173`); o **proxy do Vite** encaminha
   `/api` → back na `8080`, então o front usa **caminho relativo** — igual à produção.
2. A página inicial pinga `/api/v1/health` e mostra: **"API online — vX"** (verde),
   **"conectando…"** (loading) ou **"API indisponível"** (erro, sem quebrar a página).
3. Em produção (`VITE_API_BASE` vazio), o mesmo app usa os **mesmos caminhos relativos**
   e o binário Go serve tudo no mesmo *origin* — a tela mostra o mesmo status. Dev e prod
   compartilham o mesmo contrato; `VITE_API_BASE` fica como *override* opcional (ex.:
   apontar para uma API remota).

## 3. Escopo

**Dentro:**
- Scaffold **Vite + React + TypeScript**.
- **Tailwind + shadcn/ui** como base de componentes.
- **TanStack Router** com `queryClient` no contexto do router (base de navegação
  tipada, pronta para as telas da Fase 2).
- **TanStack Query** como camada de estado de servidor (`QueryClientProvider`).
- Cliente de API com **base configurável** (`VITE_API_BASE`).
- Uma tela de status que consome `health` via TanStack Query com estados
  **loading / erro / ok**.
- `npm run build` gerando o `dist` no local que o [0001](0001-backend-esqueleto-servidor.md) embute.

**Fora (explicitamente):**
- Telas de biblioteca, grid de posters, detalhe (Fase 2).
- Múltiplas rotas de conteúdo, autenticação, tema além do mínimo (o router entra
  agora, mas com uma rota só; as telas vêm na Fase 2).
- Geração de tipos por OpenAPI (só faz sentido com rotas além de `health`).

## 4. Contrato técnico

### Frontend (Vite/React)
- Projeto em **`/web`** (raiz do Vite).
- `vite.config.ts`: `build.outDir = "../internal/webui/dist"`, `emptyOutDir: true` —
  gera exatamente no diretório que o Go embute ([0001](0001-backend-esqueleto-servidor.md)).
- `vite.config.ts` `server.proxy`: `/api` → `http://localhost:8080` (ou o host do back
  no compose do [0004](0004-ambiente-de-desenvolvimento-hot-reload.md)) — em dev o front
  chama caminho relativo e o Vite encaminha; **sem CORS**.
- `src/lib/api.ts`: `const base = import.meta.env.VITE_API_BASE ?? ""` e
  `fetch(\`${base}/api/v1/health\`)`. Base vazia (dev **e** prod) ⇒ caminho relativo.
  `VITE_API_BASE` só é preenchido para o caso de override (API remota).
- **TanStack Router**: `createRouter({ routeTree, context: { queryClient }, Wrap: QueryClientProvider })`;
  `declare module '@tanstack/react-router'` registrando o tipo do router; **uma** rota
  (`/`) neste briefing. Adotar rotas **code-based** por ora (o `routeTree.gen` file-based
  entra quando houver várias telas, na Fase 2).
- **TanStack Query**: um `QueryClient` compartilhado, injetado no contexto do router e
  provido via `Wrap`. A rota `/` usa `queryOptions` + `useQuery` para o `health`.
- `src/routes/index.tsx` (ou equivalente): card (shadcn) com o status da API e a versão,
  derivado dos estados do `useQuery` (`isPending` / `isError` / `data`).
- Tratar erro de rede/timeout como estado visível, nunca exceção não-capturada.

### Arquivos afetados
- Criar `web/`: `package.json`, `vite.config.ts`, `tsconfig.json`, `tailwind.config`,
  `index.html`, `src/main.tsx`, `src/router.tsx`, `src/routes/…`, `src/lib/api.ts`,
  `src/lib/queries.ts` (queryOptions), setup do shadcn/ui.
- `.gitignore`: ignorar `web/node_modules` e o `internal/webui/dist` gerado (exceto o
  placeholder do 0001 — alinhar).

## 5. Critérios de aceite

- [ ] `npm run dev` sobe a `5173` e a tela mostra **"API online"** com o back na `8080`
      **via proxy do Vite** (front chamando `/api` relativo, sem CORS).
- [ ] Com a API desligada, a tela mostra **"API indisponível"** sem tela branca/crash.
- [ ] `npm run build` gera `internal/webui/dist/` com `index.html` + `assets/`.
- [ ] Com `VITE_API_BASE` vazio (dev e prod), as chamadas são **relativas** (verificável no Network).
- [ ] shadcn/ui operante (ao menos um componente real em uso).
- [ ] O `health` é consumido via **TanStack Query** (não `fetch` solto no componente).
- [ ] O app roda sob o **TanStack Router** com uma rota (`/`) e o `queryClient` no
      contexto do router (tipagem do router registrada, sem erro de type-check).

## 6. Notas de implementação / decisões

- A base configurável e os dois modos (dev/mesmo-origin) seguem a
  [doc 03](../docs/03-arquitetura.md).
- `outDir` apontando para o pacote Go é a consequência prática da regra do
  `//go:embed` (sem `../`); mantém um único artefato final.
- shadcn/ui escolhido no roadmap da Fase 1 como base de componentes.
- **TanStack Query + TanStack Router entram já na Fase 1** (decisão de sessão,
  2026-07-08). Motivo: o MyMediaDB é pesado em estado de URL (filtros/ordenação/
  paginação) e o par foi desenhado para andar junto — `queryClient` no contexto do
  router, loader alimentando o cache do Query. Antecipar o scaffold evita retrabalho
  quando as telas de biblioteca chegarem na Fase 2. Trade-off aceito: curva de
  aprendizado maior que `fetch` cru + React Router. Alternativas descartadas:
  `fetch` nativo (não escala pro estado das telas de catálogo) e React Router
  (search params na mão, integração manual com o Query).

## 7. Fora deste briefing / follow-ups

- Cliente de API tipado a partir de **OpenAPI** ([ADR-0014](../docs/adr/0014-stack-de-implementacao.md))
  quando surgirem rotas de dados.
- Design system / tema e layout de navegação, ao começar a Fase 2.
