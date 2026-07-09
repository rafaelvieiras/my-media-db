# ADR-0017 — Migrations do SQLite: goose embedado

- **Status:** Aceita
- **Data:** 2026-07-09
- **Agenda:** Fase 2 (fundação de persistência) · **Briefing:** [0006](../../briefings/0006-fundacao-persistencia-sqlite.md)

## Contexto

A [ADR-0014](0014-stack-de-implementacao.md) fixou o driver (`modernc.org/sqlite`,
CGO-free) e a camada de dados (**sqlc**), mas o sqlc **gera queries tipadas — não
versiona schema**. A Fase 2 introduz o primeiro schema real ([0007](../../briefings/0007-schema-nucleo-minimo.md))
e, dali em diante, toda evolução de tabela precisa de **migrations versionadas,
determinísticas e aplicáveis a partir do binário único** (sem passo externo, sem
CGO, sem quebrar o princípio de *single container*).

## Decisão

Adotar **[goose](https://github.com/pressly/goose) como biblioteca embedada**, não
como binário externo.

1. **Migrations em `.sql`** numeradas, versionadas no repo (ex.:
   `internal/db/migrations/0001_schema_inicial.sql`), com blocos
   `-- +goose Up` / `-- +goose Down`.
2. **Embutidas via `//go:embed`** num `embed.FS` e aplicadas **no startup do
   binário** através da API de biblioteca do goose (`goose.Up`), usando o mesmo
   `database/sql` aberto sobre `modernc.org/sqlite` — sem processo separado.
3. **Dialeto SQLite** configurado explicitamente (`goose.SetDialect("sqlite3")`).
4. As migrations rodam **antes** de o servidor aceitar tráfego; falha de migration
   **aborta o boot** (fail-fast).

## Consequências

- Schema versionado e reproduzível; o binário se auto-migra ao subir — coerente
  com "um container, zero-config" ([03](../03-arquitetura.md)).
- Uma dependência Go a mais, mas madura, sem CGO e compatível com o driver já
  escolhido; convive com o **sqlc** (goose versiona o DDL; o sqlc gera o acesso).
- `Down` disponível para desenvolvimento; em produção a política é **só avançar**
  (rollback é exceção manual).
- Alternativas descartadas: **golang-migrate** (mais pesada, historicamente
  atritada com o driver sqlite CGO-free) e um **runner próprio** (zero-dep, mas
  reinventa versionamento/locking que o goose já resolve). Decisões relacionadas:
  [ADR-0014](0014-stack-de-implementacao.md) (stack), [ADR-0010](0010-concorrencia-sqlite-wal-escritor-unico.md)
  (WAL + escritor único — as migrations correm pela conexão de escrita).
