# 0132 — `web/`: dependências sem vulnerabilidade crítica

> `npm ci` + `npm audit fix` no `web/` zera as 2 vulnerabilidades críticas que
> impediam o lado web do gate de passar.

| | |
|---|---|
| **ID** | 0132 |
| **Tipo** | chore |
| **Fase (roadmap)** | Transversal — Infraestrutura operacional |
| **Status** | in-progress |
| **Docs relacionadas** | [0131](0131-toolchain-go-1-26-10.md) |
| **Depende de** | — |

---

## 1. Objetivo

O lado **web** do quality gate nunca foi ratcheado desde que o filtro por diff
existe: ele só roda quando o diff toca `web/` ou um arquivo de infra do gate
(`.github/workflows/*`, `scripts/quality-gate.mjs`, `Makefile`, `baseline.json`,
`lefthook.yml`). Em 05/nov/2026, ao bumpar o toolchain Go (briefing 0131 — que
exige editar os workflows), o lado web rodou pela primeira vez em meses e
reprovou no `master` intocado:

```
Web: vulns critical   2 vulns  — deve ser 0 (blocking: 'zero')
  proxy-addr  1.1.0 - 2.0.7   (IP spoofing via IPv4-mapped IPv6 trust subnet)
  seroval     <=1.6.9         (fromJSON: script injection, memory exhaustion)
```

Diferente da duplicação (que é dívida de código), essa é dívida de *lockfile*:
as duas correções são patch (`proxy-addr` 2.0.8+, `seroval` 1.6.10+). Atualizar
resolve sem tocar em uma linha de código da aplicação.

## 2. Jornada do usuário

1. O dono muda qualquer arquivo de infra do gate (CI, `Makefile`, baseline).
2. O gate roda os dois lados.
3. **Antes:** "Web: vulns critical 2 vulns — deve ser 0" → push recusado por
   dívida que não tem relação com a mudança.
4. **Depois:** 0 critical; o lado web volta a ser uma catraca útil, e o próximo
   push de infra não é bloqueado por dependência antiga.

## 3. Escopo

**Dentro:**
- `web/package-lock.json`: `npm ci` (árvore limpa) + `npm audit fix` (sem
  `--force`, só patch/minor compatível).

**Fora (explicitamente):**
- `npm audit fix --force` / atualização major de qualquer dependência.
- Zerar as 16 moderadas e 2 baixas restantes (não são bloqueantes).
- Reduzir a duplicação do front (medida em 0,34 % — já abaixo do baseline
  0,37 %, nada a fazer).

## 4. Contrato técnico

### Web (TypeScript)
- Nenhuma mudança em `web/src`, `web/package.json` ou configs.
- Só o lockfile se move; `npm ci` passa a instalar a árvore corrigida.

### Arquivos afetados
- `web/package-lock.json`

## 5. Critérios de aceite

- [ ] `npm audit` não reporta nenhuma vulnerabilidade **critical**.
- [ ] `npm run build` (tsc + vite) passa.
- [ ] `npm run lint` sem issues.
- [ ] `npm test` verde e cobertura de linhas ≥ baseline (94,42 %).
- [ ] `make gate-local --full` sem regressão bloqueante no lado web.

## 6. Notas de implementação / decisões

- **Por que `npm ci` antes do `audit fix`**: a árvore local estava
  dessincronizada do lockfile; rodar o fix sobre ela produziria um lock
  diferente do que o CI instala.
- **Por que não `--force`**: forçaria major (ex. `vite`, `shadcn`) e o gate
  voltaria a rodar por outro motivo (build quebrado) — escopo errado.
- **Duplicação medida com a árvore limpa: 0,34 %** (132 de 38.433 linhas). A
  leitura de 4,08 % veio de uma árvore local sem `npm ci`; não era real.

## 7. Fora deste briefing / follow-ups

- Um refresh de dependências do `web/` de verdade (major, com revalidação do
  front) merece briefing próprio.
