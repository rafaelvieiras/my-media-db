# 0002 — Frontend Vite + React: scaffold e integração com a API

> SPA Vite+React (+shadcn/ui) que builda para o embed e prova a flag `VITE_API_BASE`
> consumindo `/api/v1/health`.

| | |
|---|---|
| **ID** | 0002 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 1 — Esqueleto do container único |
| **Status** | draft |
| **Docs relacionadas** | [03-arquitetura](../docs/03-arquitetura.md), [ADR-0014](../docs/adr/0014-stack-de-implementacao.md) |
| **Depende de** | — (mas o `outDir` deve casar com [0001](0001-backend-esqueleto-servidor.md); ver Contrato) |

---

## 1. Objetivo

Levantar o **cliente** do produto: a SPA que, em desenvolvimento, fala com a API Go
por uma base configurável e, em produção, roda no mesmo *origin* embutida no binário.
O entregável prova a ponta a ponta da flag `VITE_API_BASE` mostrando o estado do
`/api/v1/health` na tela — a fumaça que valida a arquitetura de dois modos.

## 2. Jornada do usuário

1. Em dev, o dev roda `npm run dev` (Vite na `5173`) com
   `VITE_API_BASE=http://localhost:8080`.
2. A página inicial pinga `/api/v1/health` e mostra: **"API online — vX"** (verde),
   **"conectando…"** (loading) ou **"API indisponível"** (erro, sem quebrar a página).
3. Em produção (`VITE_API_BASE` vazio), o mesmo app usa **caminhos relativos** e o
   binário Go serve tudo no mesmo *origin* — a tela mostra o mesmo status.

## 3. Escopo

**Dentro:**
- Scaffold **Vite + React + TypeScript**.
- **Tailwind + shadcn/ui** como base de componentes.
- Cliente de API com **base configurável** (`VITE_API_BASE`).
- Uma tela de status que consome `health` com estados **loading / erro / ok**.
- `npm run build` gerando o `dist` no local que o [0001](0001-backend-esqueleto-servidor.md) embute.

**Fora (explicitamente):**
- Telas de biblioteca, grid de posters, detalhe (Fase 2).
- Roteamento multi-página, autenticação, tema além do mínimo.
- Geração de tipos por OpenAPI (só faz sentido com rotas além de `health`).

## 4. Contrato técnico

### Frontend (Vite/React)
- Projeto em **`/web`** (raiz do Vite).
- `vite.config.ts`: `build.outDir = "../internal/webui/dist"`, `emptyOutDir: true` —
  gera exatamente no diretório que o Go embute ([0001](0001-backend-esqueleto-servidor.md)).
- `src/lib/api.ts`: `const base = import.meta.env.VITE_API_BASE ?? ""` e
  `fetch(\`${base}/api/v1/health\`)`. Base vazia ⇒ caminho relativo (mesmo origin).
- `src/App.tsx`: card (shadcn) com o status da API e a versão retornada.
- Tratar erro de rede/timeout como estado visível, nunca exceção não-capturada.

### Arquivos afetados
- Criar `web/`: `package.json`, `vite.config.ts`, `tsconfig.json`, `tailwind.config`,
  `index.html`, `src/main.tsx`, `src/App.tsx`, `src/lib/api.ts`, setup do shadcn/ui.
- `.gitignore`: ignorar `web/node_modules` e o `internal/webui/dist` gerado (exceto o
  placeholder do 0001 — alinhar).

## 5. Critérios de aceite

- [ ] `npm run dev` sobe a `5173` e a tela mostra **"API online"** contra a API na `8080`.
- [ ] Com a API desligada, a tela mostra **"API indisponível"** sem tela branca/crash.
- [ ] `npm run build` gera `internal/webui/dist/` com `index.html` + `assets/`.
- [ ] Com `VITE_API_BASE` vazio, as chamadas são **relativas** (verificável no Network).
- [ ] shadcn/ui operante (ao menos um componente real em uso).

## 6. Notas de implementação / decisões

- A base configurável e os dois modos (dev/mesmo-origin) seguem a
  [doc 03](../docs/03-arquitetura.md).
- `outDir` apontando para o pacote Go é a consequência prática da regra do
  `//go:embed` (sem `../`); mantém um único artefato final.
- shadcn/ui escolhido no roadmap da Fase 1 como base de componentes.

## 7. Fora deste briefing / follow-ups

- Cliente de API tipado a partir de **OpenAPI** ([ADR-0014](../docs/adr/0014-stack-de-implementacao.md))
  quando surgirem rotas de dados.
- Design system / tema e layout de navegação, ao começar a Fase 2.
