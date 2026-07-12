# 0047 — Robustez de fonte-default: não assumir "tmdb" quando ele não está registrado

> Remover o acoplamento a `"tmdb"` como fonte-default de import/enrich e no
> interno-first, para que um ambiente com só o TVDB (ou qualquer conjunto de
> providers) não caia em 502 nem perca a marcação "na biblioteca".

| | |
|---|---|
| **ID** | 0047 |
| **Tipo** | fix |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | ready |
| **Docs relacionadas** | [09-providers](../docs/09-providers.md) |
| **Depende de** | — |

---

## 1. Objetivo

A busca de filmes **já** é provider-independente (o loop `mesclarBuscas` itera
todos os providers; o TVDB retorna vazio-gracioso para `movie` sem erro —
`internal/provider/tvdb/tvdb.go:213-215`). Mas restam dois acoplamentos a
`"tmdb"` que quebram cenários sem TMDB:

1. **Default de fonte hardcoded**: `internal/server/enrich.go:65-67` e
   `:108-109` assumem `source="tmdb"` quando o request não manda `source`.
   Num ambiente **só-TVDB**, um import/enrich sem `source` explícito vira
   `provFonte("tmdb")==false` → **502** (`respondFonteIndisponivel`,
   `enrich.go:181-184`), mesmo havendo provider capaz registrado.
2. **Interno-first só resolve tmdb**: `mapearExternos`/`idInternoItem`
   (`internal/server/search.go:281`) só casa itens externos com a biblioteca
   quando a fonte é `tmdb`. Itens vindos de outra fonte perdem o
   `internal_id`/badge "na biblioteca".

## 2. Jornada do usuário

1. Admin cadastrou **só** o TheTVDB.
2. Importa uma série (após 0046) **sem** informar `source` explícito na
   requisição → o sistema escolhe a fonte pelo provider **realmente
   registrado**, não por `"tmdb"` fixo. Sem 502.
3. Ao pesquisar, itens já presentes na biblioteca (qualquer fonte) aparecem
   marcados como "na biblioteca", não só os de origem TMDB.

## 3. Escopo

**Dentro:**
- Trocar o default `"tmdb"` por **resolução dinâmica** da fonte: escolher o
  primeiro provider registrado capaz do tipo pedido (na ordem de prioridade do
  registry). Se nenhum for capaz, erro claro (400/422 "nenhuma fonte disponível
  para X"), não 502 genérico.
- Generalizar `mapearExternos`/`idInternoItem` para casar a biblioteca por
  `(source, source_id)` de **qualquer** fonte, não só tmdb.

**Fora (explicitamente):**
- Preferência de fonte configurável pelo usuário (fica para follow-up).
- Implementar novos providers (isso é 0046 e os de arte).

## 4. Contrato técnico

### Backend (Go)
- `internal/server/enrich.go`: substituir os defaults `"tmdb"` (`:65-67`,
  `:108-109`) por um helper `fonteDefault(tipo)` que consulta o registry.
- `internal/server/search.go:281` (`mapearExternos`/`idInternoItem`): remover a
  premissa "só tmdb resolve"; casar por `(source, source_id)` genérico contra a
  tabela `media_external`/entities.
- `internal/provider/registry.go`: se necessário, expor "primeiro provider capaz
  do tipo T" (reusa o type-assert de capacidade; para movie, hoje é implícito —
  considerar em conjunto com 0046).

### Arquivos afetados
- `internal/server/enrich.go`
- `internal/server/search.go`
- Testes: cenário só-TVDB (import sem source → não-502) e interno-first
  multi-fonte.

## 5. Critérios de aceite

- [ ] Com só TVDB registrado, `POST /import`/enrich **sem** `source` não retorna
      502 por assumir tmdb; ou importa pela fonte disponível, ou dá erro
      semântico claro (nenhuma fonte para o tipo).
- [ ] Item da biblioteca importado de uma fonte não-tmdb aparece com
      `internal_id` na busca (badge "na biblioteca").
- [ ] Regressão zero com TMDB presente.
- [ ] `go build/vet/test -count=1` verdes; catraca local passa.

## 6. Notas de implementação / decisões

- Casável com 0046 (ambos mexem na seleção de fonte). Podem ser um PR só se
  ficar coeso, mas o escopo aqui é **só** os defaults/interno-first — a árvore
  do TMDB é 0046.

## 7. Fora deste briefing / follow-ups

- Preferência de fonte por tipo, configurável pelo admin (UI de settings).
- Chaves de API via UI (hoje env-only).
