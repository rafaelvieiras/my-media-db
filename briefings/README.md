# Briefings

Um **briefing** é uma **ordem de serviço autocontida** para uma única unidade de
trabalho: uma feature, uma correção ou uma tarefa técnica. Ele reúne tudo o que
é preciso para implementar aquilo **sem depender de contexto externo** — o que
fazer, por quê, a jornada do usuário, o contrato técnico e como saber que ficou
pronto.

O desenvolvimento do MyMediaDB é **dirigido por briefings**: nada é implementado
sem um briefing correspondente. As `docs/` descrevem *como a aplicação é*; os
briefings descrevem *o que construir agora e como*.

## Princípios

1. **Autocontido.** Quem pega o briefing não deveria precisar caçar informação
   em conversas ou na cabeça de ninguém. Se algo é necessário, está no briefing
   (ou linkado explicitamente numa doc).
2. **Uma unidade de trabalho.** Um briefing = uma coisa entregável e testável.
   Se está grande demais para um critério de aceite claro, quebre em dois.
3. **Rastreável ao roadmap.** Todo briefing referencia a fase do roadmap
   ([`docs/12`](../docs/12-roadmap.md)) e as docs que fundamentam suas decisões.
4. **Jornada antes de código.** O briefing parte da **experiência do usuário** e
   só então desce ao contrato técnico. O código serve à jornada, não o contrário.
5. **Decisões ficam registradas.** Escolhas técnicas tomadas durante o briefing
   viram texto no próprio briefing; decisões estruturais sobem para as `docs/`.

## Nomenclatura

```
briefings/NNNN-slug-curto.md
```

- `NNNN` — número sequencial de 4 dígitos, na ordem de criação (`0001`, `0002`…).
- `slug-curto` — kebab-case descritivo (`0003-scan-de-datasource`).
- `_template.md` — o modelo; **não** é um briefing (prefixo `_`).

## Estados

Cada briefing carrega um `status` no cabeçalho:

| Status | Significado |
|---|---|
| `draft` | Em escrita/discussão; ainda não pronto para implementar |
| `ready` | Aprovado e completo; pode ser implementado |
| `in-progress` | Implementação em andamento |
| `done` | Implementado e aceito (critérios de aceite satisfeitos) |
| `blocked` | Travado por dependência (indicar qual) |
| `dropped` | Abandonado (manter o arquivo com o motivo, para histórico) |

## Como usar

1. Copie [`_template.md`](_template.md) para `NNNN-slug.md`.
2. Preencha começando pela **jornada do usuário** e o **objetivo**; o contrato
   técnico vem depois.
3. Discuta/refine enquanto `draft`. Ao fechar, marque `ready`.
4. Implemente guiado pelo briefing; atualize o `status` conforme avança.
5. Se uma decisão estrutural surgir, atualize a `docs/` correspondente e linke.

## Índice

<!-- Atualizar conforme briefings forem criados -->

### Fase 1 — Esqueleto do container único

| ID | Briefing | Tipo | Status | Depende de |
|---|---|---|---|---|
| [0001](0001-backend-esqueleto-servidor.md) | Backend Go: esqueleto do servidor HTTP | feature | done | — |
| [0002](0002-frontend-scaffold-integracao-api.md) | Frontend Vite + React: scaffold e integração com a API | feature | done | — |
| [0003](0003-empacotamento-dockerfile-multistage.md) | Empacotamento: Dockerfile multi-stage (imagem única) | chore | done | 0001, 0002 |
| [0004](0004-ambiente-de-desenvolvimento-hot-reload.md) | Ambiente de desenvolvimento: compose com hot reload | chore | done | 0001, 0002 |
| [0005](0005-quality-gate-catraca-unificada.md) | Quality gate: catraca unificada (ratchet) Go + TS | chore | done | 0001, 0002 |

### Fase 2 — UI de biblioteca

| ID | Briefing | Tipo | Status | Depende de |
|---|---|---|---|---|
| [0006](0006-fundacao-persistencia-sqlite.md) | Fundação de persistência SQLite (WAL, pools, migrations, sqlc) | feature | done | 0001 |
| [0007](0007-schema-nucleo-minimo.md) | Schema do núcleo (mínimo da Fase 2) | feature | done | 0006 |
| [0008](0008-datasources-cadastro-api-library.md) | Datasources: cadastro e API `/api/v1/library` | feature | ready | 0007 |
| [0009](0009-scanner-fswalk-ffprobe-matching.md) | Scanner: fs walk + ffprobe + matching Kodi/Jellyfin | feature | ready | 0007, 0008 |
| [0010](0010-api-leitura-resolver-minimo.md) | API de leitura + resolver mínimo (camada arquivo) | feature | ready | 0007, 0009 |
| [0011](0011-frontend-grid-lista-detalhe.md) | Frontend: grid de posters, lista e detalhe com mediainfo | feature | ready | 0008, 0009, 0010 |
