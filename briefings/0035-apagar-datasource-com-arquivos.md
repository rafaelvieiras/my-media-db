# 0035 — Apagar datasource com arquivos (corrige 500 da FK)

> Deletar uma fonte que já escaneou arquivos deve funcionar: apaga os `media_file`
> da fonte (e seus vínculos), preserva as entities do catálogo.

| | |
|---|---|
| **ID** | 0035 |
| **Tipo** | fix |
| **Fase (roadmap)** | Fase 2 — UI de biblioteca (correção) |
| **Status** | draft |
| **Docs relacionadas** | [0008](0008-datasources-cadastro-api-library.md), [0034](0034-scanner-de-series-arquivo-episodio.md), [ADR-0010](../docs/adr/0010-escritor-unico-sqlite.md) |
| **Depende de** | — |

---

## 1. Objetivo

`DELETE /api/v1/library/{id}` responde **500** quando a fonte já tem `media_file`
escaneados. Causa (pré-existente, migração 0003): `media_file.library_source_id`
referencia `library_source(id)` **sem `ON DELETE`**; com `foreign_keys=ON`, apagar
a fonte viola a FK. `Apagar` hoje só executa `DELETE FROM library_source`. Afeta
filme e série. Encontrado no smoke da Fase 2.

## 2. Jornada do usuário

1. O usuário tem um datasource já escaneado (com arquivos) e clica em remover.
2. A fonte some da lista; os arquivos daquela fonte somem do catálogo (deixam de
   contar como posse). **As entities (filmes/séries/episódios) permanecem** como
   metadado — coerente com a decisão de **não purgar entities** (0008 §6).
3. Sem erro.

## 3. Escopo

**Dentro:**
- `library.Apagar` passa a, **na mesma transação de escrita** (escritor único,
  ADR-0010): (1) apagar os `media_file` da fonte (`WHERE library_source_id = ?`),
  o que **cascateia** os vínculos `media_file_entity` (FK `ON DELETE CASCADE` já
  existe); (2) apagar a `library_source`.
- Teste cobrindo: fonte com arquivo + vínculo de episódio → delete → 204; fonte,
  media_file e media_file_entity sumiram; a entity (série/episódio/filme) **continua**.

**Fora (explicitamente):**
- Purga de entities órfãs (fica como está — 0008 §6).
- Marcar arquivos como `missing` em vez de apagar — descartado: a fonte sumiu, os
  arquivos dela também; manter linhas apontando para uma fonte inexistente é lixo.

## 4. Contrato técnico

### Backend (Go)
- Nova query sqlc (ASCII) `ApagarMediaFilesPorFonte` (`DELETE FROM media_file WHERE
  library_source_id = ?`). Regenerar sqlc.
- `internal/library/library.go` `Apagar`: abrir tx pela conexão de escrita, rodar
  `ApagarMediaFilesPorFonte(id)` e `ApagarLibrarySource(id)`, commit. Preservar a
  semântica de erro atual (`respondLibraryError`). Idempotência: apagar fonte
  inexistente segue o comportamento atual.
- **Alternativa descartada:** migração de rebuild da `media_file` para `ON DELETE
  CASCADE` no `library_source_id`. Correta em tese, mas rebuild de tabela no SQLite
  (recriar/copiar/renomear) é arriscado (preservar todas as colunas/índices/FKs) e
  `Apagar` é o único caminho de exclusão — a correção em Go é suficiente e segura.

### Arquivos afetados (previsão)
- `internal/db/queries/media_file.sql` (ou onde ficam as queries de media_file) +
  sqlc regen; `internal/library/library.go`; teste em `internal/library` e/ou
  `internal/server`.

## 5. Critérios de aceite

- [ ] `DELETE` de fonte com arquivos → **204**; fonte, `media_file` e
      `media_file_entity` da fonte apagados; entities preservadas.
- [ ] `DELETE` de fonte sem arquivos segue funcionando.
- [ ] Tudo numa transação (não deixa estado meio-apagado se algo falhar).
- [ ] Catraca completa verde.

## 6. Notas de implementação / decisões

- Escritor único (ADR-0010): as duas exclusões vão na MESMA tx pela conexão de
  escrita, nunca em conexões separadas.
- O cascade de `media_file_entity` (0034) já cobre os vínculos ao apagar o
  `media_file` — não precisa apagá-los à mão.

## 7. Fora deste briefing / follow-ups

- UI de confirmação de exclusão (se ainda não houver) — fora do escopo.
