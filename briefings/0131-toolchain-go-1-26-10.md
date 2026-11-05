# 0131 — Toolchain Go 1.26.10 nas builds

> O pre-push (`make gate-local`) bloqueia com 11 vulnerabilidades — todas na
> biblioteca padrão do Go 1.26.6, corrigidas em 1.26.9/1.26.10.

| | |
|---|---|
| **ID** | 0131 |
| **Tipo** | chore |
| **Fase (roadmap)** | Transversal — Infraestrutura operacional |
| **Status** | in-progress |
| **Docs relacionadas** | [quality gate](../CONTRIBUTING.md) |
| **Depende de** | — |

---

## 1. Objetivo

O gate local (`make gate-local`, rodado também no pre-push) mede
`govulncheck -json ./...` e exige zero vulnerabilidades que afetem o código.
Em 05/nov/2026 ele acusou **11**, todas na **biblioteca padrão** do toolchain
`go1.26.6` e todas com correção publicada:

| OSV | Pacote | Corrigido em |
|---|---|---|
| GO-2026-6809 | `crypto/x509` | 1.26.10 |
| GO-2026-6617, 6613, 6612, 6611, 6610, 6609, 6605, 6603 | `net/http` | 1.26.9 |
| GO-2026-6608 | `net/textproto` | 1.26.9 |
| GO-2026-6607 | `crypto/tls` | 1.26.9 |

Nenhuma delas vem de dependência de terceiro: subir o toolchain resolve as 11
de uma vez, sem tocar em `go.sum`. O bloqueio é falso alarme de segurança, mas
o gate está certo em bloquear — o conserto é a versão do compilador, não o
baseline do gate.

## 2. Jornada do usuário

1. O dono (ou um agente) tenta `git push`.
2. O pre-push roda `make gate-local`.
3. **Antes:** "Go: vulnerabilidades 11 vulns — deve ser 0" → push recusado,
   mesmo sem nenhuma mudança de dependência no diff.
4. **Depois:** 0 vulnerabilidades; o gate olha só o que o diff introduziu.

## 3. Escopo

**Dentro:**
- `go.mod`: `toolchain go1.26.6` → `go1.26.10`.
- `Dockerfile`: `FROM --platform=$BUILDPLATFORM golang:1.26.6-alpine` →
  `golang:1.26.10-alpine`.
- `.github/workflows/quality-gate.yml` e `e2e.yml`: `go-version: '1.26.6'` →
  `'1.26.10'`. O pin é **exato** de propósito (comentário no próprio workflow) e
  `GOTOOLCHAIN=local` é default do runner — sem o bump, o `govulncheck` do CI
  roda na 1.26.6 e acusa as mesmas 11 (aconteceu no primeiro push).

**Fora (explicitamente):**
- Subir o `go` (versão mínima exigida) de `1.26.4` — quem compila com 1.26.4 já
  é aceito; só o toolchain *preferido* e os pins de build sobem.
- Atualizar dependências de terceiros (`go get -u`).
- Mexer nos baselines do gate (cobertura, gocyclo).

## 4. Contrato técnico

### Empacotamento
- A imagem final continua `alpine:3.22`; muda só o estágio `builder-go`.
- As três pontas ficam na MESMA patch: `toolchain` do `go.mod`, `go-version`
  dos workflows e o estágio `builder-go` da imagem. O runner do GitHub define
  `GOTOOLCHAIN=local`, então o `go` **não** promove sozinho para o toolchain do
  `go.mod`: a versão instalada pelo `setup-go` é a que roda o `govulncheck`.

### Arquivos afetados
- `go.mod`, `Dockerfile`, `.github/workflows/quality-gate.yml`,
  `.github/workflows/e2e.yml`

## 5. Critérios de aceite

- [ ] `govulncheck ./...` não reporta nenhuma vulnerabilidade que afete o
      código.
- [ ] `make gate-local` passa (tabela sem regressão bloqueante).
- [ ] CI verde (`quality-gate`, `e2e`) com `go-version: '1.26.10'`.
- [ ] `docker build` da imagem passa no estágio `builder-go`.

## 6. Notas de implementação / decisões

- **Por que não `GOTOOLCHAIN=local`**: fixar o toolchain no `go.mod` mantém a
  mesma versão em máquina, CI e imagem com uma fonte só; desligar o download
  automático faria cada ambiente reportar vulnerabilidades diferentes.
- **Por que subir o mínimo junto não é necessário**: `go 1.26.4` é o requisito
  de quem consome o módulo, e nada da correção depende de linguagem nova.

## 7. Fora deste briefing / follow-ups

- **Efeito colateral conhecido:** mexer em `.github/workflows/*` faz o gate
  rodar os DOIS lados (`isGateInfraFile`), e o lado web estava com dívida antiga
  de lockfile — resolvido no [0132](0132-web-dependencias-sem-critical.md), que
  precisou entrar junto para o push passar.
- Ver se o gate deve tratar achado **somente na stdlib** como aviso quando a
  correção exige trocar o toolchain (hoje ele bloqueia igual).
