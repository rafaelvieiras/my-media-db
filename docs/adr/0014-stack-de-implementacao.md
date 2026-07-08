# ADR-0014 — Stack de implementação (backend + contrato de tipos)

- **Status:** Aceita
- **Data:** 2026-07-08
- **Agenda:** [doc 11, itens 1–5](../11-decisoes-abertas.md) · **Spec:** [03](../03-arquitetura.md), [05](../05-api.md), [06](../06-biblioteca-e-scan.md)

## Contexto

A doc 11 listava escolhas de ferramenta do backend com inclinações anotadas, mas
sem ratificação. Todas orbitam o mesmo pilar: **binário único, CGO-free, memória
mínima, cross-compile trivial (incl. ARM)**.

## Decisão

Confirmar o conjunto (todas as inclinações da doc 11):

- **Router HTTP:** **chi** — minimalista, sobre `net/http`, middlewares componíveis
  (em vez de echo, mais pesado/opinativo).
- **Driver SQLite:** **`modernc.org/sqlite`** — SQLite puro em Go, **sem CGO**
  (em vez de `mattn/go-sqlite3`, que exige CGO e complica o cross-compile).
- **Camada de dados:** **sqlc** — SQL tipado gerando Go, sem ORM (preserva controle
  fino sobre FTS5, colunas geradas e JSON1).
- **Mediainfo:** **ffprobe** (FFmpeg), subprocesso com saída JSON estável
  (em vez de `mediainfo-cli`, menos ubíquo).
- **Contrato de tipos:** **OpenAPI gerado** a partir da API é a fonte de verdade dos
  tipos TS do front (em vez de tipos mantidos à mão, que divergem).

## Consequências

- Um binário estático realmente portátil; build e distribuição simples.
- Trade-off de performance do modernc/ffprobe é irrelevante no uso self-hosted.
- Escolhas coerentes com [ADR-0010](0010-concorrencia-sqlite-wal-escritor-unico.md)
  (WAL/pools no mesmo driver) e com a arquitetura de subprocessos da doc 03.
- São decisões de **Fase 1**, reversíveis se um pressuposto mudar.
