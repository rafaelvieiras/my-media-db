# Contribuindo com o MyMediaDB

Obrigado pelo interesse! O MyMediaDB é software livre sob a licença
[MIT](LICENSE). Este guia resume **como o projeto trabalha** — leia antes de
abrir um PR grande, porque o processo é deliberadamente orientado a decisão e a
documentação, não só a código.

> **Status:** `Fase 0 — documentação em draft`. Neste momento o trabalho é
> majoritariamente de **fundação**: decidir e escrever o modelo antes de codar.

## Como o projeto pensa

Cinco princípios governam qualquer contribuição (detalhe no [README](README.md)):

1. **O dono é a autoridade.** O banco local é a fonte de verdade; dado externo é
   sugestão. Toda edição do usuário é um **override** que vence sempre.
2. **O banco é maior que a biblioteca.** Cataloga o que se tem *e* o que ainda
   não se tem, via `status` — nunca gambiarra.
3. **Universal e extensível sem tocar no núcleo.** Verticais e integrações vivem
   em `extension_data` *namespaced*; não se adicionam colunas por tipo.
4. **Local-first e leve.** Um binário, uma imagem, memória mínima. Fontes
   externas são enriquecimento opcional.
5. **Não-destrutivo.** Nada apaga dado do dono automaticamente; purga é sempre
   explícita.

Um PR que contraria um desses princípios provavelmente será recusado — abra uma
discussão antes.

## Decisões primeiro, código depois

Decisões de fundação são registradas e rastreadas, não improvisadas no código:

- A **[agenda de decisões](docs/00-agenda-de-decisoes.md)** é o backlog vivo: cada
  item nasce `aberto`, passa por discussão e vira `documentado`.
- Decisões fechadas viram **[ADRs](docs/adr/)** (formato leve) e são promovidas às
  docs de spec (`docs/03`, `docs/04`, …).
- O desenvolvimento é dirigido por **[briefings](briefings/)** — ordens de serviço
  autocontidas, uma por feature/fix.

Antes de propor mudança estrutural (schema, resolver, API), verifique se ela toca
uma decisão já fechada. Se tocar, o caminho é **propor um novo ADR que supere o
anterior**, não editar a spec direto.

## Fluxo de contribuição

1. **Abra uma issue** descrevendo o problema/ideia antes de codar algo grande.
2. **Faça um fork e um branch** a partir de `master` (ex.: `feat/scan-incremental`).
3. **Siga a stack e o estilo** do código vizinho (Go no backend, Vite/React no
   front — ver [docs/03](docs/03-arquitetura.md)).
4. **Escreva em português brasileiro** nas docs e mensagens de commit, com acentuação
   correta (o projeto é mantido nesse idioma).
5. **Mensagens de commit** no formato `tipo: resumo` (ex.: `docs:`, `feat:`,
   `fix:`, `refactor:`), no imperativo.
6. **Abra o PR** referenciando a issue e, quando aplicável, o ADR/briefing.

## Quality gate e hooks de git

O projeto tem uma **catraca de qualidade unificada** (Go + TS) — um único
`baseline.json` na raiz que só permite as métricas **melhorarem**. Ela roda no
`pre-push` local e no PR (ver [briefing 0005](briefings/0005-quality-gate-catraca-unificada.md)
e [ADR-0016](docs/adr/0016-quality-gate-catraca-unificada.md)).

**Instale os hooks uma vez** (usa [lefthook](https://github.com/evilmartians/lefthook)):

```sh
lefthook install
# lefthook via: go install github.com/evilmartians/lefthook@latest
#          ou:  npm install -g lefthook   (ou npx lefthook install)
```

Isso ativa três ganchos:

- **`pre-commit`** — auto-formata os arquivos staged (`gofmt -w` / `prettier --write`)
  e os re-adiciona. Precisa só de `gofmt` (vem com o Go) e do `prettier` (no `web/`).
- **`pre-push`** — roda a catraca (`node scripts/quality-gate.mjs --local`) e
  **aborta o push** em regressão bloqueante.
- **`commit-msg`** — valida Conventional Commits (`tipo: resumo`, em pt-BR).

**Ferramentas do `pre-push`.** A catraca mede o Go com ferramentas que precisam
estar no `PATH`. Se faltar alguma, a métrica é **pulada com aviso** (não quebra),
mas o ideal é tê-las todas:

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
go install golang.org/x/vuln/cmd/govulncheck@latest
go install github.com/fzipp/gocyclo/cmd/gocyclo@latest
# garanta que "$(go env GOPATH)/bin" está no seu PATH
```

> **Nota:** `govulncheck` é zero-tolerância. Mantenha o toolchain Go atualizado
> (o `go.mod` fixa `toolchain go1.26.5`, que já corrige as vulns de stdlib conhecidas).

**Comandos à mão** (Makefile na raiz):

| Comando | O que faz |
|---|---|
| `make fmt` | Formata Go (`gofmt`) e web (`prettier`) |
| `make gate-local` | Roda a catraca com saída humana |
| `make gate` | Roda a catraca em modo CI (Markdown) |
| `make baseline` | Regenera e **avança** o `baseline.json` (só quando você genuinamente melhorou as métricas) |

## Segurança

**Segredos nunca no código.** Chaves de API e credenciais entram por
**variável de ambiente / config**, jamais commitadas. Se encontrar um segredo no
histórico, reporte de forma privada (não abra issue pública) para que seja
**rotacionado**.

## Licença da sua contribuição

Ao contribuir, você concorda em licenciar o seu trabalho sob a **licença MIT** do
projeto.
